# Plan: durablefence

Status: shipped. Sequenced after `ledger` (phase 34; see
`docs/plans/ledger.md`), which wires this kit into
`ledger/ledger_test/scenario_test.go` as its named-caller proof.

### Amendment: close the missing happy-path Mutate check

A post-ship logic review found a real gap. No `Check*` function ever
asserted `Mutate` succeeds for a legitimate, non-fenced, currently held
token. Every existing check that calls `Mutate` only asserts a non-nil
error after a `Takeover` (`CheckTakeoverFencesPreviousOwner`,
`CheckTakeoverFencesConcurrentMutate`). A `Scenario` whose `Mutate`
field always returns an error, even for the correct current owner,
passed all six existing `RunAll` subtests. This section adds
`CheckMutateSucceedsForCurrentOwner` to close the gap, and reconciles
one doc-comment overclaim the same review found.

## Goal

Give `ledger` a shared, storage-agnostic conformance kit that proves
claim, takeover, and fence invariants hold, including the concurrent
case a hand-written sequential test cannot reach. `ledger` wires its
own `Claim`, `Takeover`, `Renew`, `Release`, and `State` into a
`Scenario` and runs the shipped check functions against it. A later
package with the same claim-and-fence shape reuses this harness
instead of writing its own copy of these checks.

## Scope

Inside: one new leaf package, `durablefence`. It defines `Scenario`, a
struct of caller-supplied functions against the implementation under
test, and a set of check functions that each take a `testing.TB`, a
`context.Context`, and a `Scenario`, and fail the test on a violated
invariant. It defines `RunAll`, which runs every check function in
sequence under `t.Run`. It defines `Scenario.Validate`, which reports
a missing field before any check runs.

Outside: any concrete claim, lease, or lock implementation, including
`ledger.Ledger` itself, except the
`ledger/ledger_test/scenario_test.go` wiring this plan's Verification
section requires. Any storage, network, or persistence code. Any
domain-specific naming tied to `flow`, `a2aclient`, or `ledger`. The
`Scenario` field names stay generic (`Claim`, `Mutate`, and so on) so
`ledger` and any later owner-token implementation adopt this harness
with no adapter type beyond the `Scenario` literal itself.

### Package name and location

`durablefence` is a new top-level package: one lowercase word, no
underscore, the same shape as `heartbeat`, `discovery`, and
`identity`. The name states the concern: a fence around a durable
claim. This package is not a `_test.go` helper file; it is an
importable package with its own directory, its own
`api/durablefence.txt` lock, and its own `policy/layers.json` row.

### Test-only: production code must not import this package

`durablefence` exists to run inside another package's `_test`
subdirectory, for example `ledger/ledger_test/scenario_test.go`. No
production `.go` file in any package may import it. Three points
enforce this:

- `durablefence/doc.go`'s first paragraph states the rule:
  `durablefence` is a conformance-test kit built on `testing.TB`, and
  no production code may import it.
- Every exported check function takes `testing.TB` as its first
  argument. This signals intent to a reader; it does not stop a
  production file from compiling `new(testing.T)`, since that only
  misbehaves at runtime.
- `scripts/check_deps.py` scans only direct, non-test `.go` files
  inside each top-level package directory. It never scans a nested
  `<pkg>_test` directory as a separate package, so no package's
  `policy/layers.json` row lists `durablefence`. A production
  top-level `.go` file that imports `durablefence` fails that scan. A
  production import placed inside a nested `_test` subdirectory has no
  automatic gate; catching it is human review's job.

### The `policy/layers.json` row

`durablefence` imports only `context`, `testing`, and `sync`, all
standard library. Its row is `"durablefence": []`, an empty allow
list, the same shape as `discovery`, `envelope`, `events`, and
`tools`. The row exists because `check_deps.py` requires every
top-level package directory with non-test `.go` files to have one, not
because any other package may import it. No other package's row
changes; no package gains `durablefence` as an allowed import, since
no production code may import it.

### The `Scenario` shape

`Scenario` carries six function fields, each justified by an
operation `ledger.Ledger` already ships:

