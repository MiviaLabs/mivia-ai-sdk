# Package reference: durablefence

`durablefence` is a leaf, test-only conformance kit that proves claim,
takeover, and fence invariants against any claim-lease implementation,
including the concurrent case a hand-written sequential test cannot
reach. No production code may import it; it exists to run inside
another package's `_test` subdirectory. The exported surface below
mirrors `api/durablefence.txt`.

## Types

- `Scenario` — six caller-supplied function fields against the
  implementation under test: `Claim`, `Takeover`, `Mutate`, `Release`,
  `IsHeld`, `IsFenced`. Every field is a plain function value; a
  caller builds a `Scenario` literal with no adapter type. `Claim` and
  `Takeover` return an opaque owner token as a string; a check
  function never inspects the token's shape.

## Methods and functions

- `Scenario.Validate()` — reports the first nil field, wrapped in
  `ErrIncompleteScenario`. Every check function calls this first.
- `CheckClaimGrantsHold`, `CheckReleaseClearsHold`,
  `CheckTakeoverFencesPreviousOwner`,
  `CheckTakeoverFencesConcurrentMutate`,
  `CheckMutateSucceedsForCurrentOwner`, `CheckClaimRejectsWhileHeld`,
  `CheckIsFencedFalseForUnknownToken` — the seven invariant checks.
  Each takes `testing.TB`, a `context.Context`, and a `Scenario`, and
  fails the test on a violated invariant. Each releases its own hold
  before returning, on both the pass and the fail path.
- `RunAll(t, ctx, s)` — runs every `Check*` function in alphabetical
  order under `t.Run`, using each check's own name as the subtest
  name.

## Sentinel errors

- `ErrIncompleteScenario` — a `Scenario` field is nil. Test with
  `errors.Is`.

## Invariants

- Every check function releases or clears its own hold before it
  returns, so `RunAll`'s fixed check order composes safely: each
  check starts from "unheld" because the previous one left it that
  way.
- `CheckTakeoverFencesConcurrentMutate` asserts a non-nil error, not a
  fencing-specific one: `Scenario` carries no generic fencing-error
  sentinel a caller-supplied `Mutate` can be checked against, since
  the token type and the error type are both opaque to this kit.

## Cross-references

- [ledger.md](ledger.md) — `ledger`'s `ledger_test/scenario_test.go`
  wires a `Scenario` against `Ledger.Claim`, `Renew`, `Release`,
  `Takeover`, and `State`, converting `ledger.FenceToken` (a `uint64`)
  to a string token with `strconv.FormatUint`.

## Usage

```go
s := durablefence.Scenario{
    Claim:    func(ctx context.Context) (string, error) { /* ... */ },
    Takeover: func(ctx context.Context) (string, error) { /* ... */ },
    Mutate:   func(ctx context.Context, token string) error { /* ... */ },
    Release:  func(ctx context.Context, token string) error { /* ... */ },
    IsHeld:   func(ctx context.Context) (bool, error) { /* ... */ },
    IsFenced: func(ctx context.Context, token string) (bool, error) { /* ... */ },
}
durablefence.RunAll(t, context.Background(), s)
```
