# Phase 33: durable-fence conformance kit

Status: ready to build, sequenced after phase 34
(`docs/plans/agents/phase34_ledger.md`). Phase 34 names this phase as
its own conformance harness and cites this file directly; this phase
names phase 34's `Ledger` as its concrete named caller in return. The
two phases are not independent: phase 34 lands first, since `ledger`
proves its own invariants with a hand-written `claim_race_test.go`
and needs no harness to exist. This phase lands immediately after,
and its first proof of correctness is wiring a `Scenario` against the
shipped `Ledger` in `ledger/ledger_test/`, replacing or supplementing
`claim_race_test.go` with the shared harness. `durablefence` ships no
value until that wiring lands, so the two changes land in the same
review pass, or this phase's own change includes the `ledger`-facing
`Scenario` wiring as its acceptance proof.

## Goal

Give `ledger` (phase 34) a shared, storage-agnostic test harness that
proves its claim, takeover, and fence invariants hold, including the
concurrent case a hand-written sequential test cannot reach. `ledger`
wires its own `Claim`, `Takeover`, `Renew`, `Release`, and `State`
into a `Scenario` and runs the shipped check functions against it. A
later package with the same claim-and-fence shape reuses the same
harness instead of writing its own copy of these checks.

## Scope

Inside: one new leaf package, `durablefence`. It defines `Scenario`, a
struct of caller-supplied functions against the implementation under
test, and a set of check functions that each take a `testing.TB`, a
`context.Context`, and a `Scenario`, and fail the test on a violated
invariant. It also defines `RunAll`, which runs every check function
in sequence, and `Scenario.Validate`, which reports a missing field
before any check runs.

Outside: any concrete claim, lease, or lock implementation, including
`ledger.Ledger` itself, except the
`ledger/ledger_test/scenario_test.go` wiring required by Verification,
below. Any storage, network, or persistence code. Any domain-specific
naming tied to `flow`, `a2aclient`, or `ledger`, with the same single
exception. The
`Scenario` field names stay generic (`Claim`, `Mutate`, and so on) so
`ledger` and any later owner-token implementation adopt the same
harness with no adapter type beyond the `Scenario` literal itself.

### Package name and location

`durablefence` is a new top-level package, matching this module's
naming rule: one lowercase word, no underscore, same shape as
`heartbeat`, `discovery`, and `identity`. The name states the concern
plainly: a fence around a durable claim. The alternative name,
`fencetest`, was considered and rejected. This package is not a
`_test.go` file's helper; it is an importable package with its own
directory, its own `api/durablefence.txt` lock, and its own
`policy/layers.json` row, so the `test` suffix would only restate what
the "test-only" doc comment already says, and this repository has no
existing package that carries a `test` suffix in its name.

### This package is test-only; production code must not import it

`durablefence` exists to run inside another package's `_test`
subdirectory, for example a future `ledger/ledger_test/claim_test.go`.
No production `.go` file in any package may import it. Three points
enforce this:

- The package doc comment in `durablefence/doc.go` states the rule in
  its first paragraph: `durablefence` is a conformance-test kit, built
  on `testing.TB`, and no production code may import it.
- Every exported check function takes a `testing.TB` as its first
  argument. This is not a compile-time gate: `new(testing.T)` compiles
  from any `.go` file, including a production file, and only
  misbehaves at runtime when a method on it runs outside a real test
  binary. The `testing.TB` argument signals intent to a reader; it
  enforces nothing on its own.
- `scripts/check_deps.py` scans only the direct, non-test `.go` files
  inside each top-level package directory; it does not scan a nested
  `<pkg>_test` directory as a separate package. A future package's
  `_test` subdirectory that imports `durablefence` never needs a
  `policy/layers.json` edge added to that package's own row, because
  the gate never inspects that file. This plan records the rule here
  since the gate stays silent about it: a reviewer must catch a
  production import by reading the diff, not by running the deps gate.
  Put together, the only real, automatic enforcement is
  `check_deps.py`'s scan of top-level, non-test files: no package's
  `policy/layers.json` row lists `durablefence`, so a production
  top-level `.go` file that imports it fails that scan. A production
  import placed inside a nested `_test` subdirectory (which the scan
  does not inspect) has no automatic gate at all; catching it is
  human review's job, not the compiler's or the gate's.

