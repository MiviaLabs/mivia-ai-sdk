// Package flow implements the declarative workflow block:
// a step graph with dependencies and panels.
//
// Map: step.go = Step; panel.go = Panel; definition.go =
// Definition, New, Roots; validate.go = validation and Kahn's
// cycle check. The graph is data, not code. The runner, the
// scheduling, the parallel waves, and the chaining land later.
// Rationale: ../docs/plans/flow.md. Contribution rules:
// ../AGENTS.md.
package flow
