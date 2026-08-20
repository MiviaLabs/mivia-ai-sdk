# Phase 86: Calibrated.Observe drops the shared-lastEst pairing

Status: plan, reviewed, not built. Depends on a companion change to
`agentloop`, landed in the same change; see the companion section
below.

## Why this plan exists

A bug audit of `contextplan` found a confirmed logic race: `Calibrated`
holds `lastEst`/`hasLast` as single shared fields, one mutex guarded,
so no data race in the Go race-detector sense, but a quietly wrong
calibration under concurrent or multi-estimate use. This phase closes
that gap.

## Goal

`Observe` must score the exact estimate its caller is reporting actual
usage for, never whatever estimate a `Calibrated`'s internal `lastEst`
field happens to hold at the moment `Observe` runs. Today
`EstimateTokens` (`contextplan/calibrated.go`) overwrites `lastEst` on
every call, and `Observe(actual int)` implicitly pairs `actual`
against whatever `lastEst` holds when it runs, not against the
estimate that produced the specific request the caller now reports on.
Two real callers break the single-estimate-then-single-observe
assumption this implicit pairing needs: `contextplan.Planner.Plan` and
`Compact` each call `EstimateTokens` many times per call, for trial
insertions and tail-fill probing, so `lastEst` holds an intermediate
candidate estimate, not the one describing the final accepted request;
and `agentloop.Loop.Run`, documented and tested to run concurrently on
one shared `*Loop` (`docs/plans/agentloop.md`'s Options.Window design;
`agentloop/agentloop_test/compaction_recovery_test.go`'s
`TestRunConcurrentSharedLoopWithPlanning`), shares one `*Calibrated`
across every goroutine, so one goroutine's `Observe` call can pair
with a different goroutine's in-flight estimate. No panic, no crash,
no race-detector flag; a quietly wrong calibration factor for every
caller sharing that `Calibrated`.

## Scope

Inside:

- `Calibrated.Observe`'s signature changes from `Observe(actual int)`
  to `Observe(estimated, actual int)`. Every call becomes
  self-contained: the caller passes both the estimate it is scoring
  and the real usage, so no implicit cross-call or cross-goroutine
  state pairing is possible.
- `estimated` is defined as exactly the value `EstimateTokens` already
  returned to that caller: the scaled, already-corrected figure, not
  the wrapped estimator's raw pre-scale count. `Calibrated` never
  exposes a raw count or a factor snapshot to any caller, so no other
  reading of `estimated` is possible or intended.
- `Observe`'s internal formula changes from divisive to multiplicative,
  matching the `estimated` definition above:
  `sample := actual / estimated`, then
  `next := factor * ((1-alpha) + alpha*sample)`, clamped to
  `[MinCorrectionFactor, MaxCorrectionFactor]` as before. A divisive
  formula (`sample := actual/estimated; next := (1-alpha)*factor +
  alpha*sample`) is wrong once `estimated` is the scaled figure: its
  fixed point is `sqrt(actual/raw)`, not `actual/raw`, so the
  correction factor would converge to the wrong steady state. The
  multiplicative formula's fixed point stays `factor* = actual/raw`,
  matching the pre-fix intent: at the first call `factor` is `1.0`, so
  `estimated == raw` and the first step is byte-for-byte the old
  formula's first step; later calls generalize correctly with no
  raw-tracking needed, since a scaled estimate that already tracks
  `actual` (`sample ≈ 1`) leaves `factor` unchanged. Independently
  verified: the recurrence reduces to `next = (1-alpha)*factor +
  alpha*(actual/raw)`, a linear contraction with ratio `(1-alpha)` for
  `alpha` in `(0, 1]`, converging geometrically to `factor* =
  actual/raw` from any starting factor.
- `Calibrated` drops the `lastEst` and `hasLast` fields. `Calibrated`
  keeps `mu`, `est`, `alpha`, and `factor` only; `mu` now guards
  `factor` alone.
- `EstimateTokens` drops its `c.lastEst = raw; c.hasLast = true`
  bookkeeping. It keeps calling the wrapped estimator and scaling by
  `factor` under the mutex; its signature is unchanged, since
  `provider.TokenEstimator` fixes it.
