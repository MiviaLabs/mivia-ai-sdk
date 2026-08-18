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

New file `flow/failure.go` holds the continue rule, the pending-handler
bookkeeping, and the `Failure` type plus `FailureFrom`. `flow/runner.go`
sits at 470 of 500 lines before this phase; the added failure-kind
tagging, panel-failure marking, and context injection would push it
over the cap. `flow/routing.go` keeps the admission-verdict change;
`flow/failure.go` calls into it but does not duplicate it.

`flow/failure.go` owns every new function and type this phase adds:
the unexported context-key type `failureContextKey`, `failureKind`,
`failureError`, `newFailureError`, `withFailure`, `failureForStep`,
`fireStep`, `confirmStep`, `pickTransitionFor`, `resolveCatchable`,
`resolvePanelFailure`, `prunePendingHandler`, `prunePendingOnRoute`,
`handledFailure`, `admitsOnFailed`, and `validateFailureAdmission`.
This list is exhaustive. No other new function or type lands in
`flow/runner.go` or elsewhere.

`flow/runner.go` changes in place. `Run`'s single-step fast path and
`advanceGroup`'s `scanSingleton` and `scanPanel` cases gain the
`pending` parameter and the continue-rule calls described below.
`nextReadyGroup` is unchanged. `runWave` changes too: it tags its own
shared `pickTransition` failure as `failureKindTransition`, so
`advanceGroup` can short-circuit a panel's shared-transition failure
to an uncatchable abort before that failure ever reaches
`resolvePanelFailure`; see Panels below. `runSingleton` and
`runSingletonAndMark` shrink to thin callers into `flow/failure.go`'s
`fireStep`, `confirmStep`, `pickTransitionFor`, and `failureForStep`,
and both gain a `pending` parameter; see Context injection below.
`fireFromChild` also changes: it calls the same `pickTransitionFor`
and `fireStep` helpers instead of calling `pickTransition` and
`m.Fire` directly, so a chained step's own parent-transition failure,
fired after its child completes, is tagged and catchable the same way
a straight-line step's failure is. `runChild` itself is unchanged; the
nested `Run` call it makes is still a separate frame with its own
`pending`, and its returned error still passes through `runSingleton`
unwrapped, per the uncatchable-child-failure rule below.

`runSingleton` and `runSingletonAndMark` lose lines from the
extraction above. `flow/runner.go`'s net line count this phase goes
down, not up, which is what keeps it under the cap without raising
it.

A fallback step's own `Fire` can fail like any other step's. Its
failure follows the same continue rule: a dependent `AdmissionOnFailed`
step may catch it. A fallback step may itself be an `AdmissionOnFailed`
target for another step's failure. `New` places no special restriction
on a fallback step; it is an ordinary step everywhere except the panel
and no-`Needs` rejections already listed below.

An `AdmissionOnFailed` step may also be a `Route` branch step. `Route`
runs after `Fire` succeeds, so a fallback that reaches `Route` already
ran; `Route` then behaves exactly as phase 22 describes. `New` places
no added restriction here.

## API

- `AdmissionOnFailed` — the third `Admission` value. Admit when at
  least one need ended `OutcomeFailed`.
- `type Failure struct { Step string; Err error }` — the failure
  context a fallback step receives.
- `func FailureFrom(ctx context.Context) (Failure, bool)` — reads the
  failure context `Run` injects. The boolean is false outside a
  fallback firing.

### Admission semantics for `AdmissionOnFailed`

`AdmissionOnFailed` is a whole-`Needs`-set predicate, not a per-need
rule. `admissionVerdict` in `flow/routing.go` today runs one `admits`
call per need and requires every need to pass: an all-of rule.
`AdmissionOnFailed` inverts this to an any-of rule: once every need
has resolved, the step admits when at least one resolved need is
`OutcomeFailed`, no matter what the other needs resolved to.

