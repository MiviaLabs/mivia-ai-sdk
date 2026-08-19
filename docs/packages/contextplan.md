# Package reference: contextplan

`contextplan` fits one durable `contextstate.Session` into a bounded
`provider.Request`. It reads a session's source events newest to
oldest, decides what fits a token window and what does not, and
returns the built request plus the list of decisions it made. The
exported surface below mirrors `api/contextplan.txt`.

## Types

- `Planner` — fits one session's source events into a bounded
  request. Built only through `NewPlanner`. Safe for concurrent use
  through its two dependencies' own concurrency guarantees.
- `Window` — the token budget for one planned request. `MaxTokens` is
  the model's context window; `Reserve` is the headroom `Plan` never
  spends.
- `PlanResult` — `Plan`'s output: the built `provider.Request`, every
  `Elision` decision `Plan` made, and the estimator's total over
  `Request.Messages`.
- `Elision` — one drop or trim decision `Plan` made for one payload.
  `Kept` is the byte length of a stubbed payload; zero means `Plan`
  inserted no message at all.
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

- `NewPlanner(store, cache)` — builds a `Planner` over a
  `*contextstate.MemStore`, the durable payload source, and a
  `*memory.Store`, a same-process decode cache. A nil `store` wraps
  `ErrNilStore`; a nil `cache` wraps `ErrNilCache`.
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
  payload-resolution failure other than a revocation.
- `Window.Validate()` — rejects a non-positive `MaxTokens`, a negative
  `Reserve`, and a `Reserve` at or above `MaxTokens`.
- `Window.Budget()` — returns `MaxTokens - Reserve`.
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
  `[MinCorrectionFactor, MaxCorrectionFactor]`.

## Cross-references

- [contextstate.md](contextstate.md) — `Planner` reads a
  `contextstate.Session` and its `MemStore`.
- [provider.md](provider.md) — `Plan` builds a `provider.Request`;
  `IsReasoningEvent` compares against `provider.ReasoningEventKind`.
- [memory.md](memory.md) — `Planner`'s decode cache.

## Usage

```go
planner, err := contextplan.NewPlanner(store, cache)
if err != nil {
    // a nil store or cache
}
window := contextplan.Window{MaxTokens: 8000, Reserve: 500}
result, err := planner.Plan(ctx, session, window, estimator)
if err != nil {
    // a malformed window, nil session, or payload-resolution failure
}
for _, e := range result.Elisions {
    // inspect what Plan dropped or stubbed, and why
    _ = e.Reason
}
```
