// Package providerregistry holds named provider.Completer values and
// routes one request across them in a caller-chosen order. Route falls
// through to the next name only when the caller's Retryable predicate
// approves the failure. The package holds no Completer of its own; a
// caller registers its own implementations.
//
// Map: registry.go = Registry, New, Register, Get, Names, and the
// sentinel errors ErrNilCompleter, ErrBlankName, ErrDuplicateName;
// route.go = Retryable, Route, and the sentinel errors ErrUnknownName,
// ErrEmptyOrder, ErrAllFailed. Rationale:
// ../docs/plans/providerregistry.md.
// Contribution rules: ../AGENTS.md.
package providerregistry