`admissionVerdict` gains a special case at its top, before the
per-need loop: when `s.When == AdmissionOnFailed`, it delegates to a
new sibling function `admitsOnFailed(s.Needs, outcomes) verdict` and
returns that result directly, skipping the per-need `admits` loop
entirely. `admitsOnFailed` returns `verdictWait` while any need is
unresolved, `verdictAdmit` when at least one resolved need is
`OutcomeFailed`, and `verdictSkip` otherwise (every need resolved and
none failed). The existing per-need loop and `admits` stay unchanged
for `AdmissionOnFinished` and `AdmissionOnSucceeded`.

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

### Context injection

`flow/failure.go` defines an unexported context-key type
`failureContextKey struct{}`, and `func withFailure(ctx
context.Context, f Failure) context.Context`, which stores `f` under
that key with `context.WithValue`. `FailureFrom` reads the same key
back with a type assertion on the stored value and reports its second
return value as false when the key holds nothing.

`flow/failure.go` also defines `func failureForStep(step Step, pending
map[string]*handledFailure) (Failure, bool)`. It scans `step.Needs` in
declaration order. For each need, it looks up `pending[need]`. The
first hit returns that entry's recorded `Failure` and `true`. No hit
after the full scan returns the zero `Failure` and `false`.
`failureForStep` is the function the Goal names: it picks "the first
failed need in the step's Needs declaration order" when a step's
`Needs` names two pending, failed steps. A nil `pending` map behaves
like an empty one; a read on a nil map never panics in Go.

The call chain from `Run` down to the point `ctx` reaches `m.Fire`,
named at every hop:

- `Run` declares `pending := make(map[string]*handledFailure)`
  immediately after `outcomes`, before the `len(d.steps) == 0` and
  `len(d.steps) == 1` fast-path checks. See the handler-skip re-check
  below for why `pending` lives there.
- `Run`'s `len(d.steps) == 1` fast path now calls
  `runSingletonAndMark(ctx, m, cur, rec, d.steps[0], confirm, bus,
  outcomes, pending)`, passing the same freshly made, empty `pending`
  map. A single-step `Definition` cannot declare `Needs`: no other
  step exists for a `Needs` entry to name. `validateFailureAdmission`'s
  no-needs rejection (below) already bars an `AdmissionOnFailed` step
  from being that lone step, so `pending` stays empty for the whole
  fast path and `failureForStep` never finds a hit there.
- `Run`'s main loop passes `pending` into `advanceGroup` on every
  call, alongside `outcomes`.
- `advanceGroup`'s `scanSingleton` case and its `scanPanel`
  one-member case call `runSingletonAndMark(ctx, m, cur, rec, next,
  confirm, bus, outcomes, pending)`. `pending` is
  `runSingletonAndMark`'s ninth parameter.
- `runSingletonAndMark` passes `pending` through unchanged to
  `runSingleton(ctx, m, cur, rec, step, confirm, bus, pending)`.
  `pending` is `runSingleton`'s eighth parameter.
- `runSingleton`, as its first action, calls `failureForStep(step,
  pending)`. On a hit, `runSingleton` builds `fireCtx :=
  withFailure(ctx, fail)`. On a miss, `fireCtx := ctx`. `runSingleton`
  uses `fireCtx`, not the original `ctx`, for every remaining call it
  makes on the step's own transition: `pickTransitionFor`, `fireStep`,
  and, in the `Sub` branch, `fireFromChild`. A step outside every
  pending entry's `Needs` gets `fireCtx == ctx`, unchanged.
- In the straight-line branch, `runSingleton` calls `rows :=
  m.AllowedTransitions(cur)`, then `row, err :=
  pickTransitionFor(step, rows, machine.Status(step.To))`, then `cur,
  rec, err = fireStep(fireCtx, m, cur, rec, step, row)`. `fireStep`
  calls `m.Fire(fireCtx, cur, row.Trigger, rec)` directly. `fireCtx`
  is the `ctx` value the fired transition's `Guard`, `OnExit`, and
  `OnEntry` receive, and the one `FailureFrom` reads inside them.
  `runSingleton` then calls `confirmStep(ctx, confirm, step)` with the
  original `ctx`; `Confirm` is not a transition action and does not
  read `FailureFrom`.
