// Package memory stores and fetches context blobs by content address.
// Put computes the sha256: ref with envelope.ContextRef and returns
// it. Get fetches a blob by that ref. A size budget bounds the store;
// a Put that would exceed the budget evicts the oldest-inserted
// blobs first.
//
// Map: store.go = Store, New, Put, Get, and the sentinel errors
// ErrNoBudget, ErrBudgetExceeded, ErrUnknownRef. Memory holds opaque
// bytes; it does not parse or validate the content, and it does not
// know about envelope.Message or any other wire type.
// Rationale: ../docs/plans/memory.md. Contribution rules: ../AGENTS.md.
package memory
