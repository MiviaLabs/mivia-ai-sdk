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
  cache that `Plan` populates on a successful resolve; the
  Correctness fix section below removes its role in skipping a
  `store.Get` call, so it no longer changes what a repeated `Plan`
  call observes). `Plan` is the planner's one method: a session and a
  window budget in; a `provider.Request` and every elision decision
  out.
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
- Spool storage. The `spool` package owns durable overflow storage.
  `Plan` emits a `contextstate.ContentRef` for every elided payload;
  it writes nothing durable itself. The `*memory.Store` cache is a
  same-process decode cache only, not a spool: it holds resolved
  payload bytes, not elision output, and a caller may run `Plan`
  without one surviving process restart. `NewPlanner` binds to the
  concrete `*contextstate.MemStore` type, not an interface. This is
  deliberate for this phase, since `MemStore` is the only shipped
  payload source; `contextplan` does not yet consume `spool`, and a
  later change revisits the binding if a durable spool needs a
  different payload-source shape.
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
  `Request.Messages`. Two conditions bound it. The bound holds for a
  deterministic estimator whose total for an empty message list is at
  or under the budget. `admit` admits a candidate only when the trial
  estimate is at or under the budget, and the final estimate re-runs
  the same estimator over the same accepted list. An estimator
  charging a fixed per-request overhead above the budget therefore
  reports a total over `Window.Budget()`, because `Plan` has no
  message left to drop. A second escape exists: a final estimate that
  errors is reported as zero, so an erroring estimator reports zero
  whatever the message list holds. `Plan` never compares its final
  estimate against the budget.
- `type Planner struct { ... }` — unexported fields. Built only
  through `NewPlanner`. Holds the payload source and the decode cache.
  The Correctness fix section below removes the mutex-guarded metadata
  map this struct once carried: `resolvePayload` no longer reads a
  cached map, or the decode cache, to skip a `store.Get` call, so
  neither cache can serve a stale `Retention` or `Revoked` value. Safe
  for concurrent use, since `Plan` holds no other mutable state of its
  own between calls other than `Calibrated`, which the caller owns
  separately.
- `func NewPlanner(store *contextstate.MemStore, cache *memory.Store) (*Planner, error)`
  — a nil `store` or nil `cache` is an error.
- `func (p *Planner) Plan(ctx context.Context, sess *contextstate.Session, w Window, e provider.TokenEstimator) (PlanResult, error)`
  — walks `sess.Source` newest to oldest. For every event, `Plan`
  resolves the full `contextstate.PayloadRecord` through one
  `store.Get` call before it decides anything — see the Correctness
  fix section below for why this reads through the store instead of
  from a cache: `Retention` lives on the record, not on `SourceEvent`, so
  a resolve happens even for a payload `Plan` ends up fully dropping,
  or for a reasoning event whose content never reaches
  `Request.Messages`. Every `Elision.Ref` is this resolved record's
  `contextstate.ContentRef`, so a reasoning-redacted entry carries the
  same fully populated `ContentRef` as any other elision. Resolve
  happens before the `IsReasoningEvent` check, so `IsReasoningEvent`
  events also hit the store; this is the accepted cost
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
  `EstimatedTokens` in `PlanResult` stays at or under `w.Budget()`
  under the two conditions stated with `PlanResult`, with no
  exception for retained content. `Plan` returns
  a non-nil error only on a malformed `Window`, a nil `sess`, or a
  payload-resolution failure from the store — including
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
  cache built per case. The Correctness fix section below replaces
  this file's cache-hit-skips-store assumption; see that section for
  the two existing cases it requires the builder to delete or rewrite:
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
    `Reserve`. The case uses a deterministic proportional estimator
    that returns zero for an empty message list.
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
  content. Every case uses a deterministic proportional estimator
  that returns zero for an empty message list. That estimator class is
  the scope of the claim.
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

## Correctness fix: rune-safe stub and the budget claim

Two defects, one code change and one doc change.