### The `policy/layers.json` row

`durablefence` imports only `context`, `testing`, and `sync`, all
standard library. Its own row is `"durablefence": []`, an empty allow
list, the same shape as `discovery`, `envelope`, `events`, and
`tools`. The row exists because `scripts/check_deps.py` requires every
top-level package directory with non-test `.go` files to have one,
regardless of whether any other package's production code is allowed
to import it. No other package's row changes; no package gains
`durablefence` as an allowed import, because no production code may
import it, per the point above.

### The `Scenario` shape

`Scenario` carries six function fields, each justified by an
operation `ledger.Ledger` (phase 34) already plans to ship:

- `Claim` — wraps `Ledger.Claim`. Grants a fresh owner token for an
  unheld resource.
- `Takeover` — wraps `Ledger.Takeover`. Reassigns ownership away from
  a stale owner and fences the old token out.
- `Mutate` — wraps `Ledger.Renew`. `Renew` is the operation phase 34
  names as the one an owner calls repeatedly during a lease, and the
  one that must reject a stale, fenced token
  (`docs/plans/agents/phase34_ledger.md`'s `ErrFenced` case). A
  caller who adopts this kit for a different owner-mutating call
  (`Complete`, for example) wires that call into `Mutate` instead;
  the field name stays generic because the invariant it tests, "a
  fenced token cannot mutate," applies to any owner action, not only
  `Renew`.
- `Release` — wraps `Ledger.Release`. Returns a held resource to the
  unheld state.
- `IsHeld` — reads the current hold state without mutating it. Against
  `Ledger`, a caller implements this by calling `Ledger.State` and
  checking the returned `Status` is `StatusClaimed`.
- `IsFenced` — reports whether a specific token has been fenced out.
  Against `Ledger`, a caller implements this by calling `Mutate`
  (`Renew`) with the token and checking the error is `ErrFenced`.

Every field takes a `context.Context` first, matching this module's
convention on every other context-carrying signature. Every field
returns an `error` last, so a caller's implementation can report a
backend failure separately from a fencing failure.

`Claim` and `Takeover` return an opaque owner token as a string. A
check function never inspects the token's shape; it only threads the
token returned by one call into the next call, so the harness stays
correct for any token encoding an implementation chooses, from a
UUID to a monotonic counter.

## API

The surface below lands in `api/durablefence.txt` via `make
api-update`.

- `type Scenario struct` — six function fields: `Claim`, `Takeover`,
  `Mutate`, `Release`, `IsHeld`, `IsFenced`. Every field is a plain
  function value, no method set, so a caller builds a `Scenario`
  literal directly against its own claim implementation with no
  adapter type.
- `func (s Scenario) Validate() error` — reports the first nil
  function field, wrapped in `ErrIncompleteScenario`. Every check
  function calls `Validate` first and calls `t.Fatal` on a non-nil
  result, so a caller seeing a check fail on `ErrIncompleteScenario`
  knows the `Scenario` literal is missing a field, not that the
  implementation under test has a bug.
- `var ErrIncompleteScenario` — the sentinel `Validate` wraps. Test
  with `errors.Is`, matching every other sentinel in this module.
- `func CheckClaimGrantsHold(t testing.TB, ctx context.Context, s Scenario)`
  — claims a fresh resource and asserts `IsHeld` reports true
  afterward, proving a successful `Claim` is visible to a caller that
  only reads hold state.
- `func CheckReleaseClearsHold(t testing.TB, ctx context.Context, s Scenario)`
  — claims, releases, and asserts `IsHeld` reports false afterward,
  proving `Release` clears the hold so a later `Claim` can reclaim the
  resource.
- `func CheckTakeoverFencesPreviousOwner(t testing.TB, ctx context.Context, s Scenario)`
  — claims with token A, takes over with token B, and asserts two
  things: `Mutate` under token A now fails, and `IsFenced` reports
  true for token A. This proves a *subsequent* `Mutate(A)` call, made
  after the takeover has already completed, is rejected. It does not
  prove the race case; `CheckTakeoverFencesConcurrentMutate` below
  covers that.
- `func CheckTakeoverFencesConcurrentMutate(t testing.TB, ctx context.Context, s Scenario)`
  — the concurrency invariant this kit exists to test. Claims with
  token A, then starts two goroutines: one calls `Mutate(A)` in a
  loop until a barrier releases it, the other calls `Takeover` for
  token B once the first goroutine has issued at least one `Mutate(A)`
  call (synchronized through a channel, not a sleep). After both
  goroutines finish, asserts every `Mutate(A)` call that returned
  after the `Takeover` call observably succeeded returned a
  fencing error, and asserts `IsFenced` reports true for token A. A
  correct implementation may let an in-flight `Mutate(A)` that was
  already committed before the `Takeover` won the race succeed; the
  check only asserts that no `Mutate(A)` call completing after the
  `Takeover` call returns success. This is the invariant
  `CheckTakeoverFencesPreviousOwner` cannot reach, since that check
  never overlaps the two calls in time.
- `func CheckClaimRejectsWhileHeld(t testing.TB, ctx context.Context, s Scenario)`
  — claims a fresh resource with token A, then calls `Claim` again on
  the same resource and asserts the second call returns a non-nil
  error, proving `Claim` does not grant a second, independent hold
  over a resource token A already owns.
- `func CheckIsFencedFalseForUnknownToken(t testing.TB, ctx context.Context, s Scenario)`
  — calls `IsFenced` with a token the `Scenario` never issued through
  `Claim` or `Takeover`, and asserts the result is false, proving
  `IsFenced` reports the fenced state of a real prior owner, not a
  default "yes" for any token it does not recognize.
- `func RunAll(t testing.TB, ctx context.Context, s Scenario)` — runs
  every check function above, in the order listed, under `t.Run` with
  the check's own name as the subtest name. A caller wanting the full
  suite calls `RunAll` once instead of naming every check by hand; a
  caller wanting one invariant calls that check function directly.

The expected lock content:

```text
package durablefence
  func CheckClaimGrantsHold(t testing.TB, ctx context.Context, s Scenario)
  func CheckClaimRejectsWhileHeld(t testing.TB, ctx context.Context, s Scenario)
  func CheckIsFencedFalseForUnknownToken(t testing.TB, ctx context.Context, s Scenario)
  func CheckReleaseClearsHold(t testing.TB, ctx context.Context, s Scenario)
  func CheckTakeoverFencesConcurrentMutate(t testing.TB, ctx context.Context, s Scenario)
  func CheckTakeoverFencesPreviousOwner(t testing.TB, ctx context.Context, s Scenario)
  func RunAll(t testing.TB, ctx context.Context, s Scenario)
  func (s Scenario) Validate() (error)
  type Scenario struct {
}
  var ErrIncompleteScenario
```

## Tests

New test files live in `durablefence/durablefence_test/`, matching the
flat test layout in `docs/plans/agents/PHASES.md`. `durablefence` is
itself a test-helper package, so its own tests prove the harness is
correct, not that a runtime feature works.

- `durablefence/durablefence_test/reference_test.go` — a small, in-memory
  reference claim implementation, guarded by one `sync.Mutex`, used as
  the system under test for every case below. It tracks one current
  owner token and a monotonic counter for issuing new tokens.
  `Takeover` always succeeds and reassigns the owner regardless of the
  token passed in, matching a real fencing implementation's contract:
  a takeover does not require proof of the current owner.
- `scenario_test.go` — the red-green cases for `Validate`. Assertions
  come first; the builder confirms they fail against the empty
  package, then implements `Validate` to green. Cases: a fully
  populated `Scenario` (nil error), and one case per field left nil
  (six cases), each asserting `errors.Is(err, ErrIncompleteScenario)`.
- `checks_test.go` — one subtest per check function, run against the
  reference implementation from `reference_test.go`. Each proves the check
  passes on a correct implementation:
  - `CheckClaimGrantsHold` against a fresh reference claim.
  - `CheckClaimRejectsWhileHeld` against a fresh reference claim.
  - `CheckReleaseClearsHold` against a fresh reference claim.
  - `CheckIsFencedFalseForUnknownToken` against a fresh reference
    claim, using a token the reference never issued.
  - `CheckTakeoverFencesPreviousOwner` against a fresh reference
    claim, proving the reference's `Takeover` really fences the prior
    token out of `Mutate`.
  - `CheckTakeoverFencesConcurrentMutate` against a fresh reference
    claim, run under `go test -race`, proving the reference's mutex
    guard fences a concurrent `Mutate(A)` against an overlapping
    `Takeover(B)`, not only a sequential one.
- `checks_negative_test.go` — proves each check function fails loud
  against a deliberately broken reference, not just that it passes on
  a correct one. A harness that always passes proves nothing. Each
  case wraps the reference so one function field returns the wrong
  answer, then wraps the check call itself in a real, nested
  `t.Run(name, func(t *testing.T) { CheckX(t, ctx, brokenScenario) })`
  and asserts `t.Run`'s own returned `bool` is false. `t.Run` already
  reports whether the subtest and everything inside it passed; no
  fake or recording `testing.TB` implementation is needed, and none
  would be possible, since `testing.TB` carries an unexported method
  only the `testing` package can satisfy:
  - A broken `Takeover` that does not fence the previous token: the
    `CheckTakeoverFencesPreviousOwner` subtest fails.
  - A broken `Takeover` that does not fence a token racing a
    concurrent `Mutate`: the `CheckTakeoverFencesConcurrentMutate`
    subtest fails.
  - A broken `Release` that does not clear the hold: the
    `CheckReleaseClearsHold` subtest fails.
  - A broken `Claim` that grants a second hold over an already-held
    resource: the `CheckClaimRejectsWhileHeld` subtest fails.
  - A broken `Claim` that does not set the hold: the
    `CheckClaimGrantsHold` subtest fails.
  - A broken `IsFenced` that reports true for a token that was never
    issued: the `CheckIsFencedFalseForUnknownToken` subtest fails.
- `runall_integration_test.go` — calls `RunAll` once against the
  reference implementation and asserts no subtest failed, proving the
  full suite composes over one `Scenario` value with no field reused
  incorrectly between checks.

No benchmark file. `durablefence` is a test-helper package that runs
inside another package's test binary; it ships no runtime code path
whose latency or allocation count a caller needs to measure. This is
not the `PHASES.md` allocation-budget exception for non-deterministic
overhead; the plain reason is that this package never runs outside a
test process, so a benchmark would measure the reference fixture, not
a shipped feature.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `durablefence` and for the total.
- The `durablefence` row lands in `policy/layers.json` as `[]`, before
  the code, matching this plan.
- No other package's `policy/layers.json` row changes. No package
  gains `durablefence` as an allowed import.
- `api/durablefence.txt` lands through `make api-update` in the same
  change as the code, matching the API section's lock content.
- `go test -race ./durablefence/...` passes.
- The builder creates `docs/plans/durablefence.md` from
  `docs/plans/TEMPLATE.md` before the code lands, since
  `scripts/check_plan.py` requires a plan for every top-level package
  directory that has `.go` files. This phase plan is the design
  contract `docs/plans/durablefence.md` restates in the package's own
  words, the same way `docs/plans/heartbeat.md` stated a fresh
  package's plan directly, with no prior phase to reference.
- `docs/architecture.md` gains a `durablefence/` package-map entry,
  marked as a test-only leaf with no import edge to or from any other
  package, in the same change as the code.
- `AGENTS.md`'s Layout section gains a one-line `durablefence` entry,
  stating it is a leaf, test-only conformance kit, imported only from
  another package's `_test` subdirectory.
- This phase adds no conformance vector under `envelope/testdata/
  vectors/`. `durablefence` carries no wire format; it drives an
  in-memory claim implementation through function calls only.
- `ledger` (phase 34) has already landed before this phase starts.
  This phase's change also adds `ledger/ledger_test/scenario_test.go`,
  which builds a `Scenario` from `ledger.Ledger` and calls `RunAll`
  against it, alongside the existing `claim_race_test.go`. `go test
  -race ./ledger/...` passes with both files present. This is the
  named-caller proof the Status section requires; the phase is not
  done until this file exists and passes.
