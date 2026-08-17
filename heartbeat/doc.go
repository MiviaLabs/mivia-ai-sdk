// Package heartbeat tracks liveness by time. A sender beats on its own
// schedule; Monitor tracks the last beat per id and reports which ids
// have gone silent past a fixed timeout.
//
// Map: monitor.go = Monitor, New, Beat, Alive, Dead, Forget, and the
// sentinel errors ErrNoTimeout, ErrNoID, ErrStaleBeat; events.go =
// MissedEvent. Monitor holds no clock of its own; every method takes
// the caller's time.Time. Monitor never emits MissedEvent itself; a
// caller reads Dead and emits the event through its own events.Bus.
// Rationale: ../docs/plans/heartbeat.md. Contribution rules: ../AGENTS.md.
package heartbeat
