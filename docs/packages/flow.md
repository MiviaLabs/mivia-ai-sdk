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
  optional nested `Definition`, an `Admission` rule, and an optional
  `Route`. A step with no prerequisites is a root. For a chained step,
  `To` is ignored by `Run` and may be empty; the child final status
  supplies the target status. A step with a non-nil `Route` is a
  branch step.
- `Panel` — a group of step IDs that run together in parallel. The
  runner schedules a panel as one wave. A panel is a named list of
  strings.
- `Definition` — a validated step graph and its panels. The fields are
  unexported. Callers read the roots through `Roots`.
- `Confirm` — the ack gate a caller supplies to `Run`. It signs the
  form `func(ctx context.Context, step Step) error`. A nil return
  means the ack confirmed.
- `Admission` — the rule that admits a step once every one of its
  needs is terminal. `AdmissionOnFinished`, the zero value, admits
  when every need ended `OutcomeSucceeded` or `OutcomeSkipped`.
  `AdmissionOnSucceeded` admits only when every need ended
  `OutcomeSucceeded`.
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
  (`machine.Status`), `Record` (`machine.InOut`), and `Done` (the
  lexicographically sorted step IDs of every step that resolved
  `OutcomeSucceeded` so far). `Done`'s order is a sort, not a
  completion order. `Encode` and `Decode` round-trip a `Checkpoint`
  through JSON; the caller owns storage. See `Run`'s `onCheckpoint`
  parameter and `Resume`.

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
  `OutcomeFailed` first. A wave error marks no member of that wave.
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
- `Report.Status()` — the run's final `machine.Status`.
- `Report.Record()` — the run's final `machine.InOut`.
- `Report.Outcome(id)` — one step's `Outcome`, and whether it
  resolved.
- `Report.Outcomes()` — a copy of every resolved step's `Outcome`,
  keyed by ID. Caller mutation cannot change the `Report`.
- `Checkpoint.Validate()` — rejects an empty `Status`.
- `Checkpoint.Encode()` — validates, then marshals the checkpoint to
  JSON.
- `Decode(data)` — unmarshals JSON, then validates the result.
- `Resume(ctx, d, m, checkpoint, confirm, bus, onCheckpoint)` — seeds
  `outcomes` from `checkpoint.Done`, `cur` from `checkpoint.Status`,
  and `rec` from `checkpoint.Record`, then continues the same graph
  walk `Run` uses. See "Pause and resume" below.

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

## Attaching work to a step

A step never names its own executor. `Step` holds graph data only: an
ID, a dependency list, a target status, a payload, and an optional
nested `Definition`.

The real work runs through one attachment mechanism outside `Step`
itself.

- A `machine.Transition`'s `OnEntry` and `OnExit` actions run when a
  step's target status fires. An action is a plain
  `func(ctx, *machine.InOut) error`. It may call an agent, call a
  method, run a program, or call another package. `flow` never knows
  which one runs.

`Confirm` is an ack gate, not an attachment mechanism. It runs once
per step after the transition fires. A caller reads `step.ID` or
decodes `step.Payload` to route the ack to the right handler.

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
`AdmissionOnFinished`, the zero value, admits a step whose needs ended
`OutcomeSucceeded` or `OutcomeSkipped`; a skipped prerequisite passes
through, so a skipped branch never deadlocks a downstream join.
`AdmissionOnSucceeded` admits only when every need ended
`OutcomeSucceeded`; a skipped need skips this step too, and that skip
can cascade to the step's own dependents in turn.

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
