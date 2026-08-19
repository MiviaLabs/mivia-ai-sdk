# Plan: contextplan

Status: shipped. Built on the shipped `contextstate` and `provider`
interfaces, under the phase 66 contract. Companion change: a fold of
reasoning vocabulary types into `provider`, documented in
`docs/plans/provider.md`.

## Goal

Fit one session into a bounded provider request. `contextplan` reads
a `contextstate.Session`, decides what fits a token window and what
does not, and returns a `provider.Request` plus the list of
decisions it made. Token estimates calibrate over time against a
completed turn's real usage. Reasoning content never crosses a
checkpoint boundary into the active context.

## Scope

Inside:

- A `contextplan` package. `policy/layers.json` gains one row:
  `contextplan` imports `contextstate`, `provider`, and `memory`
  only.
- `Planner`, built over a `*contextstate.MemStore` (the durable
  payload source) and a `*memory.Store` (a content-addressed decode
  cache, so a repeated `Plan` call in one session does not re-fetch
  and re-decode a payload it already resolved). `Plan` is the
  planner's one method: a session and a window budget in; a
  `provider.Request` and every elision decision out.
- Elision policy: newest-first inclusion, oldest-first drop, once the
  window budget is spent. A payload's `contextstate.RetentionClass`
  can protect it from an otherwise-due drop; `Plan` honors it before
  it drops by age. Retention lives on the `contextstate.PayloadRecord`,
  not the `SourceEvent`, so `Plan` resolves every candidate's full
  payload, including a reasoning event's, before it can decide drop,
  stub, or keep; see the API section for the exact rule and the Tests
  section for the resolution-failure case this implies.
- Calibration: `Calibrated` wraps a `provider.TokenEstimator` with an
  exponentially weighted moving average, corrected after each
  completed turn through `Observe`.
- Reasoning redaction at the checkpoint boundary: `IsReasoningEvent`
  marks a `contextstate.SourceEvent` whose `Kind` is
  `provider.ReasoningEventKind`. `Plan` still resolves such an
  event's payload, the same as any other, to build a fully populated
  `Elision.Ref`, but excludes the decoded content from
  `provider.Request.Messages` unconditionally; it records the
  exclusion as an `Elision` with `ElisionReasonReasoningRedacted` and
  `Kept == 0`, so a caller can audit the drop without the content
  ever reappearing.
- The `reasoning` fold: `provider` gains `ReasoningEventKind`,
  `ReasoningEffort` and its constants, `ReasoningBlock`, and
  `RedactBlock`. No new package. `api/provider.txt` gains these
  entries in the same change.

Outside:

- Durable state and ref minting. `contextstate` (phase 65) owns them;
  `contextplan` only reads a `*contextstate.MemStore` and a
  `contextstate.Session`, never mints or commits.
- Spool storage. Phase 67 owns durable overflow storage. `Plan` emits
  a `contextstate.ContentRef` for every elided payload; it writes
  nothing durable itself. The `*memory.Store` cache is a
  same-process decode cache only, not a spool: it holds resolved
  payload bytes, not elision output, and a caller may run `Plan`
  without one surviving process restart. `NewPlanner` binds to the
  concrete `*contextstate.MemStore` type, not an interface. This is
  deliberate for this phase, since `MemStore` is the only shipped
  payload source; phase 67 revisits the binding if a durable spool
  needs a different payload-source shape.
- Any concrete provider client. A caller supplies its own
  `provider.Completer`; `contextplan` calls only
  `provider.TokenEstimator`.
- Mivia's own prompt templates and per-workflow context bindings.
  Those stay in `mivia-agent`.

## API

The surface below is the lock target. `contextplan`'s share lands in
`api/contextplan.txt`; the fold lands as an addition to
`api/provider.txt`. Both via `make api-update` in the same change as
the code.

### `contextplan`