### Rune-safe stub cut

`StubContent` in `contextplan/elision.go` appends `content[:keep]`
with no rune alignment. A cut inside a multi-byte rune puts invalid
UTF-8 into the stub, and the stub reaches the model transcript.
`agentloop/wire.go` already fixed this class with `strings.ToValidUTF8`.

The change, in `StubContent`:

- Append `bytes.ToValidUTF8(content[:keep], nil)` in place of the raw
  `content[:keep]`. `bytes` is standard library, called directly. No
  new package and no copied type.
- The call drops every invalid byte in the prefix, not the trailing
  partial rune alone. That matches `agentloop`'s shipped behavior.
- The result may be shorter than `keep`. `StubContentBytes` stays the
  cap, not a promised length. State that in the doc comment.
- The `keep < 0` clamp stays as it is.

No exported symbol changes. `api/contextplan.txt` stays as locked. The
`contextplan` row in `policy/layers.json` is unchanged.

### The `EstimatedTokens` claim

Three places claim `EstimatedTokens` is always at or under
`Window.Budget()`. All three are false for two estimator classes.

- `contextplan/planner.go:26`, the `PlanResult` field comment.
- `contextplan/planner.go:63-64`, inside `Plan`'s doc comment.
- `docs/packages/contextplan.md:44`, the `Plan` entry.

The bound comes from the per-insertion trial checks in `admit`, at
`contextplan/planner.go:114` and `contextplan/planner.go:122`. Each
admits a candidate only when the trial estimate is at or under the
budget. The final estimate at `contextplan/planner.go:95` re-runs the
same estimator over the same accepted list. So the bound holds for a
deterministic estimator whose total for an empty message list is at
or under the budget. An estimator charging a fixed per-request
overhead above the budget breaks it, because `Plan` drops every event
and still reports that overhead. An estimator that errors on the
final call breaks it the other way: `contextplan/planner.go:96-97`
sets the total to zero.

The change is documentation only. Behavior stays as it is.

- Reword all three places in the same change, matching the API
  section of this plan.
- Keep each code comment to two lines: the condition, then the two
  escapes named in one clause. The reasoning stays in this plan.
- Suggested code wording: "EstimatedTokens stays at or under
  Window.Budget() for a deterministic estimator whose empty-list total
  fits the budget. An estimator with a larger fixed overhead, or one
  that errors on the final call, breaks that bound."

### Tests

In `contextplan/contextplan_test/elision_test.go`:

- A `StubContent` case whose cut boundary falls inside a multi-byte
  rune. Assert `utf8.Valid` on the output, and assert the output ends
  with the truncation marker. This case kills the mutation that
  restores the raw `content[:keep]` append.
- A `StubContent` case whose content is pure ASCII and over the cap.
  Output length stays exactly `StubContentBytes`, proving the fix
  changes nothing for aligned content.

In `contextplan/contextplan_test/plan_resolution_test.go`:

- A `Plan` case with a `RetentionCompliance` payload holding
  multi-byte runes. Assert the inserted stub message's `Content` is
  valid UTF-8.
- A `Plan` case with a deterministic estimator charging a fixed
  per-request overhead above `w.Budget()`. Assert `Plan` returns a nil
  error, an empty `Request.Messages`, one
  `ElisionReasonWindowOverflow` entry per source event, and
  `EstimatedTokens` above `w.Budget()`. This pins the limit the
  reworded comment documents, instead of documenting it away.

Do not add these cases to `plan_test.go`. That file is 452 lines
against the 500-line structure limit.

### Verification

- `make verify` passes. `contextplan` holds the 85 coverage floor.
- `go test -race ./contextplan/...` passes.
- `python3 scripts/check_api.py` passes with no `api/` diff.
- `python3 scripts/check_docs.py` and
  `python3 scripts/check_structure.py` pass.
- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  and `python3 scripts/check_prose.py` pass.
- `docs/packages/contextplan.md` changes in the same commit as the
  code.
