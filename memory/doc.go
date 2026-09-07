// Package memory stores and fetches context blobs by content address.
// Put computes the sha256: ref with contextref.Mint and returns it.
// Get fetches a blob by that ref. A size budget bounds the store; a
// Put that would exceed the budget evicts the oldest-inserted blobs
// first.
//
// Map: store.go = Store, New, Put, Get, and the sentinel errors
// ErrNoBudget, ErrBudgetExceeded, ErrUnknownRef. Memory holds opaque
// bytes; it does not parse or validate the content, and it does not
// know about envelope.Message or any other wire type. The package
// also holds the principal-scoped spool: spool.go = Spool,
// NewSpool, Spool, SpoolExpiring, Expire, GrantExpiry, Load,
// ContentStore, and the sentinels ErrNoGrantBudget and
// ErrUnknownGrantRef; context.go = WithPrincipal and PrincipalFrom;
// readtool.go = ReadOutputTool and MoreMarker; tool.go = SpoolTool
// and WithSpool. A spool grant bounds one principal's oversized
// content and hands back a bounded view plus a content ref.
// Rationale: ../docs/plans/memory.md. Contribution rules: ../AGENTS.md.
package memory
