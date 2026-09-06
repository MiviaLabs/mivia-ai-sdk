// Package flow implements the declarative workflow block:
// a step graph with dependencies and panels.
//
// Map: step.go = Step, Route, Admission; panel.go = Panel;
// definition.go = Definition, New, Roots; validate.go = step, panel,
// payload, and retry validation with Kahn's cycle check; routing.go =
// admission and panel verdicts plus route application; runner.go =
// Run, Confirm, and the wave advance; outcome.go = Outcome and
// Report; failure.go = Failure and its context accessors; retry.go =
// RetryPolicy and fireWithRetry; loop.go = LoopPolicy and LoopState;
// wave.go = one concurrent wave of ready steps; resume.go = Resume
// over a checkpoint; checkpoint.go = Checkpoint and its outcome
// views; events.go = StepCompletedEvent; wire.go = Decode for a
// stored Checkpoint.
// The graph is data, not code. A step with Sub runs a nested
// workflow to completion.
// Rationale: ../docs/plans/flow.md. Contribution rules:
// ../AGENTS.md.
package flow
