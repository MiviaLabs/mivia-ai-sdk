# Phase 23: flow failure routing

Status: ready to build. Builds on phase 22. This phase adds the
fallback path. A run survives a step failure when a declared fallback
consumes it. Fail-fast stays the default. See
`docs/plans/agents/PHASES.md`.

## Goal

Let a step declare a fallback through admission over
`OutcomeFailed`. Give the fallback the failed step's ID and error.
Keep every failure without a fallback fatal.

## Scope

Inside: the third admission value, the failure context, the
continue-on-handled-failure rule, and the panel failure rule. Outside:
retries, compensation, and a `Catch`-style fallback field on `Step`.
Admission over `OutcomeFailed` is the single v1 mechanism. A declared
fallback field would duplicate the `Needs` edge and can drift from it.
No consumer needs the second form.

## API

- `AdmissionOnFailed` — the third `Admission` value. Admit when at
  least one need ended `OutcomeFailed`.
- `type Failure struct { Step string; Err error }` — the failure
  context a fallback step receives.
- `func FailureFrom(ctx context.Context) (Failure, bool)` — reads the
  failure context `Run` injects. The boolean is false outside a
  fallback firing.

`Failure.Step` names the first failed need in the step's `Needs`
declaration order. `Failure.Err` is that need's recorded error,
wrapped as `flow: step %q: %w`. A fallback with two failed needs
always resolves to the same one. The context carries the value; the
machine actions already receive `ctx`. No record field changes. No
machine change.

A wave member whose `Fire` did not error still becomes
`OutcomeFailed`; see the panel rule below. Its recorded error is the
joined wave error. A fallback that needs such a member receives that
joined error through `Failure.Err`.

### The continue rule

A `Fire` failure marks the step `OutcomeFailed`. `Run` then scans the
unresolved steps for one whose `Needs` names the failed step and whose
`When` is `AdmissionOnFailed`. At least one such fallback means the
failure is handled; the run continues. No fallback means the run
aborts with the step error, exactly as phases 21 and 22 do.

A fallback step is an ordinary step for the status walk. It fires its
own transition from the current status. The failed step never moved
the status, so the fallback picks its row from the status the last
executed step left.

Before `Run` fires a step admitted through a failed need, it injects
the `Failure` into `ctx`. The step's `Guard`, `OnExit`, and `OnEntry`
read it with `FailureFrom`. A step admitted without a failed need gets
no value; `FailureFrom` returns false.

A dependent of the failed step whose `When` is `AdmissionOnFinished`
or `AdmissionOnSucceeded` is never admitted. It becomes
`OutcomeSkipped` when its needs are terminal. A handled failure
therefore skips the happy-path dependents and runs the fallback path.

### The handler-skip re-check

A handled failure keeps a pending handler set: the unresolved
`AdmissionOnFailed` steps that made it handled. A handler can still be
skipped later, when a branch step leaves it unchosen. Skipping the
last pending handler of a handled failure aborts the run with the
recorded step error. A declared handler that never runs would
otherwise consume the failure silently. This re-check preserves
fail-fast. The panel skip shape is unreachable here: `New` rejects
failure-admitted panel members.

The other abort policies are unchanged. A `Confirm` rejection aborts
the run; a fallback never catches it. A `pickTransition` failure
aborts the run; no fallback can fix a missing transition row. A
`Route` error marks the branch step `OutcomeFailed`; a fallback may
catch it like any failure. The stall error is unchanged.

`New` gains two validations, with pinned messages:

- `flow: step %q admits on failure but needs nothing` — an
  `AdmissionOnFailed` step with no needs. A root always admits, so
  the rule would be dead weight.
- `flow: panel %d names failure-admitted step %q` — an
  `AdmissionOnFailed` step inside a panel. The wave passes one shared
  `ctx` into every member goroutine, so a per-member failure value has
  no clean home. Rejecting the shape mirrors phase 22's routed-step
  rejection. A fallback may still need a panel member; it may not be
  one.

### Panels

A wave with any member failure marks every member `OutcomeFailed`.
The current status and record stay at their pre-wave values, as
today. The run continues only when every failed member has at least
one pending `AdmissionOnFailed` dependent. Otherwise the run aborts
with the joined error, as today.

## Tests

Test files live in `flow/flow_test/`:

- `fallback_tdd_test.go` — the red-green cases. Red step: the file
  does not compile on the empty phase, because `AdmissionOnFailed` and
  `FailureFrom` do not exist. Record the compiler error as the red.
  Cases:
  - `New` rejects an `AdmissionOnFailed` root, pinned message.
  - `New` rejects an `AdmissionOnFailed` panel member, pinned message.
  - A failed step with a fallback lets the run complete. Assert the
    failed step, the fallback, and the join outcomes.
  - `FailureFrom` inside the fallback's `OnEntry` returns the failed
    step's ID and a wrapped error that satisfies `errors.Is`.
  - A fallback with two failed needs receives the first failed need in
    `Needs` declaration order through `Failure.Step`.
  - A fallback that needs an error-free wave member receives the
    joined wave error through `Failure.Err`.
  - `FailureFrom` inside a happy-path step returns false.
  - A failed step with no fallback aborts with the pinned error
    wording from phase 5.
  - An `AdmissionOnFailed` step whose needs all succeeded becomes
    `OutcomeSkipped`. Its own dependents follow normal admission.
  - A happy-path dependent of the failed step becomes
    `OutcomeSkipped` when the failure is handled.
  - A branch step that leaves the sole handler of a handled failure
    unchosen aborts the run with the recorded step error.
  - A `Confirm` rejection aborts even when a fallback exists.
  - A wave failure with a fallback for every failed member continues.
  - A wave failure with a fallback for only one failed member aborts
    with the joined error.
  - A `Route` error on a branch step with a fallback continues down
    the fallback path.
- `fallback_integration_test.go` — run a graph end to end: a step with
  a rejecting guard, a fallback that records its `Failure`, and a
  final join. Assert the report outcomes and the final status. Assert
  the fallback read the failed step's ID. Run the confirm-rejection
  case and assert the run aborts. Run the race detector over a panel
  failure case.
- `fallback_perf_test.go` — benchmark the failure-plus-fallback path
  against the all-success path on the same graph. Measure the
  all-success baseline on the phase 22 code before this phase lands.
  Record both in the file's leading comment. Report the ratio. Set no
  allocation budget. Error wrapping and the context value injection
  vary with the wave's goroutine count, so `PHASES.md` permits
  reporting the allocs/op ratio instead.

## Verification

`make verify` passes. Run `go test -race ./...` for the panel failure
path. The coverage floor for `flow` holds. `api/flow.txt` gains the
`AdmissionOnFailed` constant, the `Failure` type, and `FailureFrom`
via `make api-update`. Commit the `api/` diff in the same change.
`policy/layers.json` is unchanged. `api/machine.txt` is unchanged.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change. Describe the fallback path and the
failure context. `AGENTS.md` updates its `flow/` layout bullet in the
same change; the bullet names the outcome, admission, route, and
fallback vocabulary.
