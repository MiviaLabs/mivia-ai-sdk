// Package flow implements the declarative workflow block:
// a step graph with dependencies and panels.
//
// Map: step.go = Step; panel.go = Panel; definition.go =
// Definition, New, Roots; validate.go = validation and Kahn's
// cycle check; runner.go = Run and Confirm. The graph is data,
// not code. Chaining ships in phase 7; a step with Sub runs a
// nested workflow to completion.
// Rationale: ../docs/plans/flow.md. Contribution rules:
// ../AGENTS.md.
package flow
