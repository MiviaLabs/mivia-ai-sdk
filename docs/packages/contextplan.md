# Package reference: contextplan

`contextplan` fits one durable `contextstate.Session` into a bounded
`provider.Request`. It reads a session's source events newest to
oldest, decides what fits a token window and what does not, and
returns the built request plus the list of decisions it made. The
exported surface below mirrors `api/contextplan.txt`.

## Types

- `Planner` — fits one session's source events into a bounded
  request. Built only through `NewPlanner`. Safe for concurrent use
  through its three dependencies' own concurrency guarantees.
- `Window` — the token budget for one planned request. `MaxTokens` is
  the model's context window; `Reserve` is the headroom `Plan` never
  spends. `Compaction` carries the compaction thresholds and retention
  rules; its zero value means the defaults, never "disabled".
- `Compaction` — the compaction thresholds and retention
  configuration: `TriggerPercent`, `TargetPercent`, `TargetTokens`,
  `RecentTail`, `PreserveNames`. `TriggerPercent` zero means
  `DefaultTriggerPercent`; `TargetPercent` zero means
  `DefaultTargetPercent`; `RecentTail` zero means `DefaultRecentTail`.
- `CompactResult` — `Compact`'s output: `Kept`, `Dropped`,
  `BeforeTokens`, `AfterTokens`, `TriggerTokens`, `TargetTokens`,
  `Compacted`, and the idempotency `Key`.
- `PlanResult` — `Plan`'s output: the built `provider.Request`, every
  `Elision` decision `Plan` made, and the estimator's total over
  `Request.Messages`.
- `Elision` — one drop or trim decision `Plan` made for one payload.
  `Kept` is the byte length of a stubbed payload; zero means `Plan`
  inserted no message at all. `SpoolRef` is the `spool.Spool.Spool`
  reference for a successful durable write, set only for
  `ElisionReasonWindowOverflow` and `ElisionReasonRetentionExpired`
  when `Planner` carries a non-nil spooler and the write succeeded.
  Empty in every other case, including a failed write.
- `ElisionReason` — the closed set of reasons `Plan` drops or trims a
  payload: `ElisionReasonWindowOverflow`, `ElisionReasonRetentionExpired`,
  `ElisionReasonReasoningRedacted`, `ElisionReasonRevoked`.
  `ElisionReasonRevoked` is security-relevant, unlike the two
  budget-driven reasons beside it: a caller that ignores it gets a
  `Request` silently missing content its own store denied.
- `Calibrated` — wraps a `provider.TokenEstimator` with an
  exponentially weighted moving average, corrected after each
  completed turn through `Observe`. Implements `provider.TokenEstimator`.

## Functions and methods

- `NewPlanner(store, cache, spooler)` — builds a `Planner` over a
  `*contextstate.MemStore`, the durable payload source, a
  `*memory.Store`, a same-process decode cache, and a `*spool.Spool`,
  an optional durable overflow target. A nil `store` wraps
  `ErrNilStore`; a nil `cache` wraps `ErrNilCache`. A nil `spooler` is
  valid: `Plan` never calls `Spool.Spool` and behaves exactly as it
  does with no spooler wired.
- `Planner.Plan(ctx, sess, w, e)` — walks `sess.Source` newest to
  oldest. Every event resolves through one `contextstate.MemStore.Get`
  call on every `Plan` call: `resolvePayload` carries no cache-hit
  fast path, so a `Revoke` issued between two `Plan` calls on the same
  `Planner` is visible on the very next call. A revoked payload never
  enters `Request.Messages` and always produces an
  `ElisionReasonRevoked` entry, checked before the reasoning check. A
  reasoning event, per `IsReasoningEvent`, never enters
  `Request.Messages` and always produces an
  `ElisionReasonReasoningRedacted` entry. For every other event, `Plan`
  adds the decoded message while the running estimate stays at or
  under `w.Budget()`; once the next message would exceed the budget, a
  `RetentionCompliance` payload gets a stub instead, unless the stub
  itself would exceed the budget, in which case it drops too.
  `EstimatedTokens` stays at or under `w.Budget()` for a deterministic
  estimator whose empty-list total fits the budget. A larger fixed
  overhead exceeds it; an estimator that errors on the final call
  reports zero. Returns a
  non-nil error only on a malformed `Window`, a nil `sess`, or a
  payload-resolution failure other than a revocation. A wired `Spool`
  receives the full payload behind every `ElisionReasonWindowOverflow`
  and `ElisionReasonRetentionExpired` entry, keyed to
  `record.Ref.SubjectID`, best-effort, never failing `Plan`.