- `docs/plans/agentloop.md` and the `policy/layers.json` row adding
  `schema` to `agentloop` stay out of this commit. They belong to the
  concurrent `agentloop` change and need their own plan review.

## Correctness fix: skip a revoked payload instead of failing Plan

`docs/plans/contextstate.md` gives `contextstate.MemStore` a `Revoke`
method, a `Status` audit accessor, and makes `Get` deny a revoked
record's `Data`, wrapping the new `contextstate.ErrPayloadRevoked` and
returning the zero value like every other `Get` error. `Plan` calls
`Get` through `resolvePayload` for every source event. Without this
change, `Plan` treats that new error like any other resolution failure
and returns a non-nil error for the whole call, so one revoked payload
anywhere in a session blocks every other event from planning. This
change makes `Plan` treat a revoked payload as a fourth elision reason
instead: the composition layer's own decision, on top of the storage
layer's fail-closed `Get`.

This change also removes `resolvePayload`'s cache-hit fast path. The
existing `meta` map and the `*memory.Store` decode cache let a repeat
resolve skip `store.Get` entirely once a ref had resolved once. A
`Revoke` issued after that first resolve was invisible to a `Planner`
that had already cached the ref: the cached `Revoked == false` metadata
never refreshed, so every later `Plan` call on that `Planner` kept
serving the revoked content from cache, forever, for the life of the
process. A storage-layer fail-closed `Get` protects nothing if its one
caller never calls it again. `resolvePayload` now resolves every event
through `store.Get` on every call; caching a resolve result to skip a
future revocation check is out of scope for this phase.

### API addition

One addition to `api/contextplan.txt`, landed through `make
api-update`:

- `const ElisionReasonRevoked ElisionReason = "revoked"` — marks a
  payload `Plan` excluded because `contextstate.MemStore.Get` denied it
  as revoked. This reason is security-relevant, unlike
  `ElisionReasonWindowOverflow` and `ElisionReasonRetentionExpired`:
  those two are budget economics a caller may reasonably ignore, but a
  caller that skips `ElisionReasonRevoked` gets a `Request` silently
  missing content its own store denied, with no other signal. `Kept`
  is always `0`: a revoked payload never gets a stub, even under
  `RetentionCompliance`, since `Get` denies `Data` before `Plan`'s
  retention check ever runs. `Elision.Ref` is still the record's
  `contextstate.ContentRef`, resolved through `Status` alongside the
  `Get` denial, so a caller can audit which ref was revoked without
  seeing its content.

### `Plan`'s new behavior

`(Planner) Plan` and `resolvePayload` in `contextplan/planner.go`
change together:

- `resolvePayload` drops its cache-hit fast path. Every call now
  resolves `event.PayloadRef` through one `store.Get` call; the `meta`
  map and the `*memory.Store` decode cache are no longer read to skip
  that call, so a `Revoke` issued between two `Plan` calls is visible
  on the very next call. `NewPlanner`'s signature and the `cache`
  parameter stay as locked; `cache.Put` still runs on a successful
  resolve, preserving today's write-side population, even though this
  path no longer relies on it to skip a `Get`. This is a deliberate
  cost: one store round trip per event per `Plan` call, in exchange
  for a revocation check that is never stale.
- On `contextstate.ErrPayloadRevoked` from `store.Get`, `resolvePayload`
  makes one more call, `store.Status(ref)`, to recover the denied
  record's metadata (`Ref`, `Retention`, `Data == nil`) for the
  `Elision` this produces. A `Status` failure (an unknown ref, which
  should not happen for a ref `Get` just found revoked, but is handled
  rather than assumed) propagates as a `Plan`-level error, the same as
  any other resolution failure. On success, `resolvePayload` returns
  the `Status` record and the original `ErrPayloadRevoked`.
