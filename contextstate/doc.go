// Package contextstate holds the durable context contract. Sessions,
// checkpoints, commit validation, retention classes, and volume Limits
// live here.
//
// Map: contracts.go = shape bounds, sentinels, ValidationError,
// ContentRef, PayloadRecord, Reassemble. ref.go = canonical reference
// minter aliases. checkpoint.go = SourceID through Session. commit.go =
// CommitRequest and its validators. limits.go = Limits. store.go =
// MemStore.
// Rationale: ../docs/plans/contextstate.md. Contribution rules:
// ../AGENTS.md.
package contextstate