- `Observe`'s no-op rule changes to match the new signature: a
  non-positive `estimated` or a non-positive `actual` is a no-op. The
  "first call, before any estimate" no-op rule is removed; there is no
  longer a "before any estimate" state to detect, since every call
  carries its own estimate.
- Concurrent updates to the shared `factor` field stay correct with no
  further synchronization change: each `Observe` call is now a valid,
  self-contained EWMA step regardless of interleaving order between
  goroutines. Only the previous implicit pairing was wrong, not the
  idea of one shared calibration factor across concurrent callers.

Outside:

- Any change to `EstimateTokens`'s signature. It keeps satisfying
  `provider.TokenEstimator` unchanged.
- Any change to the EWMA clamp bounds or `Calibrate`'s constructor
  signature.
- Any caching of a `(estimate, request)` pairing inside `Calibrated`
  itself. The fix removes the implicit pairing; it does not replace it
  with an explicit one the package tracks. The caller now owns pairing
  its own estimate to its own actual, which is the only place that
  pairing is unambiguous.

## API

One breaking change to `api/contextplan.txt`, landed through `make
api-update` in the same change as the code:

```go
// Observe records one completed turn: estimated is the value
// EstimateTokens returned to the caller for that turn - the
// already-corrected figure, not a raw pre-scale count - and actual is
// the real provider.Usage.TotalTokens for the same turn. Observe
// corrects factor multiplicatively against estimated, so the fixed
// point is unchanged from before this fix: a corrected estimate that
// tracks actual. The result is clamped to [MinCorrectionFactor,
// MaxCorrectionFactor]. A non-positive estimated or a non-positive
// actual is a no-op.
func (c *Calibrated) Observe(estimated, actual int)
```

This replaces the locked `func (c *Calibrated) Observe(actual int)`
entry. `agentloop` is the only internal caller today
(`agentloop/run.go`) and needs a matching update; see the companion
change below.

## Companion change: `agentloop`

