// Package discovery answers whether an agent can do a task.
//
// Map: card.go = Card, Parse, Validate, Match. Parse reads a
// capability card from JSON. Validate checks the card invariants.
// Match compares a capability request against the card's capability
// list. The package imports nothing of this module; it stays a leaf
// block, like envelope and events. Rationale:
// ../docs/plans/discovery.md. Contribution rules: ../AGENTS.md.
package discovery
