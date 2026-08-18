// Package contextbudget states and checks a budget for one model
// call's context. Limits holds a byte cap and an event-count cap; it
// does no I/O and keeps no state beyond the two caps.
//
// Map: contextbudget.go = Limits, Validate, Fits.
// Rationale: ../docs/plans/contextbudget.md. Contribution rules:
// ../AGENTS.md.
package contextbudget