- In the `Sub` branch, `runSingleton` calls `runChild(ctx, step.Sub,
  m, confirm)` with the original `ctx`, unchanged: the child
  workflow's own steps are not the step admitted through a failed
  need, so they get no injected `Failure`. After `runChild` returns,
  `runSingleton` calls `fireFromChild(fireCtx, m, cur, rec, step,
  child)`, the parent-transition fire for `step` itself, so `fireCtx`
  reaches the fallback step's own transition actions the same way the
  straight-line branch does. `fireFromChild` calls `rows :=
  m.AllowedTransitions(cur)`, then `pickTransitionFor(step, rows,
  child)`, then `fireStep(fireCtx, m, cur, rec, step, row)`, mirroring
  the straight-line branch. `runSingleton` then calls `confirmStep`
  with the original `ctx`, same as the straight-line branch.

A step admitted without a failed need never reaches `withFailure`, so
`fireCtx == ctx` for it, and `FailureFrom` returns false inside its
transition's actions, matching the API doc comment.

### Catchable and uncatchable failures

`runSingleton` in `flow/runner.go` currently returns one plain `error`
for three distinct failure sources: a `Fire` failure, a `Confirm`
rejection, and a `pickTransition` failure. This phase must let `Run`
tell them apart, because only a `Fire` failure and a `Route` error are
catchable by a fallback; a `Confirm` rejection and a `pickTransition`
failure are never catchable.

`flow/failure.go` defines an unexported `failureKind int` with three
values: `failureKindFire`, `failureKindConfirm`, and
`failureKindTransition`, plus a constructor `newFailureError(kind
failureKind, err error) *failureError`. `runSingleton`'s straight-line
branch (no `Sub`) splits into three named helpers, all in
`flow/failure.go`, that each own one failure source: `fireStep` (calls
`m.Fire`, wraps errors as `failureKindFire`), `confirmStep` (calls
`confirm`, wraps errors as `failureKindConfirm`), and
`pickTransitionFor` (calls `pickTransition`, wraps errors as
`failureKindTransition`). Each helper returns a `*failureError`
wrapping the kind and the underlying error; `failureError` satisfies
`error` and unwraps to the underlying error through `errors.Is`.
`runSingleton` calls `pickTransitionFor` then `fireStep` then
`confirmStep` in sequence and returns the first `*failureError`
unchanged.

`runSingleton`'s `Sub` branch has three failure sources of its own:
`runChild`'s nested `Run` call, `fireFromChild`'s `pickTransition`
call, and `fireFromChild`'s `m.Fire` call, plus the branch's own
`confirmStep` call after `fireFromChild` returns. `fireFromChild`
reuses the same `pickTransitionFor` and `fireStep` helpers the
straight-line branch uses, so its `pickTransition` failure is
`failureKindTransition` and its `m.Fire` failure is `failureKindFire`,
tagged the same way and catchable the same way. The `Sub` branch's own
`confirmStep` call after a successful `fireFromChild` is
`failureKindConfirm`, same as the straight-line branch.

`runChild`'s error is different: it comes from a nested `Run` call
over the child `Definition`, a separate frame with its own `outcomes`
and its own `pending` map. Any failure inside that child frame already
ran through the child's own continue rule; a child failure a child
fallback caught never reaches the parent at all. An error `runChild`
returns already exhausted the child's own catch logic, so `runSingleton`
passes it through unwrapped, never inside a `*failureError`. A parent-
level `resolveCatchable` call, driven by `errors.As`, cannot match an
unwrapped error, so the parent frame always aborts on it: a chained
step's failure is never catchable by a parent-level fallback in this
phase. Fallback-catchability for a chained step's own failure is out
of scope for this phase.