- `Window.Validate()` — rejects a non-positive `MaxTokens`, a negative
  `Reserve`, a `Reserve` at or above `MaxTokens`, an invalid
  `Compaction`, and a positive `Compaction.TargetTokens` at or above
  `Budget()`.
- `Window.Budget()` — returns `MaxTokens - Reserve`.
- `Window.CompactTrigger()` — returns the trigger in tokens: `Budget`
  times `TriggerPercent`, floored.
- `Window.CompactTarget()` — returns the target in tokens:
  `TargetTokens` when positive, else `Budget` times `TargetPercent`,
  floored.
- `Compaction.Validate()` — rejects percents outside `(0, 100]`, a
  negative `TargetTokens` or `RecentTail`, a `RecentTail` over
  `MaxRecentTail`, an empty but present `PreserveNames` entry, and
  duplicate `PreserveNames` entries. When `TargetTokens` is zero, a
  `TargetPercent` at or above the resolved `TriggerPercent` is
  rejected; a positive `TargetTokens` skips that comparison.
- `Compact(msgs, w, e)` — the pure compaction function over one
  message list. An invalid window fails `Window.Validate` before any
  estimate. Empty input fails `ErrNoMessages`; an input with no
  `RoleUser` message fails `ErrNoObjective`. A request below the
  trigger passes through with `Compacted` false. At or above the
  trigger, the mandatory retention set — the `RoleSystem` message at
  index zero, every unit with a preserved name, the latest `RoleUser`
  unit, and the latest complete assistant-plus-tool unit — survives;
  the tail fill adds contiguous units newest first, bounded by
  `RecentTail` messages and the target tokens, and stops at the first
  unit that breaks either bound. `Kept` preserves original order. The
  `Key` is `CompactionAlgorithm`, a colon, and `contextstate.Mint`
  over the canonical JSON fingerprint of the kept list.
- `Calibrate(est, alpha)` — wraps `est` with an EWMA correction
  factor. `alpha` is the smoothing weight in `(0, 1]`; a value outside
  that range, including zero or negative, falls back to
  `DefaultSmoothingFactor`.
- `Calibrated.EstimateTokens(req)` — calls the wrapped estimator, then
  scales the result by the current correction factor, always within
  `[MinCorrectionFactor, MaxCorrectionFactor]`.
- `Calibrated.Observe(actual)` — records one completed turn's real
  token count against the last estimate, and updates the correction
  factor by `alpha`, clamped to the same bounds. A first call before
  any estimate, and a non-positive `actual`, are both no-ops.
- `IsReasoningEvent(e)` — reports whether `e.Kind ==
  provider.ReasoningEventKind`.
- `StubContent(content)` — truncates `content` to `StubContentBytes`,
  appending an elision marker inside that cap when it truncates.
  Returns `content` unchanged when it already fits.
  `StubContentBytes` is a cap, not a promised length: the cut prefix
  drops every invalid UTF-8 byte, so the result may be shorter.

## Constants

- `DefaultSmoothingFactor` (0.3), `MinCorrectionFactor` (0.5),
  `MaxCorrectionFactor` (2.0) — the EWMA bounds `Calibrate` and
  `Observe` apply.
- `StubContentBytes` (256) — the byte cap `StubContent` truncates to.
- `DefaultTriggerPercent` (100), `DefaultTargetPercent` (10) — the
  compaction percents a zero `Compaction` field resolves to.
