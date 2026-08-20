# Package reference: flow

The flow package is the declarative workflow building block. It owns
the step graph, the cycle check, and the runner. A workflow is data,
not code. The graph has steps and panels. `Run` walks the graph and
moves a `machine.Definition` one step at a time. A step named in no
panel runs alone; a step named in a panel runs as part of that
panel's wave, in a goroutine, together with every other member.
The exported surface below mirrors `api/flow.txt`.

## Types

- `Step` — one node in a workflow graph. A step has an ID, a list of
  prerequisite step IDs, a target status string, a payload, an
  optional `PayloadFrom` payload resolver, an optional nested
  `Definition`, an `Admission` rule, an optional `Route`, an optional
  `Retry`, and an optional `Loop`. A step with no prerequisites is a
  root. For a chained step, `To` is ignored by `Run` and may be empty;
  the child final status supplies the target status. A step with a
  non-nil `Route` is a branch step. A step with a non-nil `Retry`
  retries its own `Fire` call. See Retry below. A step with a non-nil
  `Loop` repeats its `Sub` child workflow before its own transition
  and `Confirm` fire. See Loop below.
- `PayloadFrom` — a step's run-time payload resolver. The runner calls
  it against the live record immediately before the step's `Confirm`
  call. The resolved value rides the `Step` value handed to `Confirm`
  through `Payload`. A nil `PayloadFrom` leaves `Step.Payload`
  untouched. `flow.New` rejects a step that sets both `Payload` and
  `PayloadFrom`, and a `PayloadFrom` on a member of a panel of two or
  more members; a one-member panel keeps the field, because `Run` still
  calls `Confirm` for its single member.
- `Panel` — a group of step IDs that run together in parallel. The
  runner schedules a panel as one wave. A panel is a named list of
  strings.
- `Definition` — a validated step graph and its panels. The fields are
  unexported. Callers read the roots through `Roots`.
- `Confirm` — the ack gate a caller supplies to `Run`. It signs the
  form `func(ctx context.Context, step Step) error`. A nil return
  means the ack confirmed.
- `Admission` — the rule that admits a step once every one of its
  needs is terminal. `AdmissionOnSucceeded`, the zero value, admits
  only when every need ended `OutcomeSucceeded`. `AdmissionOnFinished`
  is the explicit opt-in for skip tolerance: it admits when every need
  ended `OutcomeSucceeded` or `OutcomeSkipped`. `AdmissionOnFailed` admits once every need is
  terminal and at least one ended `OutcomeFailed`; it is an any-of
  rule, unlike the all-of rule the other two values use. A step with
  this rule is a fallback.
- `Route` — the routing function of a branch step. It signs the form
  `func(ctx context.Context, cur machine.Status, rec machine.InOut)
  ([]string, error)`. It receives the branch step's post-fire status
  and record, and returns the IDs of the direct dependents the run
  keeps; every other direct dependent skips at once.
- `Outcome` — the terminal state of one step: `OutcomeSucceeded`,
  `OutcomeFailed`, or `OutcomeSkipped`. Admission, route exclusion, and
  a whole-panel skip each produce `OutcomeSkipped`.
- `Report` — the result `Run` returns: the final status, the final
  record, and every resolved step's `Outcome`. The fields are
  unexported. Callers read them through `Status`, `Record`, `Outcome`,
  and `Outcomes`.
- `Checkpoint` — the full resumable state of a `Run`: `Status`
  (`machine.Status`), `Record` (`machine.InOut`), `Done` (the
  lexicographically sorted step IDs of every step that resolved
  `OutcomeSucceeded` so far), `Skipped` (the same for
  `OutcomeSkipped`), and `Failed` (the same for `OutcomeFailed`,
  whether or not a fallback caught the failure). Each list's order is
  a sort, not a completion order. `Encode` and `Decode` round-trip a
  `Checkpoint` through JSON; the caller owns storage. See `Run`'s
  `onCheckpoint` parameter and `Resume`.
- `Failure` — the failed step's context a fallback step receives:
  `Step` names the failed step, `Err` is its recorded error.
- `RetryPolicy` — a step's retry rule for its own `Fire` call:
  `MaxAttempts`, `BaseDelay`, `MaxDelay`, `Retryable`, `Jitter`, and
  `Sleep`. `BaseDelay` must not be negative; zero means an immediate
  retry. See Retry below.