- `Claim` — wraps `Ledger.Claim`. Grants a fresh owner token for an
  unheld resource.
- `Takeover` — wraps `Ledger.Takeover`. Reassigns ownership away from
  a stale owner and fences the old token out.
- `Mutate` — wraps `Ledger.Renew`, the operation an owner calls
  repeatedly during a lease and the one that must reject a stale,
  fenced token (`ledger.ErrFenced`). The field name stays generic: a
  caller adopting this kit for a different owner-mutating call (for
  example `Complete`) wires that call into `Mutate` instead, since the
  invariant under test, "a fenced token cannot mutate," applies to any
  owner action, not only `Renew`.
- `Release` — wraps `Ledger.Release`. Returns a held resource to the
  unheld state.
- `IsHeld` — reads the current hold state without mutating it. Against
  `Ledger`, a caller implements this by calling `Ledger.State` and
  checking the returned status is claimed.
- `IsFenced` — reports whether a specific token has been fenced out.
  Against `Ledger`, a caller implements this by calling `Mutate`
  (`Renew`) with the token and checking the error is `ErrFenced`.

Every field takes a `context.Context` first, matching this module's
convention on every context-carrying signature. Every field returns
an `error` last, so a caller's implementation reports a backend
failure separately from a fencing failure.

`Claim` and `Takeover` return an opaque owner token as a string. A
check function never inspects the token's shape; it only threads the
token returned by one call into the next call, so the harness stays
correct for any token encoding, from a UUID to a monotonic counter.

`ledger`'s own token type is not a string: `ledger.FenceToken` is a
`uint64` (`ledger/task_state.go`), and `Ledger.Claim` and
`Ledger.Takeover` return `(FenceToken, error)`. The `ledger` wiring in
`ledger/ledger_test/scenario_test.go` converts each returned
`FenceToken` to a string with
`strconv.FormatUint(uint64(fence), 10)` before storing it in the
`Scenario` literal's closures. A caller whose owner-token type is
already a string passes it through unchanged.

### Every check leaves its resource unheld on return

Every `Check*` function releases or clears its own hold before it
returns, on both the pass and the fail path, so the resource is
unheld again when the next `Check*` function runs. This contract
makes `RunAll`'s fixed check order safe: each check starts from
"unheld" because the previous one left it that way. A `Check*`
function that claims a resource and returns while still holding it
would break every check listed after it in `RunAll`'s order, since
`CheckClaimGrantsHold` runs first and the checks after it assume the
resource starts unheld.

## API

The surface below lands in `api/durablefence.txt` via
`make api-update`.

- `type Scenario struct` — six function fields, in this declaration
  order:
  - `Claim func(context.Context) (string, error)`
  - `Takeover func(context.Context) (string, error)`
  - `Mutate func(context.Context, string) error`
  - `Release func(context.Context, string) error`
  - `IsHeld func(context.Context) (bool, error)`
  - `IsFenced func(context.Context, string) (bool, error)`

  Every field is a plain function value, no method set, so a caller
  builds a `Scenario` literal directly against its own claim
  implementation with no adapter type.
- `func (s Scenario) Validate() error` — reports the first nil
  function field, wrapped in `ErrIncompleteScenario`. Every check
  function calls `Validate` first and calls `t.Fatal` on a non-nil
  result, so a check failing on `ErrIncompleteScenario` means the
  `Scenario` literal is missing a field, not that the implementation
  under test has a bug.
- `var ErrIncompleteScenario` — the sentinel `Validate` wraps. Test
  with `errors.Is`, matching every other sentinel in this module.
- `func CheckClaimGrantsHold(t testing.TB, ctx context.Context, s Scenario)`
  — claims a fresh resource and asserts `IsHeld` reports true
  afterward, proving a successful `Claim` is visible to a caller that
  only reads hold state. Also asserts `IsFenced` reports false for the
  just-claimed, still-active token, proving `IsFenced` does not default
  to true for a token that has never been fenced. Releases the hold
  before returning, so the resource is unheld for the next check.
