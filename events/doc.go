// Package events implements a caller-owned reaction bus:
// typed events, one subscription set, and in-process dispatch.
//
// Map: bus.go = Event, Handler, Bus, New, Subscribe, Emit, and
// Validate. registry.go = HookHandler, Registry, NewRegistry, Add,
// Remove, Fire, and the sentinels ErrBlankName, ErrNilHandler,
// ErrDuplicateName, and ErrVetoed. point.go = Point, its named
// constants, Validate, and String. A hook handler observes or vetoes
// one lifecycle point's action. A caller emits one event per state
// change. The bus runs each handler in order, one at a time. The
// package imports nothing of this module; it stays a leaf block. The
// module has no shared bus. Rationale: ../docs/plans/events.md.
// Contribution rules: ../AGENTS.md.
package events