- `LoopPolicy` — a step's loop rule for its `Sub` child workflow:
  `Guard` (a `machine.Guard`; nil means always continue) and `Max` (the
  iteration cap; zero means unbounded). See Loop below.
- `LoopState` — the loop context a `Guard` closure reads: `Iteration`
  (completed iterations, starting at zero before the first `Guard`
  call) and `Record` (the most recent child workflow's output). See
  Loop below.

## Functions and methods

- `New(steps, panels)` — builds a `Definition` and validates the graph.
- `Definition.Roots()` — returns the root step IDs in declaration
  order. A root is a step with no prerequisites.
- `Run(ctx, d, m, in, confirm, bus, onCheckpoint)` — walks `d` in
  topological order. Ready steps run in declaration order. A step
  named in no panel fires the `machine.Transition` row whose `To`
  matches the step's target status, then waits for `confirm` before
  the next step runs, exactly as before panels existed. A step named
  in a panel of one member runs alone the same way, and `confirm`
  still gates it. A step named in a panel of two or more members runs
  as part of that panel's wave: every member fires the one shared row
  that matches the panel's common `To`, concurrently, in its own
  goroutine. `Run` does not call `confirm` for a wave of two or more
  members; the ack gate applies to a step named in no panel, and to a
  one-member panel. `Run` returns a `Report` and an error. On every
  abort, `Run` returns the `Report` built so far, alongside the
  error: a step whose `Fire` fails or whose ack is rejected is marked
  `OutcomeFailed` first. A wave's shared, pre-spawn transition failure
  marks no member of that wave; a per-member `Fire` failure inside a
  wave marks every member `OutcomeFailed`, whether or not a
  dependent's `AdmissionOnFailed` rule catches it.
  When `bus` is non-nil, `Run` emits a `StepCompletedEvent` to it
  after each step completes; a chained step's child sub-workflow runs
  with a nil bus, so only the parent step emits. A skipped step fires
  no transition, calls no `Confirm`, and emits no
  `StepCompletedEvent`.
  Once every one of a step's needs is terminal, `Run` evaluates the
  step's `Admission` rule: `OutcomeSkipped` for a failed admission,
  otherwise the step runs. After a branch step runs, `Run` calls its
  `Route` with the post-fire status and record, then skips every
  direct dependent `Route` did not name. A panel resolves the same
  way, as one atomic unit: one unadmitted member skips every member.
  When `onCheckpoint` is non-nil, it fires immediately after each step
  or wave resolves `OutcomeSucceeded`, once any route-driven skip on
  that step has also resolved, with a fresh `Checkpoint`. Before each
  step or wave starts, `Run` checks `ctx` for cancellation; a canceled
  `ctx` stops the walk and returns the pinned pause error, wrapping
  `ctx.Err()`. See "Pause and resume" below.
  A step admitted through a failed need is a fallback; `Run` catches
  the dependency's `Fire` or `Route` failure and continues the run
  instead of aborting. See Fallback and continue-on-failure below.
- `Report.Status()` — the run's final `machine.Status`.
- `Report.Record()` — the run's final `machine.InOut`.
- `Report.Outcome(id)` — one step's `Outcome`, and whether it
  resolved.
- `Report.Outcomes()` — a copy of every resolved step's `Outcome`,
  keyed by ID. Caller mutation cannot change the `Report`.
- `Checkpoint.Validate()` — rejects an empty `Status`, a step ID
  named in more than one of `Done`, `Skipped`, and `Failed`, and an
  unsorted `Done`, `Skipped`, or `Failed`.
- `Checkpoint.Encode()` — validates, then marshals the checkpoint to
  JSON.
- `Decode(data)` — unmarshals JSON, then validates the result.
- `Resume(ctx, d, m, checkpoint, confirm, bus, onCheckpoint)` — seeds
  `outcomes` from `checkpoint.Done`, `checkpoint.Skipped`, and
  `checkpoint.Failed`, `cur` from `checkpoint.Status`, and `rec` from
  `checkpoint.Record`, then continues the same graph walk `Run` uses.
  See "Pause and resume" below.
- `FailureFrom(ctx)` — reads the `Failure` `Run` injects into a
  fallback step's own transition context. The boolean is false outside
  a fallback firing.
- `RetryPolicy.Validate()` — rejects a `MaxAttempts` below 1, a
  `MaxDelay` at or below zero, and a negative `BaseDelay`.
- `RetryPolicy.NextDelay(attempt)` — the backoff before the given
  retry attempt, one-indexed from the first retry. Doubles from
  `BaseDelay`, clamped at `MaxDelay`, then applies `Jitter` when
  non-nil. See Retry below.
- `LoopPolicy.Validate()` — rejects a negative `Max`.
- `LoopStateFrom(ctx)` — reads the `LoopState` `Run` injects before
  each `Guard` call of a loop step. The boolean is false outside a
  loop step's `Guard` evaluation.

## Invariants

`New` enforces the rules below.

- Every step ID is present and unique. An empty ID fails. A duplicate
  ID fails.
- Every dependency names an existing step. A dependency on an unknown
  step fails.
- Every panel entry names an existing step. A panel entry for an
  unknown step fails.
- No panel names one step ID twice. A repeated step ID fails.
- Every member of a panel shares one `To`. A member whose `To` differs
  from the panel's first member fails.
- No step ID is named in two panels. A repeat across panels fails.
- The step graph is acyclic. Kahn's algorithm detects a cycle before
  any step runs. A cycle fails.
- No panel member's `Needs` closure reaches a fellow member of the
  same panel, directly or through a chain of dependencies. This check
  runs after the cycle check, since it needs an acyclic graph to walk
  safely.
- A chained step's `Sub` is a valid `Definition`. It was already built
  by `New`, so its internal graph is acyclic.
- A chained step may not be a member of a multi-member panel. `New`
  rejects any such panel.
- A chained step's `To` is ignored by `Run`; `New` uses it only for
  panel homogeneity checks.
- Nesting stays within the depth limit. `New` rejects a `Sub` chain
  deeper than eight levels. Eight bounds the recursive runner's stack use
  while still allowing deep workflow composition; it is a local safety
  guard, not a wire-format limit.
- A step with no prerequisites is a root. `Roots` returns every root.
- `New` copies the input slices. A `Definition` is immutable after
  `New`. The fields are unexported, so the invariant is enforced.
  `Roots` returns a copy of the root list.
- A step may not combine a non-nil `Sub` and a non-nil `Route`. `New`
  rejects the shape.
- A branch step must have at least one direct dependent. `New` rejects
  a `Route` no step could ever select.
- No panel names a branch step. A wave fires every member concurrently,
  so per-member routing has no defined meaning. `New` rejects the
  shape.
- No panel names a direct dependent of a branch step. A route
  exclusion mid-panel would stall that panel forever, since a panel
  resolves only once every member's needs are terminal. `New` rejects
  the shape.
- An `AdmissionOnFailed` step must have at least one need. A root
  always admits, so the rule would be dead weight. `New` rejects the
  shape.
- No panel names an `AdmissionOnFailed` step. A wave shares one `ctx`
  across every member, with no per-member home for the `Failure` a
  fallback would catch. `New` rejects the shape.
- A non-nil `Retry` passes `RetryPolicy.Validate()`: `MaxAttempts` at
  or above 1, `MaxDelay` above zero, and `BaseDelay` at or above zero.
  `New` rejects a `Retry` combined with a non-nil `Sub`, and a `Retry`
  on a panel member. See Retry below.
- A non-nil `Loop` passes `LoopPolicy.Validate()`. `New` rejects a
  `Loop` combined with a nil `Sub`, and a `Loop` on a panel member. See
  Loop below.

`Run` enforces the rules below.

- `Run` rejects a nil `d`, a nil `m`, and a nil `confirm`. It checks
  `d` first, then `m`, then `confirm`, so it never dereferences a nil
  pointer.
- `Run` fails when zero or when more than one transition row targets
  a wave's shared status. Every failure names the failing step ID, or
  wraps `panel:` for a wave-wide failure.
- A guard rejection inside `machine.Fire` stops a singleton step
  before its ack, or fails the whole wave before any member's ack.
- A step named in no panel without a nil-returning `confirm` call does
  not advance. The next step never fires until the prior ack confirms.
- A wave that fails leaves the current status and the record at their
  pre-wave values. No member of that wave is marked done.
- When a chained step is not part of a multi-member panel, it runs its
  child `Definition` to completion before the parent resumes. The child
  final status becomes the parent step's target status. `confirm` runs
  for each child step and for the parent chained step.
- `New` does not validate cross-panel scheduling feasibility. A member
  of one panel that needs a member of another panel, with the reverse
  true too, stalls `Run` at runtime with the same "no ready step"
  error a `Needs` cycle would report, since neither panel's own
  independence rule catches this shape.

## Pause and resume

A caller pauses a run by canceling `ctx`. `Run` checks `ctx.Err()` at
the top of each loop iteration, before the next step or wave starts.
A step already running keeps running to its own completion; `Run`
only refuses to start the next step after an observed cancellation.
The last `Checkpoint` `onCheckpoint` delivered is the resume point;
`flow` adds no separate pause API.

`onCheckpoint` fires only after a step's or wave's outcome is marked
`OutcomeSucceeded`, so a checkpoint never captures a step mid-flight.
A zero-step `Definition` never fires `onCheckpoint`: there is no step
to complete.

`Resume` restarts the walk from a stored `Checkpoint`. It runs five
entry checks in order, before any seeding happens: a nil `d`, a nil
`m`, a nil `confirm`, a `checkpoint` that fails `Validate`, and a
`checkpoint.Done` entry naming a step ID absent from `d`. The first
failing check returns an error immediately; no step runs.

`Resume` never re-runs a step already in `checkpoint.Done`, because
`nextReadyGroup` skips any step ID already present in the seeded
`outcomes`. `Resume` on a checkpoint whose `Done` already covers every
step in `d` returns the checkpoint's status and record without
calling `confirm` or `onCheckpoint`; this holds for a one-step
`Definition` too.

For a chained step: a checkpoint captured right after the chained
step's parent transition fires already lists the chained step's ID as
`OutcomeSucceeded`. A subsequent `Resume` skips the whole step,
including its child workflow; the child's own internal progress is
not a granularity a checkpoint records.

`Resume` performs no topology check across `Done`: it never confirms
that a step named in `Done` has every one of its own `Needs` also
named in `Done`. `nextReadyGroup` treats a missing prerequisite as
still unresolved and selects it to run again; the resulting
`pickTransition` or `machine.Fire` call then fails, because
`checkpoint.Status` no longer names a status the seeded walk can reach
that step from. `Resume` returns that failure as an ordinary error.

`checkpoint.Failed` preserves a caught failure's outcome across a
pause, so `Resume` never re-runs a step that already resolved
`OutcomeFailed`. `Resume` starts its own fallback bookkeeping empty,
though: a fallback step that has not yet run at pause time still runs
after `Resume`, admitted by `AdmissionOnFailed` the same way it would
without a pause, but `FailureFrom` returns false inside it, and a
`Route` exclusion that would have emptied the failure's last pending
handler set resolves as an ordinary skip instead of aborting the run.
See Fallback and continue-on-failure below.

A caller whose `Record.Input` or `Record.Output` must survive a
`Checkpoint` round-trip is responsible for using JSON-primitive-
compatible types, or for re-hydrating its own concrete type after
`Decode`: `encoding/json` decodes an `any` field back to
`map[string]interface{}`, never the original concrete type. `flow`
performs no type-fidelity handling and no registry lookup.

## Panel waves

A wave fires every member of one panel through the same
`machine.Transition` row: one `Guard`, one `OnExit`, one `OnEntry`,
each called once per member, concurrently. Only each member's own
`machine.InOut` copy differs by memory identity; the closures cannot
see which member they run for, since `machine.Fire`'s signature
carries no step identity. A panel member's `Guard`, `OnExit`, and
`OnEntry` must be safe for concurrent invocation.

`Run` copies the wave's incoming record before it fires each member's
transition, but the copy is shallow. A map, a slice, or a pointer an
`Input` or `Output` field holds is not copied. Two members that alias
the same underlying data still race if either mutates it in place.
`flow` cannot deep-copy an arbitrary `any` value; a panel member's
`Input` and `Output` must be an immutable value, or a value the caller
already cloned per step.

A wave forwards one record: the output of the panel's first member, in
declaration order, chosen after every member finishes. The other
members' transitions still ran; only their records are discarded. A
caller whose panel members need their outputs merged cannot rely on
this package yet.

## Fallback and continue-on-failure

A step declares a fallback through admission, not through a dedicated
field. A step with `When: AdmissionOnFailed` admits once every one of
its needs is terminal and at least one ended `OutcomeFailed`; this is
an any-of rule, unlike the all-of rule `AdmissionOnFinished` and
`AdmissionOnSucceeded` use.

When a step's `Fire` fails, or a branch step's `Route` fails, `Run`
scans the unresolved steps for an `AdmissionOnFailed` step whose
`Needs` names the failed step. At least one match catches the
failure: the run continues instead of aborting, and the failed step
stays `OutcomeFailed`. No match aborts the run, exactly as before this
mechanism existed.

Before `Run` fires the fallback's own transition, it injects a
`Failure` into the transition's context: `Failure.Step` names the
failed step, and `Failure.Err` is its recorded error. `FailureFrom`
reads it back inside `Guard`, `OnExit`, or `OnEntry`. A fallback with
two failed needs receives the first failed need in `Needs` declaration
order. A dependent of the failed step that keeps the default
admission rule never satisfies it with a failed need, so it becomes
`OutcomeSkipped` once the failure is handled; only a declared fallback
runs down that path.

A panel failure follows its own rule. `runWave`'s shared, pre-spawn
transition failure is never catchable: it happens before any member's
`Guard` runs. A per-member `Fire` failure marks every member of that
wave `OutcomeFailed`, then the continue rule runs once per member: the
run continues only when every failed member has at least one
`AdmissionOnFailed` dependent. One unhandled member aborts the whole
wave's failure, joined, exactly as an unhandled wave failure did
before this mechanism existed.

A `Route` exclusion can silently remove the sole fallback of an
already-handled failure. `Run` re-checks this after every successful
`Route` call: when an excluded dependent was a failure's last
declared handler, the run aborts with that failure's recorded error,
preserving fail-fast.

A fallback is an ordinary step. It may itself fail and be caught by a
second, nested fallback; it may also be a branch step, running `Route`
normally after its own `Fire` succeeds. The only restrictions
`AdmissionOnFailed` adds are the two `New` invariants above: at least
one need, and no panel membership.

A chained step's own child-workflow failure is never catchable by a
parent-level fallback. The child's nested `Run` call already ran
through its own continue rule, in its own frame; whatever error
crosses back into the parent is treated as final.

A checkpoint taken after a catch preserves the failed step's outcome,
through `Checkpoint.Failed`, but not the pending-handler bookkeeping
behind it. See "Pause and resume" above.

## Retry

A step's `Retry` field bounds and paces repeated attempts of its own
`Fire` call. A nil `Retry` keeps the single-attempt behavior every
step had before this field existed.

`Run` calls `fireWithRetry`, an internal helper, in place of the
single `Fire` attempt. `fireWithRetry` wraps `fireStep`, not
`machine.Fire` directly, so a retried step's exhausted failure still
carries the tag `resolveCatchable` needs to route it into a declared
`AdmissionOnFailed` fallback. On a `Fire` failure, the loop first
checks the completed attempt count against `MaxAttempts`: an
exhausted budget stops the loop at once, so a `MaxAttempts` of 1 never
calls `Retryable` or `Sleep`. When budget remains, the loop checks
`Retryable`, when non-nil; a false result stops the loop at once.
Otherwise it calls `Sleep(ctx, NextDelay(attempt))` and retries `Fire`
from the same pre-step status and record the first attempt used.

`NextDelay` computes each backoff as a pure function of the attempt
number: it doubles `BaseDelay` once per attempt above 1, clamped at
`MaxDelay`, checking the bound before each doubling so the computation
never overflows `time.Duration`'s range. That bound holds for a policy
that passes `Validate`, which is why a negative `BaseDelay` fails
validation. `Jitter`, when non-nil,
perturbs the clamped result last; `NextDelay` does not re-clamp
`Jitter`'s output. `Sleep` defaults to a context-aware wait when the
field is nil: a canceled `ctx` returns at once, with the context's
error, instead of waiting out the full backoff. `fireWithRetry` checks
`ctx.Err()` after every `Sleep` call and aborts the loop the same way
on cancellation.

A retried step's `Guard`, `OnExit`, and `OnEntry` closures run once
per attempt, with no de-duplication; a closure with a side effect that
is not safe to repeat must guard its own idempotency. `fireWithRetry`
wraps only `Fire`: a `Confirm` rejection and a `Route` error never
retry, matching the fatal and scheduling-only rules those two already
follow. A step that exhausts its retries reports `OutcomeFailed`,
carrying the last attempt's error, exactly like a single-attempt
failure; a declared `AdmissionOnFailed` fallback still catches it, and
`FailureFrom` returns the last attempt's error inside the fallback.

## Loop

A step's `Loop` field runs its `Sub` child workflow more than once,
gated by `LoopPolicy.Guard`, before the step's own transition and
`Confirm` fire. `Max`, the iteration cap, defaults to zero, meaning
explicitly unbounded: the loop runs until `Guard` clears or `ctx` is
canceled or expires, bounded only by the caller's own `ctx`. `Loop`
requires a non-nil `Sub`; `New` rejects the combination of `Loop` and a
nil `Sub`, and a `Loop` on a panel member.

The loop reuses `Step.Sub`, the chaining mechanism, in place of a graph
cycle: `flow.Definition` stays a DAG, and `New`'s cycle rejection is
unchanged. Each iteration runs the child workflow from the previous
iteration's output record, except the first iteration, which threads
the parent step's own incoming record; a non-looped `Sub` step is
unaffected and keeps starting its child from a fresh `machine.InOut{}`.
Each iteration then fires the parent's own transition from the current
status to the child's final status, the same way a non-looped chained
step's single fire does.

Before every iteration, including the first, the loop checks `ctx` for
cancellation; a non-nil error stops the loop at once, with zero child
workflow runs if this is the first check, and fails the step wrapped
`flow: step %q: %w`. This check runs in the loop driver itself, so an
unbounded loop still terminates even when `Guard`, `OnEntry`, or
`OnExit` never inspect `ctx`. After each iteration fires, the loop
injects a `LoopState`, readable through `LoopStateFrom`, into `ctx`
before the next `Guard` call. `Max`, when non-zero, stops the loop
once reached, without a further `Guard` call. Otherwise the loop
evaluates `Guard`: a nil `Guard` reads as true, matching
`machine.Guard`'s own nil convention; a `Guard` error stops the loop
and fails the step the same way a `ctx` error does; a false result
stops the loop as a normal, successful exit, and a true result repeats.

A step that exhausts `ctx` or whose `Guard` errors reports
`OutcomeFailed`, exactly like any other `Fire` or `Confirm` failure; a
declared `AdmissionOnFailed` fallback still catches it, reading the
loop step's failure through `FailureFrom`. `Loop` and `Retry` are
already mutually exclusive: `Retry` requires a nil `Sub`, and `Loop`
requires a non-nil `Sub`.

## Attaching work to a step

A step never names its own executor. `Step` holds graph data only: an
ID, a dependency list, a target status, a payload, an optional
`PayloadFrom` resolver, and an optional nested `Definition`.

The real work runs through one attachment mechanism outside `Step`
itself.

- A `machine.Transition`'s `OnEntry` and `OnExit` actions run when a
  step's target status fires. An action is a plain
  `func(ctx, *machine.InOut) error`. It may call an agent, call a
  method, run a program, or call another package. `flow` never knows
  which one runs.

`Confirm` is an ack gate, not an attachment mechanism. It runs once
per step after the transition fires. A caller reads `step.ID` or
decodes `step.Payload` to route the ack to the right handler. `Run`
resolves `PayloadFrom` against the record current at the `Confirm`
call, so a transition's written output reaches the ack through
`step.Payload` without a captured pointer.

Agents are one caller of this contract, not a special case inside
`flow`. The `agent` package composes a `machine.Definition` and a
`flow.Definition` the same way any other automation would. `flow`
never imports `agent`; see [policy/layers.json](../../policy/layers.json).
See [agent.md](agent.md) for the composition layer's full reference.

### Two attachment mechanisms, not three

A step may nest a `Definition` and run it as a sub-workflow. This
composes workflows; it does not run arbitrary code.

Exactly two attachment mechanisms exist by design. A third must not
appear.

- The `machine.Transition` action closures run arbitrary work.
- A nested `Definition` composes one workflow inside another.

`Step.Sub` is the one `Step` field that carries this second mechanism.
No third attachment field belongs on `Step`, such as a `Handler` or an
`Executor` field. New work runs through an action closure instead.

`Step.Route` is not a third attachment mechanism. `Route` is
scheduling: it fires no transition and runs no step work. It only
picks which of a branch step's direct dependents the run keeps. The
work those dependents run still attaches only through an action
closure or a nested `Definition`.

## Admission and branch routing

`When` and `Route` decide whether a step runs, not what it runs.

A step admits once every one of its needs is terminal.
`AdmissionOnSucceeded`, the zero value, admits a step whose needs all
ended `OutcomeSucceeded`; a skipped need skips this step too, and that
skip cascades to the step's own dependents, so route exclusion
propagates by default. `AdmissionOnFinished` is the explicit opt-in
for skip tolerance; it admits through a skipped prerequisite, so an
optional branch never deadlocks a downstream join that declares it.

A step with a non-nil `Route` is a branch step. It fires its own
transition and confirms its ack like any other step. `Run` then calls
`Route` with the post-fire status and record. `Route` returns the IDs
of the direct dependents the run keeps; every other direct dependent
skips at once, even if that dependent has another, still-pending
need. An empty return skips every direct dependent. A duplicate ID in
the return collapses to one admission. A return that names a step
that is not a direct dependent, or a `Route` error, aborts the run: the
branch step is marked `OutcomeFailed`, exactly like a `Fire` failure.

A panel resolves as one atomic unit. It runs its wave only once every
member admits; one unadmitted member skips every member, even a
member whose own needs would otherwise admit it.

## Failure modes

This package returns plain errors, not sentinels. A caller cannot
match them with `errors.Is`.

- `New` fails on a malformed step graph: an empty or duplicate step
  ID, a missing dependency, a cycle, a panel naming an unknown step
  or one step twice, a panel whose members disagree on `To`, a step
  named in two panels, or a chained step sharing a panel of two or
  more members. Pinned by `flow_test/new_test.go` and
  `flow_test/panel_test.go`.
- `New` fails on invalid retry or loop configuration: a `Retry`
  policy combined with `Sub` or set on a panel member, and a `Loop`
  policy combined with a nil `Sub` or set on a panel member. Pinned
  by `flow_test/retry_test.go` and `flow_test/loop_test.go`.
- `New` fails on invalid admission or routing: a step combining
  `Sub` and `Route`, a branch step with no dependent, a panel naming
  a branch step or a branch step's direct dependent, and an
  `AdmissionOnFailed` step with no needs or named in a panel. Pinned
  by `flow_test/routing_test.go` and `flow_test/fallback_test.go`.
- `Run`'s branch step fails when `Route` returns an unknown step ID
  or a non-nil error; the branch step then reports
  `OutcomeFailed`. Pinned by `flow_test/routing_test.go`.
- `Run` fails when no transition or more than one transition matches
  a step's target status in the wired `machine.Definition`. Pinned
  by `flow_test/run_test.go` and `flow_test/chain_test.go`.

## Usage

```go
graph, err := flow.New([]flow.Step{
    {ID: "start"},
    {ID: "left", Needs: []string{"start"}, To: "reviewed"},
    {ID: "right", Needs: []string{"start"}, To: "reviewed"},
    {ID: "join", Needs: []string{"left", "right"}},
}, []flow.Panel{{"left", "right"}})
if err != nil {
    // the graph has a missing step, a bad panel, a panel that
    // disagrees on To, a panel that repeats a step, a panel whose
    // members depend on each other, or a cycle
}
roots := graph.Roots()
_ = roots
```

`left` and `right` share one `To`, so `Run` schedules them as one
wave once both are ready: their `Guard`, `OnExit`, and `OnEntry` fire
concurrently through the one transition row targeting `"reviewed"`.

`Run` walks a graph and a matching `machine.Definition` together:

```go
confirm := func(ctx context.Context, step flow.Step) error {
    return nil // the caller's ack transport confirms here
}
bus := events.New() // nil is also valid; Run skips emission then
onCheckpoint := func(c flow.Checkpoint) {
    // the caller stores c.Encode() somewhere durable; nil skips checkpointing
}
report, err := flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm, bus, onCheckpoint)
if err != nil {
    // a transition pick failed, a guard rejected a step, an ack did
    // not confirm, or ctx canceled and the run paused
}
status := report.Status()
out := report.Record()
_ = status
_ = out
```