- `func CheckReleaseClearsHold(t testing.TB, ctx context.Context, s Scenario)`
  — claims, releases, and asserts `IsHeld` reports false afterward,
  proving `Release` clears the hold so a later `Claim` can reclaim the
  resource. The resource is already unheld when this check returns,
  since the assertion itself happens after the release.
- `func CheckTakeoverFencesPreviousOwner(t testing.TB, ctx context.Context, s Scenario)`
  — claims with token A, takes over with token B, and asserts two
  things: `Mutate` under token A now fails, and `IsFenced` reports
  true for token A. This proves a subsequent `Mutate(A)` call, made
  after the takeover has already completed, is rejected. It does not
  prove the race case; `CheckTakeoverFencesConcurrentMutate` covers
  that. Releases the hold under token B before returning, so the
  resource is unheld for the next check.
- `func CheckTakeoverFencesConcurrentMutate(t testing.TB, ctx context.Context, s Scenario)`
  — the concurrency invariant this kit exists to test. Claims with
  token A, then starts two goroutines: one calls `Mutate(A)` in a loop
  until a barrier releases it, the other calls `Takeover` for token B
  once the first goroutine has issued at least one `Mutate(A)` call
  (synchronized through a channel, not a sleep). After both goroutines
  finish, asserts every `Mutate(A)` call that completed after
  `Takeover` returned success returned a non-nil error, and asserts
  `IsFenced` reports true for token A. The check asserts a non-nil
  error, not a fencing-specific one: `Scenario` carries no generic
  fencing-error sentinel a caller-supplied `Mutate` can be checked
  against, since the token type and the error type are both opaque to
  this kit. A correct implementation may let an in-flight `Mutate(A)`
  that was already committed before `Takeover` won the race succeed;
  the check only asserts no `Mutate(A)` call completing after
  `Takeover` returns success. Releases the hold under token B before
  returning, so the resource is unheld for the next check.
- `func CheckMutateSucceedsForCurrentOwner(t testing.TB, ctx context.Context, s Scenario)`
  — claims a fresh resource with token A and calls `Mutate(A)` before
  any `Takeover`, asserting the call returns nil. This proves the
  legitimate, currently held owner can mutate at all, closing the gap
  a `Mutate` that always errors, even for the correct owner, would
  otherwise slip past every other check in this kit: the other checks
  that call `Mutate` only assert it fails after a fencing event, never
  that it succeeds before one. Releases the hold under token A before
  returning, so the resource is unheld for the next check.
- `func CheckClaimRejectsWhileHeld(t testing.TB, ctx context.Context, s Scenario)`
  — claims a fresh resource with token A, then calls `Claim` again on
  the same resource and asserts the second call returns a non-nil
  error, proving `Claim` does not grant a second, independent hold
  over a resource token A already owns. Releases the hold under token
  A before returning, so the resource is unheld for the next check.
- `func CheckIsFencedFalseForUnknownToken(t testing.TB, ctx context.Context, s Scenario)`
  — calls `IsFenced` with a token the `Scenario` never issued through
  `Claim` or `Takeover`, and asserts the result is false, proving
  `IsFenced` reports the fenced state of a real prior owner, not a
  default "yes" for any token it does not recognize.
- `func RunAll(t testing.TB, ctx context.Context, s Scenario)` — runs
  every `Check*` function in alphabetical order by function name,
  under `t.Run` with the check's own name as the subtest name:
  `CheckClaimGrantsHold`, `CheckClaimRejectsWhileHeld`,
  `CheckIsFencedFalseForUnknownToken`,
  `CheckMutateSucceedsForCurrentOwner`, `CheckReleaseClearsHold`,
  `CheckTakeoverFencesConcurrentMutate`,
  `CheckTakeoverFencesPreviousOwner`. A caller wanting the full suite
  calls `RunAll` once; a caller wanting one invariant calls that check
  function directly. `RunAll` relies on every `Check*` function
  releasing its own hold before returning; that contract is why a
  fixed check order composes safely over one `Scenario` value.