`runSingletonAndMark` in `flow/runner.go` inspects the returned error
with `errors.As` into `*failureError`. It marks the step `OutcomeFailed`
on any error, tagged or not, and returns the error unchanged; it does
not decide catchability itself. `advanceGroup` decides catchability;
see the continue rule below.

### The continue rule

`flow/failure.go` defines `resolveCatchable(err error, failedID
string, steps []Step, outcomes map[string]Outcome, pending
map[string]*handledFailure) (error, bool)`. It runs `errors.As(err,
&fe)` for a `*failureError`. When `fe` is missing, or its kind is
`failureKindConfirm` or `failureKindTransition`, `resolveCatchable`
returns `(err, false)`: uncatchable. When the kind is
`failureKindFire`, it scans the unresolved steps in `steps` for one
whose `Needs` names `failedID` and whose `When` is
`AdmissionOnFailed`. No match returns `(err, false)`. At least one
match writes a `pending[failedID]` entry (see the handler-skip
re-check below) and returns `(nil, true)`: handled.

`advanceGroup`'s `scanSingleton` case now reads:

```
cur, rec, err := runSingletonAndMark(ctx, m, cur, rec, next, confirm, bus, outcomes, pending)
if err != nil {
    resolved, handled := resolveCatchable(err, next.ID, steps, outcomes, pending)
    if handled {
        return cur, rec, nil
    }
    return cur, rec, resolved
}
if next.Route == nil {
    return cur, rec, nil
}
if rerr := applyRoute(ctx, next, cur, rec, steps, outcomes); rerr != nil {
    outcomes[next.ID] = OutcomeFailed
    resolved, handled := resolveCatchable(newFailureError(failureKindFire, rerr), next.ID, steps, outcomes, pending)
    if handled {
        return cur, rec, nil
    }
    return cur, rec, resolved
}
if aerr := prunePendingOnRoute(next, steps, outcomes, pending); aerr != nil {
    return cur, rec, aerr
}
return cur, rec, nil
```

`runSingletonAndMark` gains a `pending` parameter. After it marks
`step.ID`'s outcome (`OutcomeSucceeded` or `OutcomeFailed`), it calls
`prunePendingHandler(pending, step.ID)`, defined in `flow/failure.go`:
for every `pending` entry whose `handlers` set contains `step.ID`, it
deletes that whole entry. A handler that ran, whichever way it
resolved, already ran; the failure it caught can never lose its only
runner. `prunePendingHandler` never touches `handlers` sets for a
handler ID it does not find; two failures never share a `pending`
entry, so settling one failure's handler never touches another
failure's entry.

`prunePendingOnRoute(next Step, steps []Step, outcomes
map[string]Outcome, pending map[string]*handledFailure) error`, also
in `flow/failure.go`, runs after a successful `applyRoute` call. It
walks `next`'s direct dependents and, for each one `applyRoute` marked
`OutcomeSkipped`, removes that dependent's ID from every `pending`
entry's `handlers` set. When an entry's `handlers` set becomes empty,
`prunePendingOnRoute` returns that entry's recorded `Failure.Err` at
once, without checking the remaining dependents; `advanceGroup`
returns that error and the run aborts. A declared handler that a
`Route` exclusion removed would otherwise consume the failure
silently.

`advanceGroup`'s `scanPanel` case, for a one-member group, calls
`runSingletonAndMark` and then `resolveCatchable` the same way. For a
group of two or more, a `runWave` error goes to `resolvePanelFailure`
instead; see Panels below.

