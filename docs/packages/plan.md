# Package reference: context/plan

`context/plan` manages token budget windows, token calibration, and history compaction. It compacts a conversation history down to a target token budget while preserving critical context. The exported surface below mirrors `api/context/plan.txt`.

## Types

- `Window` — the token budget for one request. `MaxTokens` is the model context window. `Reserve` is the headroom held back for the reply. `Compaction` carries compaction thresholds and retention configuration.
- `Compaction` — the compaction thresholds and retention configuration: `TriggerPercent`, `TargetPercent`, `TargetTokens`, `RecentTail`, and `PreserveNames`. Zero values select sensible defaults.
- `CompactResult` — `Compact` outcome: `Kept`, `Dropped`, `BeforeTokens`, `AfterTokens`, `TriggerTokens`, `TargetTokens`, `Compacted`, and the idempotency `Key`.
- `Calibrated` — wraps a `provider.TokenEstimator` with an exponentially weighted moving average. Corrects estimates after each completed turn. Implements `provider.TokenEstimator`.

## Functions and methods

- `Window.Validate()` — rejects non-positive `MaxTokens`, negative `Reserve`, `Reserve` at or above `MaxTokens`, invalid `Compaction`, and positive `Compaction.TargetTokens` at or above `Budget()`.
- `Window.Budget()` — returns `MaxTokens - Reserve`.
- `Window.CompactTrigger()` — returns the trigger in tokens: `Budget` times `TriggerPercent`, floored.
- `Window.CompactTarget()` — returns the target in tokens: `TargetTokens` when positive, else `Budget` times `TargetPercent`, floored.
- `Compaction.Validate()` — rejects percentages outside `[0, 100]`, negative `TargetTokens` or `RecentTail`, `RecentTail` over `MaxRecentTail`, empty `PreserveNames`, and duplicate `PreserveNames`. When `TargetTokens` is zero, rejects `TargetPercent` at or above `TriggerPercent`.
- `Compact(msgs, w, e)` — compacts a message list. Fails on empty input (`ErrNoMessages`) or missing `RoleUser` message (`ErrNoObjective`). Passes through messages below the trigger. At or above trigger, preserves mandatory units and fills recent tail units up to the target.
- `Calibrate(est, alpha)` — wraps estimator `est` with an EWMA smoothing factor `alpha`. Falls back to `DefaultSmoothingFactor` when `alpha` is not in `(0, 1]`.
- `Calibrated.EstimateTokens(req)` — calls wrapped estimator and scales by current correction factor within `[MinCorrectionFactor, MaxCorrectionFactor]`.
- `Calibrated.Observe(estimated, actual)` — updates correction factor based on observed turn usage.

## Constants

- `DefaultSmoothingFactor` (0.3), `MinCorrectionFactor` (0.5), `MaxCorrectionFactor` (2.0) — EWMA bounds for calibration.
- `DefaultTriggerPercent` (100), `DefaultTargetPercent` (10) — default compaction percentages.
- `DefaultRecentTail` (8), `MaxRecentTail` (64) — tail fill bounds.
- `CompactionAlgorithm` ("context-compact-v1") — idempotency key algorithm tag.

## Failure modes

Use `errors.Is` to test these errors:
- `ErrMaxTokensNotPositive` — `Window.Validate` returns it when `MaxTokens <= 0`.
- `ErrReserveNegative` — `Window.Validate` returns it when `Reserve < 0`.
- `ErrReserveTooLarge` — `Window.Validate` returns it when `Reserve >= MaxTokens`.
- `ErrNoMessages` — `Compact` returns it for an empty message list.
- `ErrEstimateFailed` — `Compact` returns it when token estimation fails.
- `ErrRetentionOverflow` — `Compact` returns it when mandatory retention alone exceeds `Budget()`.
- `ErrNoObjective` — `Compact` returns it when no `RoleUser` message exists in the input.

## Invariants

- `Calibrated` correction factor never leaves `[MinCorrectionFactor, MaxCorrectionFactor]`.
- Compaction operates on atomic message units: an assistant call and its tool replies stay together.
- Tail fill preserves contiguous chronological order and never resumes after stopping.
- Compaction keys are deterministic and derived via `context/ref.Mint`.

## Cross-references

- [context/ref.md](context/ref.md) — canonical content address hashing.
- [provider.md](provider.md) — token estimator interfaces and message types.
- [contextsummary.md](contextsummary.md) — summary generation for dropped messages.
- [contextsession.md](contextsession.md) — durable session planning using `Window`.