- `Plan`'s per-event loop checks `resolvePayload`'s error with
  `errors.Is` against `contextstate.ErrPayloadRevoked` before treating
  any other error as fatal. On a match, `Plan` appends
  `Elision{Ref: record.Ref, Reason: ElisionReasonRevoked}` and
  continues to the next event; no message enters `Request.Messages`.
  Every other resolution error still fails `Plan` outright, unchanged.
  A revoked reasoning event takes the revoked branch too, before the
  `IsReasoningEvent` check runs; it never reaches
  `ElisionReasonReasoningRedacted`, since `Get` denied its `Data`
  first and `Plan` has nothing left to redact.
- `PlanResult.Request` stays a valid `provider.Request`: skipping a
  revoked event's message is exactly the existing drop path window
  overflow already takes, just gated on a different signal. No new
  validity condition on `Request`.

### Tests

In `contextplan/contextplan_test/plan_test.go` or a same-package
sibling under the 500-line limit:

- Delete or rewrite, in the same commit as this change,
  `TestPlanResolutionCacheHitSkipsStore` and
  `TestPlanResolutionMetaSurvivesCacheEviction` in
  `contextplan/contextplan_test/plan_resolution_test.go`. Both pin the
  cache-hit fast path this change removes:
  `TestPlanResolutionCacheHitSkipsStore` asserts a second `Plan` call
  must not see a `Put` that overwrote the store between calls, which
  is now false, since every call re-reads the store;
  `TestPlanResolutionMetaSurvivesCacheEviction` exercises meta-cache
  survival past a decode-cache eviction, a behavior that no longer
  exists once `resolvePayload` stops reading the meta map to decide
  whether to skip `store.Get`. Left in place, both fail against this
  change's own correct behavior. A builder that keeps either test
  unchanged has not finished this change.
- A session where one middle event's payload is revoked in the
  backing `*contextstate.MemStore` before `Plan` runs. Asserts: that
  event's ref appears in `Elisions` with `ElisionReasonRevoked` and
  `Kept == 0`; every other event's message is present in
  `Request.Messages`; `Plan` returns a `nil` error. Kills a mutation
  that propagates `ErrPayloadRevoked` as a `Plan`-level failure.
- Revoke after a warm cache, on one `*Planner`: run `Plan` once while
  the payload is not revoked, asserting its message is present and no
  `Elisions` entry names it. Call `Revoke` on the backing store. Run
  `Plan` again on the same `*Planner`, same session. Assert the second
  call's result now carries `ElisionReasonRevoked` for that ref and no
  longer includes its message. This is the adversarial case the
  feature exists for; it fails against the shipped cache-hit fast
  path and must pass against this change. Kills a mutation that
  reintroduces a cache-hit skip of `store.Get`.
- A revoked event holding `RetentionCompliance`. Asserts it still gets
  `ElisionReasonRevoked` with `Kept == 0`, not a stub. Kills a
  mutation that runs the retention-stub path before the revoked check.
- A revoked event whose `Kind` is `provider.ReasoningEventKind`.
  Asserts `ElisionReasonRevoked`, not `ElisionReasonReasoningRedacted`.
  Kills a mutation that reorders the two checks.
- A session with every event revoked. Asserts `Plan` returns a `nil`
  error, an empty `Request.Messages`, and one `ElisionReasonRevoked`
  entry per event. Kills a mutation that fails `Plan` when
  `Request.Messages` ends up empty.
- No test targets a `Status` failure on the revoked branch. This
  branch is defensive and untestable against `*contextstate.MemStore`
  today: nothing in the shipped store lets a ref return
  `ErrPayloadRevoked` from `Get` and then `ErrPayloadNotFound` from
  `Status`, since `contextstate.md`'s Outside section excludes
  deletion and `NewPlanner` binds to the concrete `*MemStore` with no
  seam for a fake. The `Plan`-level error return on this branch stays
  in the code as a fail-closed guard, not as a covered path.
- A non-revoked `Plan` case, unchanged from the existing suite, stays
  green: proves the new branch does not affect the common path.

### Verification