`advanceGroup` is the function that decides abort versus continue: it
returns `nil` in the handled case and the original error in the
unhandled case. `Run`'s own loop keeps its existing `if err != nil {
return ... }` unchanged; it never sees a handled failure's error,
because `advanceGroup` already turned it into `nil` before returning.

On a handled `failureKindFire` failure from `runSingletonAndMark`,
`advanceGroup` returns at once, before it reaches the `next.Route`
check. A caught `Fire` failure never reaches `applyRoute`: `Route`
only makes sense after a successful `Fire`, and the failed step's
`cur`/`rec` are stale. This holds regardless of whether `next.Route`
is `nil` or not.

A fallback step is an ordinary step for the status walk. It fires its
own transition from the current status. The failed step never moved
the status, so the fallback picks its row from the status the last
executed step left.

Before `Run` fires a step admitted through a failed need, it injects
the `Failure` into `ctx`, through `runSingleton`'s `fireCtx`
computation; see Context injection above for the full call chain and
the named function at every hop.

A dependent of the failed step whose `When` is `AdmissionOnFinished`
or `AdmissionOnSucceeded` is never admitted. It becomes
`OutcomeSkipped` when its needs are terminal. A handled failure
therefore skips the happy-path dependents and runs the fallback path.

### The handler-skip re-check

`Run` owns a new local map, `pending map[string]*handledFailure`, keyed
by the failed step's ID. `handledFailure` (defined in
`flow/failure.go`) holds the recorded `Failure` for that step plus a
`handlers map[string]bool`: the set of unresolved `AdmissionOnFailed`
step IDs that made the failure handled. See Context injection above
for exactly where `Run` declares `pending` and every call site that
threads it through. `nextReadyGroup` does not take `pending`: it
only computes a `scanResult` from `steps`, `panels`, and `outcomes`,
and the pending-handler scan and pruning are `resolveCatchable`,
`resolvePanelFailure`, and `advanceGroup` operations, not
`nextReadyGroup` ones.

`resolveCatchable` and `resolvePanelFailure` write a new `pending`
entry the moment a catchable failure gets at least one
`AdmissionOnFailed` dependent (the continue rule above). The entry's
`handlers` set starts with every such dependent's ID.

`prunePendingOnRoute`, called from `advanceGroup` after a successful
`applyRoute` (see the continue rule above), owns the only removal path
from a `handlers` set: it fires only for a dependent `applyRoute`
marked `OutcomeSkipped`. When an entry's `handlers` set becomes empty
there, `advanceGroup` aborts the run at once with that entry's
recorded `Failure.Err`. A declared handler that a `Route` exclusion
removed would otherwise consume the failure silently. This re-check
preserves fail-fast.

The singleton skip path (`scanSkipSingleton`) and the panel skip path
(`scanSkipPanel`) never resolve a pending handler to `OutcomeSkipped`,
so neither needs a removal call. `scanSkipPanel` cannot: `New` rejects
a failure-admitted panel member, so no handler ID can ever appear in a
`scanSkipPanel` group. `scanSkipSingleton` cannot, for the parallel
reason: a pending handler's `Needs` always includes the failed step
ID, and that need is already, permanently, `OutcomeFailed` the moment
the handler enters `pending`. `admitsOnFailed` returns `verdictSkip`
only when every need has resolved and none is `OutcomeFailed`; a
pending handler can never meet that condition, because its failed need
never un-fails. Once a handler is in `pending`, `admitsOnFailed` can
only ever return `verdictWait` (while another need is unresolved) or
`verdictAdmit` (once every need is resolved); it never returns
`verdictSkip` for that handler. The re-check therefore only ever
needs the `Route` skip path, matching the Tests section, which
exercises the `Route` skip abort but no singleton-skip abort.

Once a pending entry's handler runs (`OutcomeSucceeded` or
`OutcomeFailed`, not `OutcomeSkipped`), `prunePendingHandler`, called
from `runSingletonAndMark` (see the continue rule above), deletes that
entry from `pending`: the failure has a running handler, so it can
never lose its last one. Two failures never share a `pending` entry;
each is keyed by its own failed step ID, so resolving or skipping one
failure's handlers never touches another failure's `handlers` set.

The other abort policies are unchanged. A `Confirm` rejection
(`failureKindConfirm`) aborts the run; a fallback never catches it. A
`pickTransition` failure (`failureKindTransition`) aborts the run; no
fallback can fix a missing transition row. A `Route` error
(`failureKindFire`) marks the branch step `OutcomeFailed`; a fallback
may catch it like any failure. The stall error is unchanged.

`flow/failure.go` defines `validateFailureAdmission(steps []Step,
panels []Panel, ids map[string]int) error`, with two pinned messages:

- `flow: step %q admits on failure but needs nothing` — an
  `AdmissionOnFailed` step with no needs. A root always admits, so
  the rule would be dead weight.
- `flow: panel %d names failure-admitted step %q` — an
  `AdmissionOnFailed` step inside a panel. The wave passes one shared
  `ctx` into every member goroutine, so a per-member failure value has
  no clean home. Rejecting the shape mirrors phase 22's routed-step
  rejection. A fallback may still need a panel member; it may not be
  one.

`New` in `flow/definition.go` calls `validateFailureAdmission` right
after `validateRouting` and before `validatePanelChains`: both
`validateRouting` and `validateFailureAdmission` check panel-membership
restrictions over the `ids` map `validateSteps` and `validatePanels`
already filled, so the two panel-restriction checks sit next to each
other in the call sequence.

### Panels

`runWave` resolves the shared transition row once, before any
goroutine spawns (see its doc comment). Today a `pickTransition`
failure there returns a plain `errorf("panel: %w", err)`; a per-member
`Fire` failure inside a goroutine returns its own plain, per-member
wrapped error, and the group's member failures join with
`errors.Join`. Both shapes reach `advanceGroup`'s `scanPanel` case as
one plain `error` today; nothing distinguishes them.

This phase changes `runWave`'s shared-transition failure only. It
wraps that one failure as `newFailureError(failureKindTransition,
errorf("panel: %w", err))` in place of the plain `errorf` call. A
member `Fire` failure inside the wave stays a plain, per-member
wrapped error, joined the same way as today. `runWave` does not call
`fireStep` for a member's `Fire`: a member's `Fire` runs inside its
own goroutine over a per-member `InOut` copy, not the single-step
transition `fireStep` was built for. Only the one shared, pre-spawn
transition failure gets the `*failureError` tag.

`advanceGroup`'s `scanPanel` case, for a group of two or more members,
inspects the `runWave` error before it decides what to call next. It
runs `errors.As(err, &fe)`. When `fe != nil`, the failure is the
shared transition failure: it happened before any member's `Guard`,
`OnExit`, `OnEntry`, or `Fire` ran. `advanceGroup` returns the error
unchanged at once, marking no member's outcome, matching today's
behavior for a pre-spawn failure. No fallback can catch it; this
mirrors the singleton rule that "a `pickTransition` failure aborts the
run; no fallback can fix a missing transition row."
`resolvePanelFailure` never runs for this case.

When `fe == nil`, at least one member's `Fire` actually ran and
failed; `runWave` never tags a member `Fire` failure, so a nil `fe`
can only mean this. This phase adds `resolvePanelFailure(err error,
group []Step, steps []Step, outcomes map[string]Outcome, pending
map[string]*handledFailure) (error, bool)` to `flow/failure.go`.
`advanceGroup`'s `scanPanel` case calls it in this branch only, in
place of returning the error directly. `resolvePanelFailure` first
marks every member of `group` `OutcomeFailed` in `outcomes`, before it
evaluates the continue rule. The current status and record stay at
their pre-wave values, as today.

After marking, `resolvePanelFailure` builds one `pending` entry per
failed member that has at least one `AdmissionOnFailed` dependent,
using the joined wave error as that member's `Failure.Err` (see the
API section above). It returns `(nil, true)` only when every failed
member has at least one such dependent; `advanceGroup` then returns
`(cur, rec, nil)`, and the run continues. Otherwise
`resolvePanelFailure` returns `(err, false)`; `advanceGroup` returns
the joined error unchanged, and no `pending` entry survives, matching
today's abort behavior.

## Tests

Test files live in `flow/flow_test/`:

- `fallback_test.go` — the red-green cases. Red step: the file
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
  - A panel's shared `pickTransition` failure, which fails before any
    member's `Fire` runs, aborts uncatchably even when every member
    has an `AdmissionOnFailed` fallback declared. Assert no member's
    outcome got marked and no `pending` entry survives. Distinguishes
    this pre-spawn failure from the per-member wave-failure cases
    above.
  - A chained step's own nested `Run` call fails inside its child
    workflow, with a parent-level `AdmissionOnFailed` dependent
    declared for that chained step. The run aborts uncatchably: assert
    the dependent never ran, the returned error is not a
    `*failureError`, and no `pending` entry exists.
  - A `Route` error on a branch step with a fallback continues down
    the fallback path.
  - A fallback with MIXED needs, one `OutcomeFailed` and one
    `OutcomeSucceeded`, still admits. Pins the any-of rule against the
    all-of rule the other admission values keep.
  - A fallback step's own `Fire` fails, and a second, nested fallback
    admitted on that failure lets the run complete. Pins that a
    fallback is an ordinary step with no restriction on declaring its
    own fallback.
  - An `AdmissionOnFailed` step is also a `Route` branch step: its own
    `Fire` succeeds, `Route` then excludes one dependent, the kept
    dependent runs. Pins that `Route` on a fallback works exactly as
    phase 22 describes.
  - Two independent failed steps, each with its own fallback and its
    own pending-handler set. Skipping the last handler of the first
    failure aborts the run; assert the second failure's still-pending
    handler played no part in that abort and never itself ran. Pins
    that `pending` bookkeeping does not leak across failures.
- `fallback_integration_test.go` — run a graph end to end: a step with
  a rejecting guard, a fallback that records its `Failure`, and a
  final join. Assert the report outcomes and the final status. Assert
  the fallback read the failed step's ID. Run the confirm-rejection
  case and assert the run aborts. Run the race detector over a panel
  failure case.
- `fallback_bench_test.go` — benchmark the failure-plus-fallback path
  against the all-success path on the same graph. Measure the
  all-success baseline on the phase 22 code before this phase lands.
  Record both in the file's leading comment. Report the ratio. Set no
  allocation budget. Error wrapping and the context value injection
  vary with the wave's goroutine count, so `PHASES.md` permits
  reporting the allocs/op ratio instead.

## Verification

`make verify` passes. Run `go test -race ./...` for the panel failure
path. The coverage floor for `flow` holds, including the new
`flow/failure.go` file. `api/flow.txt` gains the `AdmissionOnFailed`
constant, the `Failure` type, and `FailureFrom` via `make api-update`.
`failureContextKey`, `failureKind`, `failureError`, `newFailureError`,
`withFailure`, `failureForStep`, `fireStep`, `confirmStep`,
`pickTransitionFor`, `resolveCatchable`, `resolvePanelFailure`,
`prunePendingHandler`, `prunePendingOnRoute`, `handledFailure`,
`admitsOnFailed`, and `validateFailureAdmission` stay unexported; none
appear in the lock. Commit the `api/` diff in
the same change. `policy/layers.json` is unchanged. `api/machine.txt`
is unchanged. `scripts/check_structure.py` must pass on the changed
`flow/runner.go` and the new `flow/failure.go`.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change. Describe the fallback path and the
failure context. `AGENTS.md` updates its `flow/` layout bullet in the
same change; the bullet names the outcome, admission, route, and
fallback vocabulary.