- `DefaultRecentTail` (8), `MaxRecentTail` (64) — the tail-fill
  message-count bound and its ceiling.
- `CompactionAlgorithm` ("context-compact-v1") — the idempotency-key
  fingerprint scheme name.

## Failure modes

Use `errors.Is` to test these.

- `ErrNilStore` — `NewPlanner` returns it when `store` is nil.
- `ErrNilCache` — `NewPlanner` returns it when `cache` is nil.
- `ErrNilSession` — `Plan` returns it when `sess` is nil.
- `ErrMaxTokensNotPositive` — `Window.Validate` returns it when
  `MaxTokens <= 0`.
- `ErrReserveNegative` — `Window.Validate` returns it when `Reserve <
  0`.
- `ErrReserveTooLarge` — `Window.Validate` returns it when `Reserve >=
  MaxTokens`.
- `ErrNoMessages` — `Compact` returns it for an empty message list.
- `ErrEstimateFailed` — `Compact` returns it wrapping the estimator's
  own failure.
- `ErrRetentionOverflow` — `Compact` returns it when the mandatory
  retention set alone estimates above `Window.Budget()`. No kept-list
  truncation happens; the result stays empty.
- `ErrNoObjective` — `Compact` returns it when no `RoleUser` message
  exists anywhere in the input, whatever the budget headroom.

## Invariants

- `Plan` resolves every event's full `contextstate.PayloadRecord`
  through the store on every call, before it decides anything,
  including a reasoning event and a payload it ends up fully
  dropping. No cache skips this round trip.
- A revoked payload never enters `Request.Messages`, regardless of
  budget or retention, and takes the revoked branch before the
  reasoning check.
- A reasoning event never enters `Request.Messages`, regardless of
  budget.
- Only a `RetentionCompliance` payload past budget gets a stub; every
  other payload past budget drops.
- `Calibrated`'s correction factor never leaves
  `[MinCorrectionFactor, MaxCorrectionFactor]`. One mutex guards
  `factor` and `lastEst`, so `EstimateTokens` and `Observe` are safe
  for concurrent use on one shared value.
- A unit — an assistant message with `ToolCalls` plus its contiguous
  matching replies, or one single message — is selected atomically. A
  `PreserveNames` match on any message of a unit selects the whole
  unit.
- The optional tail stays a contiguous suffix: the fill stops at the
  first unselected unit that breaks the message-count bound or the
  target, and never resumes past it.
- Repeated `Compact` calls on equal inputs return equal `Key` values;
  inputs differing in one tool call's arguments return different keys.
- No kept `RoleTool` message replies to a dropped assistant call:
  selection is per unit, never per message inside one.

## Cross-references

- [contextstate.md](contextstate.md) — `Planner` reads a
  `contextstate.Session` and its `MemStore`.
- [provider.md](provider.md) — `Plan` builds a `provider.Request`;
  `IsReasoningEvent` compares against `provider.ReasoningEventKind`.
- [memory.md](memory.md) — `Planner`'s decode cache.
- [spool.md](spool.md) — `Planner`'s optional durable overflow target
  for a budget-driven elision.
- [contextsummary.md](contextsummary.md) — the LLM summarizer whose
  input `Compact`'s `Dropped` list supplies.

## Usage

```go
planner, err := contextplan.NewPlanner(store, cache, spooler)
if err != nil {
    // a nil store or cache; spooler may be nil
}
window := contextplan.Window{MaxTokens: 8000, Reserve: 500}
result, err := planner.Plan(ctx, session, window, estimator)
if err != nil {
    // a malformed window, nil session, or payload-resolution failure
}
for _, e := range result.Elisions {
    // inspect what Plan dropped or stubbed, and why
    _ = e.Reason
    if e.SpoolRef != "" {
        // the full payload survives in spooler, retrievable by SubjectID
    }
}
```
