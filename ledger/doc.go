// Package ledger implements the durable-task-admission block:
// idempotency-keyed admission, lease-based ownership with fencing,
// and dependency-driven blocking on failure.
//
// Map: task_state.go = IdempotencyKey, OwnerID, Sequence, FenceToken,
// the five Status constants, TaskState, Validate; store.go = Store,
// MemStore, NewMemStore; ledger.go = Ledger, New, Admit, State,
// Blocked; claim.go = Claim, Renew, Release, Takeover; complete.go =
// Complete and the dependency-blocking walk; snapshot.go = Snapshot,
// Validate, Restore; wire.go = Encode, Decode, and the row-marshal
// helpers SQLiteStore shares with it; events.go = the emitted event
// names; errors.go = the sentinel errors; sqlite_store.go (behind the
// ledger_sqlite build tag) = SQLiteStore, NewSQLiteStore, Close, a
// modernc.org/sqlite-backed Store.
// Rationale: ../docs/plans/ledger.md. Contribution rules: ../AGENTS.md.
package ledger
