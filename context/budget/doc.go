// Package budget states and checks a budget for one model
// call's context. Limits holds a byte cap and an event-count cap; it
// does no I/O and keeps no state beyond the two caps.
//
// Map: budget.go = Limits, Validate, Fits.
// Rationale: ../docs/history/context/budget.md. Contribution rules:
// ../AGENTS.md.
package budget
