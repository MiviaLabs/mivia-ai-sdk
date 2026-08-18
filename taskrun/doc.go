// Package taskrun runs one task under ledger admission: admit, claim,
// complete. It maps the caller work's result onto the ledger status and
// returns the work's own error. The caller supplies every value; the
// package holds no state between calls.
//
// Map: taskrun.go = Options, Task, Run, and the sentinel errors.
// Rationale: ../docs/plans/taskrun.md. Contribution rules: ../AGENTS.md.
package taskrun