`agentloop/run.go`'s `runChat` builds `req := provider.Request{Model:
l.model, Messages: history, Tools: l.defs}` and calls
`l.completer.Chat(ctx, req)`. The estimate that describes this exact
request does not exist yet at that point when `l.window` is set,
because `l.calibrated.EstimateTokens` last ran during `planHistory`,
against the pre-compaction or pre-recovery-rebuild history, not
necessarily the same `req.Messages` `Chat` finally receives.

- Add an `estimatedTokens int` field to `chatAttempt`
  (`agentloop/run.go`).
- Inside `runChat`, when `l.calibrated != nil`, call
  `l.calibrated.EstimateTokens(req)` at the point `req` is built, and
  store the result on `chatAttempt.estimatedTokens`, before the `Chat`
  call. A non-nil `l.window` already requires a non-nil `l.calibrated`
  through `Options.Validate`'s `ErrEstimatorRequired` rule, so this
  call is safe to gate on `l.calibrated != nil` alone, covering both
  the windowed and the non-windowed-but-calibrated configurations. An
  estimate error here is non-fatal to the request: `Chat` still runs,
  `estimatedTokens` stays zero, and the later `Observe` call sees a
  non-positive `estimated` and no-ops, matching today's silent-degrade
  behavior for an estimator failure outside the planning step.
- At the call site in `run`, change `l.calibrated.Observe(resp.Usage.
  TotalTokens)` to `l.calibrated.Observe(at.estimatedTokens, resp.
  Usage.TotalTokens)`.
- This adds one `EstimateTokens` call per iteration when `l.calibrated`
  is set but `l.window` is nil, a configuration that did not call
  `EstimateTokens` at all before. One estimator call is far cheaper
  than one `Chat` call, so the added cost is small relative to the
  iteration it measures.
- `agentloop/compaction.go`'s `checkCompactedBudget` and
  `planHistory`'s own `EstimateTokens` calls need no change. Those are
  pure planning-time estimates that decide whether to compact; they
  never feed `Observe`, so the pairing fix does not touch them.
- `api/agentloop.txt` is unaffected: `chatAttempt` is unexported, and
  `Result`, `Loop`, and `Options` keep their locked shape. Confirm this
  with `make api-update` producing no `agentloop` diff.

## Tests

In `contextplan/contextplan_test/calibrated_test.go`:

- The reference formula every case below computes against, stated
  once so no case can pass against a divisive drift instead:
  `sample := actual / estimated`, then `next := factor *
  ((1-alpha) + alpha*sample)`, clamped to `[MinCorrectionFactor,
  MaxCorrectionFactor]`.
- Converges toward `actual`, not toward `sqrt(raw*actual)`: builds a
  real `provider.TokenEstimator` stub returning a fixed `raw` count,
  wraps it in `Calibrate`, and drives several turns where each turn
  calls the real `EstimateTokens(req)` to obtain `estimated` (never a
  test-author-chosen literal), then calls `Observe(estimated, actual)`
  with a fixed `actual` different from `raw`. Asserts the scaled
  estimate `EstimateTokens` returns on each later turn moves strictly
  closer to `actual` and converges near it within a bounded number of
  turns. This is the case that distinguishes the multiplicative
  formula from the divisive drift.
- `Observe(estimated, actual)` pairs correctly when called out of
  estimate order: two `EstimateTokens` calls run back to back, then
  two `Observe` calls run with each call's own remembered estimate, in
  reverse order from how the estimates were produced. The resulting
  factor matches the reference formula applied in the order the
  `Observe` calls actually ran, proving the factor depends only on the
  arguments passed to each call, not on call order or on any earlier
  `EstimateTokens` call.
- Zero and negative `estimated` is a no-op: `Observe(0, actual)` and
  `Observe(-5, actual)` leave `factor` unchanged.
- Zero and negative `actual` is a no-op, unchanged from today:
  `Observe(estimated, 0)` leaves `factor` unchanged.
- `Observe` with no prior `EstimateTokens` call at all still applies:
  a fresh `*Calibrated`'s first call is `Observe(100, 120)`, and the
  factor updates to the reference formula's result with `factor`
  starting at `1.0`, proving the removed "first call is a no-op" rule
  left no equivalent gate behind.
- `TestCalibratedConcurrentUse`, existing in this file: update its
  call sites to the new two-argument `Observe` and add an assertion
  beyond "no panic": run N goroutines, each holding its own known
  `(estimated, actual)` pair distinct from every other goroutine's
  pair, call `Observe` on each pair concurrently on one shared
  `*Calibrated`, then assert the resulting `factor` falls within the
  reachable range across every valid application order of the
  reference formula, since EWMA is order-dependent by construction.
  This replaces a same-name test that only checked absence of a panic
  and a call count; the replacement must be able to fail against the
  reverted, single-`lastEst` implementation.
- Delete or rewrite any existing case that asserts `Observe` no-ops
  "before any `EstimateTokens` call" as a named rule; that rule no
  longer exists once `Observe` takes its own estimate argument.

Companion tests in `agentloop/agentloop_test/`:

- A deterministic-estimate `Completer` and estimator pair, where
  `runChat` triggers `recoverPromptTooLong` for one iteration and
  succeeds normally for others. Assert the `Observe` call for the
  recovered iteration pairs with the recovery-path's own estimate, not
  the original pre-recovery estimate, by asserting the resulting
  factor matches the reference formula's result computed from the
  known estimate and actual, instead of only asserting no panic.
- `TestRunConcurrentSharedLoopWithPlanning`, existing in
  `compaction_recovery_test.go`: add an assertion on the shared
  `Calibrated`'s resulting factor, computed from the known, controlled
  `(estimated, actual)` pairs each goroutine's scripted `Completer`
  and estimator produce, proving the factor reflects correct pairing
  under concurrency, not merely the absence of a panic or a wrong call
  count.

## Verification

- `make verify` passes; `contextplan` and `agentloop` both hold the
  85 coverage floor.
- `api/contextplan.txt` changes through `make api-update`: the
  `Observe` entry's signature changes from one `int` parameter to two.
  This is a breaking, deliberate change to a locked entry.
- `api/agentloop.txt` produces no diff from `make api-update`; confirm
  this explicitly, since the companion change adds an unexported field
  only.
- `go test -race ./contextplan/... ./agentloop/...` passes.
- No `policy/layers.json` change: neither package's import edges move.
- `docs/packages/contextplan.md`'s `Calibrated.Observe(actual)` entry
  updates to the two-argument form, in the same commit as the code.
