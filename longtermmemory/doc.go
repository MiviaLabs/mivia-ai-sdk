// Package longtermmemory holds durable-feeling learnings an agent
// wants across turns: entries in core and archive tiers, a small
// never-evicted core per scope, automatic consolidation near
// capacity, keyword search, and a bounded core-context frame a caller
// renders into its own system prompt. In-memory only; a leaf package
// with no internal imports.
package longtermmemory

import "errors"

// Sentinel errors; test with errors.Is.
var (
	// ErrEntryNotFound is Delete and PromoteToCore's error for an
	// unknown id.
	ErrEntryNotFound = errors.New("longtermmemory: entry not found")
	// ErrCoreTierFull is PromoteToCore's error when the scope's core
	// tier already holds CoreTierCap rows.
	ErrCoreTierFull = errors.New("longtermmemory: core tier is full")
	// ErrStoreFull is Save's error when the scope is still full after
	// consolidation.
	ErrStoreFull = errors.New("longtermmemory: scope is full")
	// ErrQueryRequired is Search's error for blank query text.
	ErrQueryRequired = errors.New("longtermmemory: query text is required")
	// ErrScopeRequired is Search's error for a blank scope.
	ErrScopeRequired = errors.New("longtermmemory: scope is required")
)
