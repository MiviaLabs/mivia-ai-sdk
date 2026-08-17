// Package events implements a caller-owned reaction bus:
// typed events, one subscription set, and in-process dispatch.
//
// Map: bus.go = Event, Handler, Bus, New, Subscribe, Emit,
// Validate. A caller emits one event per state change. The bus runs
// each handler in order, one at a time. The bus imports nothing of
// this module; it stays a leaf block. The module has no shared bus.
// Rationale: ../docs/plans/events.md. Contribution rules: ../AGENTS.md.
package events