- `type Window struct { MaxTokens int; Reserve int }` — the token
  budget for one planned request. `MaxTokens` is the model's context
  window; `Reserve` is the headroom `Plan` never spends (for the
  model's own reply).
- `func (w Window) Validate() error` — rejects `MaxTokens <= 0`,
  `Reserve < 0`, and `Reserve >= MaxTokens`.
- `func (w Window) Budget() int` — `MaxTokens - Reserve`, the tokens
  `Plan` may spend on `Request.Messages`.
- `type ElisionReason string` — the closed set of reasons `Plan`
  drops or trims a payload.
- `const ElisionReasonWindowOverflow ElisionReason = "window_overflow"`
  — dropped because the window filled before this payload's turn.
- `const ElisionReasonRetentionExpired ElisionReason = "retention_expired"`
  — the payload's full content dropped for age, but its
  `contextstate.RetentionClass` earned it a stub instead of a full
  removal. Retention rule, exact and closed: `Plan` protects
  `contextstate.RetentionCompliance` only. `RetentionCompliance`
  gets a stub past its age-driven turn; `RetentionSession` and any
  other `RetentionClass` value, including an unrecognized or empty
  one, behave as unprotected and get
  `ElisionReasonWindowOverflow` with `Kept == 0` instead.
- `const ElisionReasonReasoningRedacted ElisionReason = "reasoning_redacted"`
  — excluded because `IsReasoningEvent` marked its source event; the
  content never entered `Request.Messages` at all.
- `type Elision struct { Ref contextstate.ContentRef; Reason ElisionReason; Kept int }`
  — one drop or trim decision. `Ref` is always the resolved
  `contextstate.PayloadRecord`'s `ContentRef`; `Plan` resolves every
  candidate's payload, including a reasoning event's, before it
  decides drop, stub, or keep. `Kept` is the byte length of
  `StubContent(...)`'s return for a stubbed payload; zero means
  `Plan` inserted no message at all for that payload.
- `const StubContentBytes = 256` — the fixed byte cap `Plan` applies
  when it keeps a stub for a `RetentionCompliance` payload past its
  age-driven turn.
- `func StubContent(content []byte) []byte` — truncates `content` to
  `StubContentBytes`, appending a fixed truncation marker
  (`"...[elided]"`) inside that cap when truncation occurs; returns
  `content` unchanged when it already fits. `Plan` calls this to
  build the one `provider.Message` it inserts for a retained stub,
  keeping the original `Message.Role` and setting `Content` to the
  stub bytes as a string.
- `type PlanResult struct { Request provider.Request; Elisions []Elision; EstimatedTokens int }`
  — `Plan`'s output. `EstimatedTokens` is the estimator's total over
  `Request.Messages`, always at or under `Window.Budget()`.
- `type Planner struct { ... }` — unexported fields. Built only
  through `NewPlanner`. Holds the payload source, the decode cache,
  and a mutex-guarded metadata cache keyed by `PayloadRef` that
  stores each resolved record's `Retention` and `ContentRef`; safe
  for concurrent use, since `Plan` holds no other mutable state of
  its own between calls other than `Calibrated`, which the caller
  owns separately.
- `func NewPlanner(store *contextstate.MemStore, cache *memory.Store) (*Planner, error)`
  — a nil `store` or nil `cache` is an error.
- `func (p *Planner) Plan(ctx context.Context, sess *contextstate.Session, w Window, e provider.TokenEstimator) (PlanResult, error)`
  — walks `sess.Source` newest to oldest. For every event, `Plan`
  resolves the full `contextstate.PayloadRecord` from the decode
  cache and the metadata cache together on a full hit, or through
  `store.Get` once on either miss, before it decides anything:
  `Retention` lives on the record, not on `SourceEvent`, so
  a resolve happens even for a payload `Plan` ends up fully dropping,
  or for a reasoning event whose content never reaches
  `Request.Messages`. Every `Elision.Ref` is this resolved record's
  `contextstate.ContentRef`, so a reasoning-redacted entry carries the
  same fully populated `ContentRef` as any other elision. Resolve
  happens before the `IsReasoningEvent` check, so `IsReasoningEvent`
  events also hit the cache and the store; this is the accepted cost
  of a typed `ContentRef` on every `Elision`, over the alternative of
  a partial ref built from `SourceEvent.PayloadRef` alone.
  `IsReasoningEvent` events never enter `Request.Messages`, in either
  direction of the walk, and always produce an `Elision` with
  `ElisionReasonReasoningRedacted`. For every other event, `Plan`
  adds the decoded `provider.Message` while the running estimate,
  from `e.EstimateTokens`, stays at or under `w.Budget()`. Once
  adding the next-oldest message would exceed the budget: a
  `RetentionCompliance` payload gets `Plan`'s stub, built by
  `StubContent`, inserted as one `provider.Message` in place of the
  full one, and `Plan` re-runs `e.EstimateTokens` against the request
  with the stub included before it accepts the insertion. When the
  stub itself would exceed `w.Budget()`, `Plan` drops the stub too:
  no message inserted, recorded as `ElisionReasonWindowOverflow` with
  `Kept == 0`, the same as an unprotected payload. Retention
  guarantees a stub over a full drop; it never overrides the window
  bound. Every other payload past the budget is fully dropped, no
  message inserted, recorded as `ElisionReasonWindowOverflow` with
  `Kept == 0`. `EstimatedTokens` in `PlanResult` is always at or under
  `w.Budget()`, with no exception for retained content. `Plan` returns
  a non-nil error only on a malformed `Window`, a nil `sess`, or a
  payload-resolution failure from the store or the cache — including
  one encountered only during the retention check of an
  otherwise-fully-droppable payload, or one on a reasoning event; it
  never returns a partial `PlanResult`.
- `const DefaultSmoothingFactor = 0.3` — the default EWMA weight a
  new `Observe` sample carries against the running correction factor,
  used when `Calibrate` receives a non-positive `alpha`.
- `const MinCorrectionFactor = 0.5` — the floor `Observe` clamps the
  correction factor to.
- `const MaxCorrectionFactor = 2.0` — the ceiling `Observe` clamps the
  correction factor to.
- `type Calibrated struct { ... }` — unexported fields. Wraps a
  `provider.TokenEstimator` and implements the same interface, so a
  `*Calibrated` is itself a valid `Plan` argument.
- `func Calibrate(est provider.TokenEstimator, alpha float64) *Calibrated`
  — a nil `est` is a caller error caught at first `EstimateTokens`
  call, not at construction, matching the wrapped interface's own
  contract. `alpha` is the EWMA smoothing weight in `(0, 1]`; a value
  outside that range, including zero or negative, falls back to
  `DefaultSmoothingFactor`. `alpha` is a constructor argument, not a
  hardcoded constant, so a test can drive convergence in a bounded
  number of `Observe` calls.
- `func (c *Calibrated) EstimateTokens(req provider.Request) (int, error)`
  — calls the wrapped estimator, then scales the result by the
  current EWMA correction factor, itself always within
  `[MinCorrectionFactor, MaxCorrectionFactor]`.
- `func (c *Calibrated) Observe(actual int)` — records one completed
  turn's real `provider.Usage.TotalTokens` against the last estimate
  this `Calibrated` produced, and updates the correction factor by
  `alpha`, clamped to `[MinCorrectionFactor, MaxCorrectionFactor]`. A
  first call, before any estimate, and a non-positive `actual`, are
  both no-ops.
- `func IsReasoningEvent(e contextstate.SourceEvent) bool` — reports
  whether `e.Kind == provider.ReasoningEventKind`.

### `provider` fold (reasoning vocabulary)

- `const ReasoningEventKind = "reasoning"` — the
  `contextstate.SourceEvent.Kind` value that marks a reasoning trace.
  The one place the literal appears; `contextplan.IsReasoningEvent`
  compares against this constant, never the literal.
- `type ReasoningEffort string` — the provider-neutral reasoning
  effort vocabulary, closed by four constants below. A
  `provider.ReasoningPolicy` implementation may report any of these
  from `ReasoningEffort() string`; the interface's return type stays
  `string` to keep the existing lock, but a caller compares against
  these constants instead of a literal.
- `const ReasoningEffortNone ReasoningEffort = "none"`
- `const ReasoningEffortLow ReasoningEffort = "low"`
- `const ReasoningEffortMedium ReasoningEffort = "medium"`
- `const ReasoningEffortHigh ReasoningEffort = "high"`
- `type ReasoningBlock struct { Content string; Redacted bool }` — one
  reasoning segment a model produced. `Content` is empty whenever
  `Redacted` is true. `ReasoningBlock` never appears on `Message` or
  `Response`; it is a value a caller carries alongside its own
  session state, matching the `internal/reasoning` source this fold
  ports.
- `func RedactBlock(b ReasoningBlock) ReasoningBlock` — returns `b`
  with `Content` cleared and `Redacted` set true. Idempotent: a
  second call on an already-redacted block returns it unchanged.

## Tests

`contextplan/contextplan_test/`, an external test package.

- `window_test.go` — `Window.Validate` table: non-positive
  `MaxTokens`, negative `Reserve`, `Reserve == MaxTokens`,
  `Reserve > MaxTokens`, and one valid case. `Window.Budget` for a
  valid and an invalid window.
- `plan_test.go` — `Planner.Plan` unit cases, one payload store and
  cache built per case:
  - Fits whole: every source event's payload fits the budget; result
    carries every message, zero `Elisions`.
  - Elides oldest first: a session over budget; the oldest events'
    refs appear in `Elisions` with `ElisionReasonWindowOverflow`, the
    newest survive in `Request.Messages`.
  - Respects retention: an old event whose `PayloadRecord.Retention`
    is `contextstate.RetentionCompliance` keeps a stub (`Kept > 0`,
    equal to `StubContent`'s output length) and reports
    `ElisionReasonRetentionExpired` instead of being fully dropped.
    The case asserts `EstimatedTokens` still covers the inserted
    stub, proving `Plan` re-ran the estimate after the insertion.
  - Unrecognized retention: an old event whose `Retention` is
    `contextstate.RetentionSession`, and a second case with an empty
    `RetentionClass`; both behave as unprotected, fully drop, and
    report `ElisionReasonWindowOverflow` with `Kept == 0`.
  - Reserves headroom: `EstimatedTokens` never exceeds
    `w.Budget()`, checked against a `Window` with a non-zero
    `Reserve`.
  - Stub does not fit: a `Window` whose remaining budget, after the
    newest messages, is smaller than `StubContentBytes`'s estimated
    token cost, and a `RetentionCompliance` payload landing at that
    boundary. Asserts the stub is dropped despite retention: no
    message inserted, `ElisionReasonWindowOverflow`, `Kept == 0`, and
    `EstimatedTokens` still at or under `w.Budget()`. A second case
    stacks two `RetentionCompliance` payloads back-to-back at the
    boundary, where the first stub fits and the second does not;
    asserts the first keeps its stub and the second fully drops.
  - Reasoning events: a session mixing `provider.ReasoningEventKind`
    and ordinary events; every reasoning event's ref lands in
    `Elisions` with `ElisionReasonReasoningRedacted` and no
    corresponding `provider.Message`, regardless of budget headroom.
    `Plan` still resolves each reasoning event's payload; the case
    asserts the returned `Elision.Ref` matches the resolved record's
    `ContentRef`, proving the resolve happened even though the
    content never reached `Request.Messages`.
  - Error cases: nil `sess`, an invalid `Window`, a payload
    resolution failure from the store for a payload `Plan` would
    have kept, a payload resolution failure from the store for a
    payload `Plan` would otherwise have fully dropped by age, and a
    payload resolution failure from the store for a reasoning event —
    the resolve happens in every case, so each failure surfaces.
  - Concurrent use: N goroutines each call `Plan` on one shared
    `*Planner` with distinct sessions built from a shared
    `*contextstate.MemStore` and `*memory.Store`, run under
    `go test -race`. Every goroutine's `PlanResult` matches the
    single-goroutine result for its own session; no call panics or
    corrupts another goroutine's result.
- `calibrated_test.go` — `Calibrate` and `Observe`:
  - EWMA converges: repeated `Observe` calls with a stable actual
    value pull `EstimateTokens`'s scale factor toward that value.
  - Bounds clamp: an extreme single `Observe` does not send the
    correction factor outside `[MinCorrectionFactor,
    MaxCorrectionFactor]`.
  - Alpha selection: a non-positive `alpha` passed to `Calibrate`
    falls back to `DefaultSmoothingFactor`; a valid `alpha` in
    `(0, 1]` is used as given, checked by comparing convergence speed
    across two `Calibrated` values built with different `alpha`.
  - Zero estimates skip: `Observe(0)` does not corrupt the running
    average; `EstimateTokens` on a zero-cost request stays zero.
- `reasoning_event_test.go` — `IsReasoningEvent` true for
  `provider.ReasoningEventKind`, false for every other `Kind`.
- `redact_block_test.go`, in `provider/provider_test/` — `RedactBlock`
  clears `Content`, sets `Redacted`, and is idempotent on a second
  call.
- Property: `TestPropertyPlanNeverExceedsWindow`, table-driven over
  varied session sizes, `Window` values, and retention mixes,
  including boundary cases where a stub barely fits or barely does
  not — the planned request's `EstimatedTokens` never exceeds
  `w.Budget()` across every case, with no exception for retained
  content.
- `plan_integration_test.go` — a full session: seed a
  `*contextstate.MemStore` with payloads across two retention
  classes and a reasoning-kind event, run `Plan` against a small
  `Window`, and assert the returned `Request.Messages`, `Elisions`,
  and `EstimatedTokens` together describe one consistent outcome.

## Verification

- `make verify` passes with the new `contextplan` row in
  `policy/layers.json`, present before any `contextplan` code lands.
- `api/contextplan.txt` lands via `make api-update`, matching the API
  section above.
- `api/provider.txt` gains the reasoning-vocabulary entries via the
  same `make api-update` run, in the same change as the fold.
- No `agentrun` or `e2e` wiring in this phase: neither row in
  `policy/layers.json` lists `contextplan` today, and this plan adds
  no edge for either. An `agentrun`-wired scenario proving a long
  session completes with recorded elisions is a later phase's plan,
  once that edge is deliberately added.
- Coverage floor of 85 holds for `contextplan` and for the total,
  including the reasoning-fold additions to `provider`.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass with this plan and the new `policy/layers.json` row in place.
