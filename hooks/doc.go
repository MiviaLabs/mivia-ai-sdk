// Package hooks gives a caller a named, multi-handler registry for a
// lifecycle point: Point, Handler, and a Registry whose Fire runs
// every handler at a point in registration order and stops at the
// first veto. A leaf package: no I/O, no goroutine, no persistence.
//
// Map: point.go = Point, its named constants, Validate, and String;
// registry.go = Handler, Registry, New, Add, Remove, Fire, and the
// sentinel errors ErrBlankName, ErrNilHandler, ErrDuplicateName,
// ErrVetoed. Rationale: ../docs/plans/hooks.md.
// Contribution rules: ../AGENTS.md.
package hooks
