// Package contextstate holds the durable context contract and the
// single canonical content-reference minter. Sessions, checkpoints,
// commit validation, retention classes, and volume Limits live here.
// envelope and memory reuse the minter, so every ref in this SDK has
// one form.
//
// Map: ref.go = HashPrefix, Digest, Mint, IsRef. contracts.go =
// shape bounds, sentinels, ValidationError, ContentRef, PayloadRecord,
// Reassemble. checkpoint.go = SourceID through Session. commit.go =
// CommitRequest and its validators. limits.go = Limits. store.go =
// MemStore.
// Rationale: ../docs/plans/contextstate.md. Contribution rules:
// ../AGENTS.md.
package contextstate