`make api-update` is the authoritative source for the exact lock
text; the block below is the plan's best-effort rendering, matching
the field-declaration-order convention `api/flow.txt` already uses
for `Step`'s exported fields. The builder runs `make api-update` and
treats its output as the real lock, then reconciles this plan's block
with it if the two differ only in formatting. This amendment adds one
new exported function, `CheckMutateSucceedsForCurrentOwner`, so
`api/durablefence.txt` gains exactly one new line versus the currently
committed lock. The builder must run `make api-update` and commit the
`api/` diff in the same change, per the enforcement ladder in
`AGENTS.md`.

The expected lock content:

```text
package durablefence
  func (s Scenario) Validate() (error)
  func CheckClaimGrantsHold(t testing.TB, ctx context.Context, s Scenario)
  func CheckClaimRejectsWhileHeld(t testing.TB, ctx context.Context, s Scenario)
  func CheckIsFencedFalseForUnknownToken(t testing.TB, ctx context.Context, s Scenario)
  func CheckMutateSucceedsForCurrentOwner(t testing.TB, ctx context.Context, s Scenario)
  func CheckReleaseClearsHold(t testing.TB, ctx context.Context, s Scenario)
  func CheckTakeoverFencesConcurrentMutate(t testing.TB, ctx context.Context, s Scenario)
  func CheckTakeoverFencesPreviousOwner(t testing.TB, ctx context.Context, s Scenario)
  func RunAll(t testing.TB, ctx context.Context, s Scenario)
  type Scenario struct {
  Claim func(context.Context) (string, error)
  Takeover func(context.Context) (string, error)
  Mutate func(context.Context, string) error
  Release func(context.Context, string) error
  IsHeld func(context.Context) (bool, error)
  IsFenced func(context.Context, string) (bool, error)
}
  var ErrIncompleteScenario
```

## File layout

- `durablefence/doc.go` — package doc stating the test-only rule as
  its first paragraph, plus a file map.
- `durablefence/scenario.go` — `Scenario`, `Validate`,
  `ErrIncompleteScenario`.
- `durablefence/checks.go` — the seven `Check*` functions and
  `RunAll`. Split into a second file if the 500-line structure cap
  requires it.

## Tests

New test files live in `durablefence/durablefence_test/`, matching
the flat test layout in `docs/plans/agents/PHASES.md`. `durablefence`
is itself a test-helper package, so its own tests prove the harness is
correct, not that a runtime feature works.

- `durablefence/durablefence_test/reference_test.go` — a small,
  in-memory reference claim implementation, guarded by one
  `sync.Mutex`, used as the system under test for every case below. It
  tracks one current owner token and a monotonic counter for issuing
  new tokens. `Takeover` always succeeds and reassigns the owner
  regardless of the token passed in, matching a real fencing
  implementation's contract: a takeover does not require proof of the
  current owner.
- `scenario_test.go` — the red-green cases for `Validate`. Assertions
  come first; the builder confirms they fail against the empty
  package, then implements `Validate` to green. Cases: a fully
  populated `Scenario` (nil error), and one case per field left nil
  (six cases), each asserting `errors.Is(err, ErrIncompleteScenario)`.
- `checks_test.go` — one subtest per check function, run against the
  reference implementation from `reference_test.go`. Each proves the
  check passes on a correct implementation, and each proves the
  reference is unheld again after the check returns, by asserting
  `IsHeld` reports false once the subtest body finishes:
  - `CheckClaimGrantsHold` against a fresh reference claim; asserts
    the reference is unheld after the check returns.
  - `CheckClaimRejectsWhileHeld` against a fresh reference claim;
    asserts the reference is unheld after the check returns.
  - `CheckReleaseClearsHold` against a fresh reference claim.
  - `CheckIsFencedFalseForUnknownToken` against a fresh reference
    claim, using a token the reference never issued.
  - `CheckTakeoverFencesPreviousOwner` against a fresh reference
    claim, proving the reference's `Takeover` really fences the prior
    token out of `Mutate`; asserts the reference is unheld after the
    check returns.
  - `CheckTakeoverFencesConcurrentMutate` against a fresh reference
    claim, run under `go test -race`, proving the reference's mutex
    guard fences a concurrent `Mutate(A)` against an overlapping
    `Takeover(B)`, not only a sequential one; asserts the reference is
    unheld after the check returns.
  - `CheckMutateSucceedsForCurrentOwner` against a fresh reference
    claim; proves `Mutate(A)` returns nil for the current, non-fenced
    owner before any `Takeover`; asserts the reference is unheld after
    the check returns.