- `make verify` passes; `contextplan` holds the 85 coverage floor.
- `api/contextplan.txt` lands through `make api-update`, adding only
  `ElisionReasonRevoked`.
- `go test -race ./contextplan/... ./contextstate/...` passes.
- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  and `python3 scripts/check_prose.py` pass. No new import edge; the
  `contextplan` row in `policy/layers.json` is unchanged.
- `docs/packages/contextplan.md` gains `ElisionReasonRevoked` in the
  `ElisionReason` list, states its security-relevance, and states that
  `resolvePayload` no longer skips `store.Get` on a cache hit, in the
  same commit as the code.
- This change lands with or after the `contextstate` change in
  `docs/plans/contextstate.md`: `MemStore.Revoke`, `MemStore.Status`,
  and the new `Get` contract must exist before `contextplan` can
  compile against `contextstate.ErrPayloadRevoked`.

## Change: compaction policy

Status: plan, ready for plan review. Ports the structural retention
half of `mivia-agent/internal/contextmgr/planner.go` into
`contextplan`, under this task's changed defaults and hard rules.

### Change goal

Compact a message history that reaches the window trigger down to a
target, through a fixed retention set plus a contiguous recent-tail
fill. Produce a deterministic idempotency key. Compaction here is
structure only: the LLM summary that must accompany it lives in
`contextsummary`, and the caller that joins the two is `agentloop`.

### Change scope

Inside:

- `Compaction`, the threshold and retention configuration, embedded in
  `Window` as a new `Compaction` field.
- `Compact`, one pure function over `[]provider.Message`. No store, no
  LLM call, no state.
- The retention set port: the system message at index zero when its
  role is `RoleSystem`, the latest `RoleUser` message, every message
  whose `Name` is in `PreserveNames`, and the latest complete
  assistant-plus-tool unit.
- The recent-tail fill: contiguous units, newest first, bounded by
  count and by the target tokens.
- The idempotency key, fingerprinted through `contextstate` over the
  retained set, under the algorithm string `context-compact-v1`.
- A mutex inside `Calibrated`, guarding `factor` and `lastEst`. No
  API change: `EstimateTokens` and `Observe` become safe for
  concurrent use on one shared value. `agentloop` calls both per
  iteration, and concurrent `Run` calls on one shared `Loop` share one
  `Calibrated`, so the unguarded fields are a data race.

Outside:

- Any LLM call. `Compact` never dials a provider. The task's hard rule
  reverses the reference: compaction is LLM-only at the caller, so
  `contextplan` carries no structural fallback and no `Force` flag,
  and no manual compact entry point exists.
- Any loop wiring, usage observation, or prompt-too-long recovery.
  `agentloop` owns those; see `docs/plans/agentloop.md`.
- Changes to `Plan`, `Elision`, `StubContent`, or `Calibrated`'s API
  or EWMA semantics; the one internal change is the concurrency guard
  the Inside section adds. The session-store planner keeps its
  shipped behavior.
- Tool-schema pricing. The reference priced tool schemas into the
  trigger; this port estimates messages only, matching what `Plan`
  already estimates.

### Change API

One addition to `api/contextplan.txt`, landed through `make
api-update`:

