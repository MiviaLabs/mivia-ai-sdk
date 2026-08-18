// Package scheduler invokes a caller-supplied Job on a schedule.
// scheduler decides when to run something; it never decides what that
// something is. Job is func(ctx context.Context) error, the same
// decoupled closure shape machine.Guard already uses, so a caller
// wraps an agent run, a flow run, or a tool call in a closure
// matching that signature.
//
// Map: schedule.go = Schedule, Every, At; scheduler.go = Scheduler,
// New, Add, Remove, and the sentinel errors ErrBlankID,
// ErrNilSchedule, ErrNilJob, ErrDuplicateID; run.go = Run and its
// wake-channel sleep loop; events.go = JobFailedEvent. Scheduler holds
// no events.Bus of its own; Run takes one as a parameter, and a nil
// bus silently skips the emit, matching flow.Run's own precedent.
// Rationale: ../docs/plans/scheduler.md. Contribution rules: ../AGENTS.md.
package scheduler