- `checks_negative_test.go` — proves each check function fails loud
  against a deliberately broken reference, not just that it passes on
  a correct one. Each case wraps the reference so one function field
  returns the wrong answer, wraps the check call in a nested
  `t.Run(name, func(t *testing.T) { CheckX(t, ctx, brokenScenario) })`,
  and asserts `t.Run`'s own returned `bool` is false. No fake or
  recording `testing.TB` is needed, and none would be possible, since
  `testing.TB` carries an unexported method only the `testing` package
  can satisfy:
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
  - A broken `IsFenced` that defaults to true for any active,
    still-held token it was not built to recognize, instead of only
    for a truly unknown one: the `CheckClaimGrantsHold` subtest fails,
    since that check now asserts `IsFenced` is false for the token it
    just claimed.
  - A broken `Mutate` that always returns an error, even for the
    correct, currently held, non-fenced owner: the
    `CheckMutateSucceedsForCurrentOwner` subtest fails. This is the
    exact reproduction the logic review used to prove the pre-amendment
    kit certified a broken backend as conformant: this broken
    `Mutate` still passed all six original checks, since none of them
    ever asserted `Mutate` succeeds outside a fencing scenario.
- `runall_integration_test.go` — calls `RunAll` once against the
  reference implementation and asserts no subtest failed, proving the
  full suite composes over one `Scenario` value with no field reused
  incorrectly between checks. This is the proof that the
  release-on-return contract holds end to end: each check starts from
  an unheld resource because the previous check in `RunAll`'s fixed
  order released its own hold.

No benchmark file. `durablefence` is a test-helper package that runs
inside another package's test binary; it ships no runtime code path
whose latency or allocation count a caller needs to measure. This
package never runs outside a test process, so a benchmark would
measure the reference fixture, not a shipped feature.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `durablefence` and for the total.
- The `durablefence` row lands in `policy/layers.json` as `[]`, before
  the code, matching this plan.
- No other package's `policy/layers.json` row changes. No package
  gains `durablefence` as an allowed import.
- `api/durablefence.txt` lands through `make api-update` in the same
  change as the code, matching the API section's lock content. This
  amendment adds one new line for `CheckMutateSucceedsForCurrentOwner`
  versus the lock committed before this amendment; the builder runs
  `make api-update` and commits the diff.
- `go test -race ./durablefence/...` passes.
- `durablefence/checks.go`'s doc comment on
  `CheckTakeoverFencesConcurrentMutate` changes from "observed a
  fencing error" to the reconciled non-nil-error wording in the API
  section above, in the same change as the new check.
- `checks_test.go` and `checks_negative_test.go` both gain coverage
  for `CheckMutateSucceedsForCurrentOwner`, proving it passes against
  the correct reference implementation and fails against a reference
  whose `Mutate` always errors, even for the current, non-fenced
  owner. This is the reproduction the logic review used to find the
  gap; the negative case must exist so a future regression on this
  check is caught the same way.
- This phase adds no conformance vector under
  `envelope/testdata/vectors/`. `durablefence` carries no wire format;
  it drives an in-memory claim implementation through function calls
  only.
- `docs/architecture.md` gains a `durablefence/` package-map entry,
  marked as a test-only leaf with no import edge to or from any other
  package, in the same change as the code.
- `AGENTS.md`'s Layout section gains a one-line `durablefence` entry,
  stating it is a leaf, test-only conformance kit, imported only from
  another package's `_test` subdirectory.
- `ledger`'s change also adds `ledger/ledger_test/scenario_test.go`,
  which builds a `Scenario` from `ledger.Ledger` and calls `RunAll`
  against it, alongside the existing `claim_race_test.go`.
  `go test -race ./ledger/...` passes with both files present. This is
  the named-caller proof phase 33 requires: `durablefence` ships no
  value until this wiring lands, so both changes land in the same
  review pass.