```go
// DefaultTriggerPercent compacts at this percent of Window.Budget().
const DefaultTriggerPercent = 100

// DefaultTargetPercent compacts down to this percent of Budget().
const DefaultTargetPercent = 10

// DefaultRecentTail is the message-count bound of the tail fill.
const DefaultRecentTail = 8

// MaxRecentTail is the highest tail bound a caller may set.
const MaxRecentTail = 64

// CompactionAlgorithm names the idempotency-key fingerprint scheme.
const CompactionAlgorithm = "context-compact-v1"

// Compaction configures compaction thresholds and retention. The zero
// value means the defaults, never "disabled": TriggerPercent zero
// means DefaultTriggerPercent, TargetPercent zero means
// DefaultTargetPercent, RecentTail zero means DefaultRecentTail.
type Compaction struct {
    TriggerPercent int
    TargetPercent  int
    TargetTokens   int
    RecentTail     int
    PreserveNames  []string
}

// Validate rejects percents outside (0, 100], a negative TargetTokens
// or RecentTail, a RecentTail over MaxRecentTail, an empty but
// present PreserveNames entry, and duplicate PreserveNames entries.
// When TargetTokens is zero, a TargetPercent at or above the resolved
// TriggerPercent is rejected; when TargetTokens is positive, that
// comparison is skipped and Window.Validate instead rejects
// a TargetTokens at or above Budget().
func (c Compaction) Validate() error

// Window gains one field: Compaction Compaction. Window.Validate now
// also runs Compaction.Validate and rejects a positive TargetTokens at
// or above Budget().
type Window struct {
    MaxTokens  int
    Reserve    int
    Compaction Compaction
}

// CompactTrigger returns the trigger in tokens: Budget times
// TriggerPercent, floored.
func (w Window) CompactTrigger() int

// CompactTarget returns the target in tokens: TargetTokens when
// positive, else Budget times TargetPercent, floored.
func (w Window) CompactTarget() int

// CompactResult is Compact's output.
type CompactResult struct {
    Kept          []provider.Message
    Dropped       []provider.Message
    BeforeTokens  int
    AfterTokens   int
    TriggerTokens int
    TargetTokens  int
    Compacted     bool
    Key           string
}

// Compact applies the trigger check and the retention policy. An
// invalid window fails Window.Validate before any estimate. A
// request at or above the trigger compacts; below it passes through
// with Compacted false. The retention set is mandatory; the tail fill
// is optional and stops at the first unit that breaks contiguity, the
// message-count bound, or the target. Kept preserves the original
// relative order. The Key is deterministic per input.
func Compact(msgs []provider.Message, w Window, e provider.TokenEstimator) (CompactResult, error)

// Sentinel errors for Compact; test with errors.Is.
var (
    ErrNoMessages        = errors.New("contextplan: no messages to compact")
    ErrEstimateFailed    = errors.New("contextplan: token estimate failed")
    ErrRetentionOverflow = errors.New("contextplan: retention set alone exceeds the window")
    ErrNoObjective       = errors.New("contextplan: no user message to retain as objective")
)
```

### Retention rules, exact

- `Compact` validates `w` through `Window.Validate`, which runs
  `Compaction.Validate`; an invalid window fails before any estimate
  runs.
- A unit is one `RoleAssistant` message that carries `ToolCalls`
  together with the contiguous `RoleTool` replies that directly follow
  it. The unit ends at the first reply whose `ToolCallID` is not one
  of that assistant's call ids; that reply is its own single-message
  unit. Every other message is one single-message unit. Selection is
  atomic per unit.
- A `PreserveNames` match on any message of a unit selects the whole
  unit.
- The latest complete assistant-plus-tool unit is the newest unit
  whose assistant message carries `ToolCalls` and whose replies are
  all present directly after it.
- When no `RoleUser` message exists anywhere in the input, `Compact`
  fails closed with `ErrNoObjective` and no partial result. The
  objective is mandatory, mirroring the reference's missing-objective
  failure.
- The tail fill walks units newest to oldest, skips selected ones,
  stops at the first unselected unit that would break the message
  count or the target, and never resumes past it. The retained
  optional tail stays a contiguous suffix.
- `BeforeTokens` and `AfterTokens` come from `e.EstimateTokens` over
  the input and the kept list. An estimator error fails `Compact`
  with `ErrEstimateFailed`; it never degrades to a silent pass.
- When the retention set alone estimates above `w.Budget()`, `Compact`
  fails closed with `ErrRetentionOverflow` and no partial result. This
  is the fail-closed distinct error the task requires.
