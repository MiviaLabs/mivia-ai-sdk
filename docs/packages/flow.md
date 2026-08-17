# Package reference: flow

The flow package is the declarative workflow building block. It owns
the step graph, the cycle check, and the sequential runner. A workflow
is data, not code. The graph has steps and panels. `Run` walks the
graph and moves a `machine.Definition` one step at a time. The panel
scheduling and the chaining land later. The exported surface below
mirrors `api/flow.txt`.

## Types

- `Step` — one node in a workflow graph. A step has an ID, a list of
  prerequisite step IDs, a target status string, and a payload. A step
  with no prerequisites is a root.
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
- `Run(ctx, d, m, in, confirm)` — walks `d` in topological order.
  Ready steps run in declaration order. Each step fires the
  `machine.Transition` row whose `To` matches the step's target
  status, then waits for `confirm` before the next step runs. `Run`
  returns the final `machine.Status`, the final `machine.InOut`
  record, and an error.

## Invariants

`New` enforces the rules below.

- Every step ID is present and unique. An empty ID fails. A duplicate
  ID fails.
- Every dependency names an existing step. A dependency on an unknown
  step fails.
- Every panel entry names an existing step. A panel entry for an
  unknown step fails.
- The step graph is acyclic. Kahn's algorithm detects a cycle before
  any step runs. A cycle fails.
- A step with no prerequisites is a root. `Roots` returns every root.
- `New` copies the input slices. A `Definition` is immutable after
  `New`. The fields are unexported, so the invariant is enforced.
  `Roots` returns a copy of the root list.

`Run` enforces the rules below.

- `Run` rejects a nil `d`, a nil `m`, and a nil `confirm`. It checks
  `d` first, then `m`, then `confirm`, so it never dereferences a nil
  pointer.
- `Run` fails when zero or when more than one transition row targets
  a step's status. Every failure names the failing step ID.
- A guard rejection inside `machine.Fire` stops the run before the
  step's ack.
- A step without a nil-returning `confirm` call does not advance. The
  next step never fires until the prior ack confirms.

## Usage

```go
graph, err := flow.New([]flow.Step{
    {ID: "start"},
    {ID: "left", Needs: []string{"start"}},
    {ID: "right", Needs: []string{"start"}},
    {ID: "join", Needs: []string{"left", "right"}},
}, []flow.Panel{{"left", "right"}})
if err != nil {
    // the graph has a missing step, a bad panel, or a cycle
}
roots := graph.Roots()
_ = roots
```

`Run` walks a graph and a matching `machine.Definition` together:

```go
confirm := func(ctx context.Context, step flow.Step) error {
    return nil // the caller's ack transport confirms here
}
status, out, err := flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm)
if err != nil {
    // a transition pick failed, a guard rejected a step, or an ack
    // did not confirm
}
_ = status
_ = out
```
