# Package reference: flow

The flow package is the declarative workflow building block. It owns
the step graph and the cycle check. A workflow is data, not code. The
graph has steps and panels. The runner and the scheduling land later.
The exported surface below mirrors `api/flow.txt`.

## Types

- `Step` — one node in a workflow graph. A step has an ID, a list of
  prerequisite step IDs, a target status string, and a payload. A step
  with no prerequisites is a root.
- `Panel` — a group of step IDs that run together in parallel. The
  runner schedules a panel as one wave. A panel is a named list of
  strings.
- `Definition` — a validated step graph and its panels. The fields are
  unexported. Callers read the roots through `Roots`.

## Functions and methods

- `New(steps, panels)` — builds a `Definition` and validates the graph.
- `Definition.Roots()` — returns the root step IDs in declaration
  order. A root is a step with no prerequisites.

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
