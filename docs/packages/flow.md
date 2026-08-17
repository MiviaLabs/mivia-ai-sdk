# Package reference: flow

The flow package is the declarative workflow building block. It owns
the step graph, the cycle check, and the runner. A workflow is data,
not code. The graph has steps and panels. `Run` walks the graph and
moves a `machine.Definition` one step at a time. A step named in no
panel runs alone; a step named in a panel runs as part of that
panel's wave, in a goroutine, together with every other member.
Chaining ships in phase 7. The exported surface below mirrors
`api/flow.txt`.

## Types

- `Step` — one node in a workflow graph. A step has an ID, a list of
  prerequisite step IDs, a target status string, a payload, and an
  optional nested `Definition`. A step with no prerequisites is a root.
  For a chained step, `To` is ignored by `Run` and may be empty; the
  child final status supplies the target status.
- `Panel` — a group of step IDs that run together in parallel. The
  runner schedules a panel as one wave. A panel is a named list of
  strings.
- `Definition` — a validated step graph and its panels. The fields are
  unexported. Callers read the roots through `Roots`.
- `Confirm` — the ack gate a caller supplies to `Run`. It signs the
  form `func(ctx context.Context, step Step) error`. A nil return
  means the ack confirmed.

## Functions and methods

- `New(steps, panels)` — builds a `Definition` and validates the graph.
- `Definition.Roots()` — returns the root step IDs in declaration
  order. A root is a step with no prerequisites.
- `Run(ctx, d, m, in, confirm, bus)` — walks `d` in topological order.
  Ready steps run in declaration order. A step named in no panel fires
  the `machine.Transition` row whose `To` matches the step's target
  status, then waits for `confirm` before the next step runs, exactly
  as before panels existed. A step named in a panel of one member
  runs alone the same way, and `confirm` still gates it. A step named
  in a panel of two or more members runs as part of that panel's
  wave: every member fires the one shared row that matches the
  panel's common `To`, concurrently, in its own goroutine. `Run` does
  not call `confirm` for a wave of two or more members; the ack gate
  applies to a step named in no panel, and to a one-member panel.
  `Run` returns the final
  `machine.Status`, the final `machine.InOut` record, and an error.
  When `bus` is non-nil, `Run` emits a `StepCompletedEvent` to it
  after each step completes; a chained step's child sub-workflow runs
  with a nil bus, so only the parent step emits.

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
`flow`. The future `agent` package composes a `machine.Definition` and
a `flow.Definition` the same way any other automation would. `flow`
never imports `agent`; see `policy/layers.json`.

### Phase 7 design note: two attachment mechanisms, not three

Phase 7 (`docs/plans/agents/phase07_flow_chain.md`) adds the second
attachment mechanism. A step may nest a `Definition` and run it as a
sub-workflow. This composes workflows; it does not run arbitrary code.

Two attachment mechanisms exist by design. A third must not appear.

- The `machine.Transition` action closures run arbitrary work.
- A nested `Definition` composes one workflow inside another.

`Step.Sub` is the one new `Step` field allowed for this mechanism.
Do not add a third attachment field to `Step` for a new use case, such
as a `Handler` or an `Executor` field. Send new work through an
action closure instead.

Phase 22 adds `Step.Route`. `Route` is scheduling, not work
attachment. It selects which dependents run. It fires no transition
and runs no step work. The two-mechanism rule stands.

Options for phase 7, recorded here so the choice does not get lost:

- Option A (recommended). State this two-mechanism rule in the phase
  7 plan before implementation starts. Map every future use case to
  one of the two attachment mechanisms, never to a new `Step` field
  beyond `Sub`.
- Option B. Defer the decision until phase 7 begins, and re-run an
  architecture assessment then. This risks losing the reasoning
  between now and phase 7.

This note mirrors the same text in
`docs/plans/agents/phase07_flow_chain.md`.

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
status, out, err := flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm, bus)
if err != nil {
    // a transition pick failed, a guard rejected a step, or an ack
    // did not confirm
}
_ = status
_ = out
```