- The `Key` is `CompactionAlgorithm`, a colon, then
  `contextstate.Mint` over the canonical JSON fingerprint: algorithm,
  budget, trigger tokens, target tokens, and one fingerprint record
  per kept message, fields role, content, tool call id, name, and for
  every tool call its id and its arguments, matching the reference
  fingerprint shape. Repeated `Compact` calls on equal inputs return
  equal keys; inputs differing in one tool call's arguments return
  different keys. `encoding/json` over a struct is deterministic; no
  map appears in the fingerprint.
- `Compact` reads `provider.Message.Name` for `PreserveNames`. That
  field lands through `docs/plans/provider.md` in the same change
  window; `Compact` does not compile before it exists.

### Deviations from the reference

- Trigger 100 percent and target 10 percent, where the reference used
  80 and 50. The task overrides the defaults.
- No `Force` flag, no structural-only path, no manual compact. The
  task forbids all three.
- No `OutputReserve`, `CalibrationRatio`, `ContextAccounting`, source
  ranges, or revision plumbing. The caller's `Window` and its own
  calibrated estimator carry those concerns.
- The key prefix reuses `contextstate.Mint`'s canonical address form,
  where the reference minted a bare `compact-` prefix. One ref form
  per SDK.

### Change tests

In `contextplan/contextplan_test/compact_test.go`:

- `Compaction.Validate` table: each bound above, plus the zero value
  passing as defaults. One case pins the two target modes: a
  `TargetPercent` at or above the trigger fails in percent mode and
  passes in override mode, where `Window.Validate`'s `Budget()` bound
  applies instead.
- `Window.Validate` rejects a positive `TargetTokens` at or above
  `Budget()`.
- `CompactTrigger` and `CompactTarget` math: defaults, explicit
  percents, the `TargetTokens` override, and flooring.
- Below trigger: everything kept, `Compacted` false, `Dropped` empty.
- At trigger: compacts; the retention set survives; the tail is a
  contiguous suffix under `RecentTail`; `AfterTokens` lands at or
  under the target or the fill stopped at contiguity.
- Preserve names: a named message survives regardless of age; a match
  inside a unit keeps the whole unit.
- The latest complete assistant-plus-tool unit survives whole; an
  assistant message with a missing reply is not selected as the latest
  unit.
- Unit boundary: a `RoleTool` reply whose `ToolCallID` is not one of
  the preceding assistant's call ids ends the unit there. The
  mismatching reply forms its own single-message unit and is never
  folded into the assistant's unit.
- No objective: an input with no `RoleUser` message fails
  `ErrNoObjective`, whatever the budget headroom.
- Tail cap: a `RecentTail` of one drops every optional unit older than
  the newest; `MaxRecentTail` bounds the fill.
- Retention overflow: a mandatory set priced above `Budget()` fails
  `ErrRetentionOverflow`, with no kept-list truncation.
- Empty input fails `ErrNoMessages`; an estimator error fails
  `ErrEstimateFailed`; an invalid window fails `Window.Validate`.
- Idempotency: two `Compact` calls on equal input return equal `Key`,
  equal `Kept`, and equal `Dropped`. A second case differs in one
  tool call's arguments only; its key differs from the first case's.
- Unit integrity: no kept `RoleTool` message replies to a dropped
  assistant call.
- Calibrated concurrency, in `calibrated_test.go` or a sibling under
  the 500-line limit: goroutines call `EstimateTokens` and `Observe`
  on one shared `*Calibrated` under `go test -race`. No race, no
  panic, and the correction factor stays inside the clamp bounds after
  every join.

### Change verification

- `make verify` passes; `contextplan` holds the 85 coverage floor.
- `api/contextplan.txt` gains the compaction surface through `make
  api-update`, in the same change as the code.
- `go test -race ./contextplan/...` passes.
- `python3 scripts/check_plan.py`, `check_deps.py`, and
  `check_prose.py` pass. The `contextplan` row in
  `policy/layers.json` is unchanged; it already allows
  `contextstate` and `provider`.
- `docs/packages/contextplan.md` gains the compaction surface in the
  same change as the code.
- This change lands with or after the `provider` `Message.Name`
  change; `Compact` reads that field.
