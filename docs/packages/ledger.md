# Package reference: ledger

The ledger package is the durable-task-admission block. A task
submitted under an idempotency key admits exactly once. Ownership
moves between processes through a time-boxed lease with a monotonic
fence. A failed task marks its dependents blocked. The exported
surface below mirrors `api/ledger.txt`.

## Types

- `IdempotencyKey` — the caller-chosen key that dedupes a task.
- `OwnerID` — the caller-chosen identity of a claimant.
- `Sequence` — the watermark a caller assigns per submission.
- `FenceToken` — the monotonic counter `Claim` and `Takeover` return.
- `Actor` — the caller-chosen identity of whoever performs a write: an
  external user ID, an agent ID, or any other identifier the caller
  finds meaningful. `ledger` does not validate its shape.
- `TaskState` — the full record for one key: `Key`, `Status`,
  `Sequence`, `Owner`, `Fence`, `LeaseUntil`, `Needs`, `BlockedBy`,
  `Task`, `Rev`, `CreatedBy`, `CreatedAt`, `UpdatedBy`, `UpdatedAt`.
  `Task` is caller-owned; `ledger` never inspects it. `Rev` is a
  `Store`-assigned revision counter. `CreatedBy`/`CreatedAt` and
  `UpdatedBy`/`UpdatedAt` are `Actor`/`time.Time` pairs every mutating
  method stamps; see Invariants below.
- `Store` — the pluggable record backend: `Load`, `CompareAndSwap`,
  `Range`.
- `MemStore` — the shipped mutex-guarded `Store`.
- `MemStoreOptions` — optional cap for `MemStore`. Field `MaxEntries`
  int; zero means unbounded, positive bounds live entries, negative is
  rejected.
- `SQLiteStore` — a `Store` backed by a local `modernc.org/sqlite`
  database file (or `":memory:"`). Behind the `ledger_sqlite` build
  tag; see "SQLiteStore" below.
- `Ledger` — the handle over one `Store` and an optional `events.Bus`.
- `Snapshot` — a point-in-time copy of every record: `Tasks`.

## Constants

- `StatusPending`, `StatusClaimed`, `StatusCompleted`, `StatusFailed`,
  `StatusBlocked` — the five task states, as `machine.Status` values.
- `AdmittedEvent`, `ClaimedEvent`, `RenewedEvent`, `ReleasedEvent`,
  `TakenOverEvent`, `CompletedEvent`, `BlockedEvent` — the typed
  `events.Name` constants, emitted after the matching write succeeds.

## Functions and methods

- `New(store, bus)` — builds a `Ledger`. A nil `store` defaults to
  `NewMemStore()`. A nil `bus` disables events.
- `NewMemStore()` — builds an empty, unbounded `MemStore`.
- `NewMemStoreWithOptions(opts)` — builds a `MemStore` honoring the
  cap. Returns `ErrInvalidMaxEntries` for a negative `MaxEntries`.
- `Ledger.Admit(ctx, actor, key, seq, task, now, needs...)` — records a
  task once per key. A need already failed or blocked lands the new
  record `StatusBlocked`, so a late dependent never claims. It returns
  `false, nil`, not an error, for a duplicate or a post-completion
  resubmission. On first insert, it stamps `CreatedBy`/`CreatedAt` from
  `actor`/`now`; a rebase over an existing non-terminal record carries
  them forward unchanged. Every successful write stamps `UpdatedBy` and
  `UpdatedAt`.
- `Ledger.Claim(ctx, actor, key, owner, lease, now)` — claims a pending
  record, or a claimed record whose lease has expired. Returns
  `ErrEmptyOwner` for a blank `owner`.
- `Ledger.Renew(ctx, actor, key, owner, fence, lease, now)` — extends
  the lease under the current fence.
- `Ledger.Release(ctx, actor, key, owner, fence, now)` — returns a
  claimed record to pending.
- `Ledger.Takeover(ctx, actor, key, owner, lease, now)` — claims a
  stale claimed record and fences the dispossessed owner's token.
  Returns `ErrEmptyOwner` for a blank `owner`.
- `Ledger.Complete(ctx, actor, key, owner, fence, status, now)` — marks
  a claimed record `StatusCompleted` or `StatusFailed`. A failed
  completion blocks every dependent, transitively.
- `Ledger.State(ctx, key)` — the current record for a key.
- `Ledger.Blocked(ctx, key)` — the blocking ancestor when a key is
  blocked.
- `Ledger.Snapshot(ctx)` — a point-in-time copy of every record.
- `Snapshot.Validate()` — runs `TaskState.Validate` over every entry.
- `Snapshot.Encode()` and `Decode(data)` — JSON round-trip for a
  snapshot, validating before and after.
- `Ledger.Restore(ctx, snapshot)` — inserts every snapshot record.
- `TaskState.Validate()` — checks one record's field rules.
- `MemStore.Load`, `MemStore.CompareAndSwap`, `MemStore.Range` — the
  `Store` implementation.
- `NewSQLiteStore(path)`, `SQLiteStore.Close()`, `SQLiteStore.Load`,
  `SQLiteStore.CompareAndSwap`, `SQLiteStore.Range` — the
  `modernc.org/sqlite`-backed `Store` implementation. See
  "SQLiteStore" below.

## Failure modes

- `ErrLeaseActive` ("ledger: lease is still active") — `Claim`
  returns it when another owner's `LeaseUntil` is still after now.
  Pinned by `ledger_test/claim_test.go`.
- `ErrFenced` ("ledger: fence token is stale") — `Renew`, `Release`,
  and `Complete` return it when the supplied fence token does not
  match the stored one. Pinned by `ledger_test/claim_test.go`,
  `ledger_test/complete_test.go`, and `ledger_test/takeover_test.go`.
- `ErrNotStale` ("ledger: lease is not stale") — `Takeover` returns
  it when the current lease has not yet expired. Pinned by
  `ledger_test/takeover_test.go`.
- `ErrNotClaimed` ("ledger: record is not claimed") — `Claim`,
  `Renew`, `Release`, `Complete`, and `Takeover` return it when the
  record's status is outside their eligible set. Pinned by
  `ledger_test/claim_test.go`, `ledger_test/complete_test.go`, and
  `ledger_test/takeover_test.go`.
- `ErrNoKey` ("ledger: key has no record") — `Claim`, `Renew`,
  `Release`, `Complete`, and `Takeover` return it when `Store.Load`
  finds no record for the key. Pinned by `ledger_test/claim_test.go`,
  `ledger_test/complete_test.go`, and `ledger_test/takeover_test.go`.
- `ErrUnknownStatus` ("ledger: status must be StatusCompleted or
  StatusFailed") — `Complete` returns it when `status` is neither
  `StatusCompleted` nor `StatusFailed`. Pinned by
  `ledger_test/complete_test.go`.
- `ErrEmptyOwner` ("ledger: owner must not be empty") — `Claim` and
  `Takeover` return it when `owner` is blank. Pinned by
  `ledger_test/claim_test.go` and `ledger_test/takeover_test.go`.
- `ErrInvalidMaxEntries` ("ledger: MaxEntries must not be negative")
  — `NewMemStoreWithOptions` wraps it when `opts.MaxEntries` is
  negative. Pinned by `ledger_test/mem_store_options_test.go`.

## Invariants

- `TaskState.Validate` rejects an empty `Key`, a `Status` outside the
  five constants, a `Needs` entry equal to `Key`, a non-empty
  `BlockedBy` outside `StatusBlocked`, an empty `BlockedBy` inside
  `StatusBlocked`, and a `StatusClaimed` record with an empty `Owner`
  or a zero `LeaseUntil`.
- `Admit` rebases a `StatusPending` or `StatusClaimed` record at a
  higher sequence; it never rebases a terminal record.
- `Claim` and `Takeover` read `LeaseUntil` against the caller-supplied
  `now` as the only staleness signal. `ledger` imports no liveness
  package.
- Lease expiry alone never fences. `Renew`, `Release`, and `Complete`
  check only the fence token, so a stale owner nobody has taken over
  still holds a valid fence. Pinned by
  `ledger_test/lease_semantics_test.go`.
- Every mutating method follows the same retry-and-reclassify contract
  on a losing `CompareAndSwap`: it reloads the record and re-evaluates
  its own eligibility rule, retrying while the caller still
  legitimately owns the record and returning the matching sentinel
  error once it does not.
- `Complete` on `StatusFailed` walks the dependency graph in two
  `Store` passes and blocks every transitive dependent. An ordinary
  tree-shaped `Needs` graph never blocks the failed key itself; a
  genuine cycle in `Needs` routes back to it, so the failed key joins
  the blocked set too. See docs/plans/ledger.md for the worked
  example.
- Pass two's per-dependent `CompareAndSwap` follows the same
  retry-and-reclassify contract as every other mutating method. On a
  losing compare, it reloads the dependent and retries against the
  fresh record. It stops once the write lands or the fresh record is
  already `StatusBlocked`. A concurrent write to a dependent between
  the `Range` snapshot and pass two never drops the block silently.
- `MemStore.CompareAndSwap` compares `old` against the stored record
  on `(Sequence, Status, Fence, Rev)`, and bumps `Rev` by one on every
  successful write, closing the blind spot two concurrent same-fence
  `Renew` calls would otherwise leave.
- Every mutating method takes a leading `actor Actor` argument and
  stamps `UpdatedBy`/`UpdatedAt` on every successful write; `Admit`
  additionally stamps `CreatedBy`/`CreatedAt` on first insert only.
  `ledger` does not validate `Actor`'s shape or require it to name a
  real identity.

## SQLiteStore

`SQLiteStore` is an opt-in, durable `Store` for a caller who needs a
task record to survive a process restart on one host. It lives behind
the `ledger_sqlite` build tag: a caller who imports `ledger` for
`MemStore` and `Ledger` alone never compiles it and never pulls in
`modernc.org/sqlite`. Build and test it with
`go build -tags ledger_sqlite ./ledger/...` and
`go test -tags ledger_sqlite -race ./ledger/...`.

- `NewSQLiteStore(path)` opens `path` (a file path, or `":memory:"`
  for an in-process database with no file), creates its one-table
  schema (`ledger_tasks`) if absent, sets the connection pool size and
  the WAL/synchronous/foreign-keys/busy-timeout pragmas, and `Ping`s
  once to fail fast on a bad path.
- `Task` stores through `encoding/json.Marshal`/`Unmarshal`, not as a
  live Go value: a caller storing a channel, a function, or an
  unexported-field-only struct in `Task` gets a non-nil error from
  `CompareAndSwap`, not silent data loss. `MemStore` has no such
  limit, since it never serializes `Task`.
- `CompareAndSwap`'s `Rev` bump happens inside the SQL statement
  itself, so a concurrent writer against the same row from a second
  process is serialized by the database, not only by an in-process
  mutex.
- `MemStore` stays the default, zero-dependency `Store`; `New`'s
  nil-`Store` fallback is unchanged. `SQLiteStore` is a caller-built,
  explicitly passed alternative.

## Cross-references

- [machine.md](machine.md) — `ledger` reuses `machine.Status` for its
  five task states instead of a new enum.
- [events.md](events.md) — `ledger` emits through a caller-owned
  `events.Bus`, matching the `machine` and `flow` emit pattern.

## Usage

```go
l, err := ledger.New(nil, nil) // nil Store defaults to MemStore
if err != nil {
    // New has no error path today; still check it
}
ctx := context.Background()
now := time.Now()
ok, err := l.Admit(ctx, "scheduler", "task-1", 1, "payload", now)
if err != nil || !ok {
    // err on a Store failure; !ok means duplicate or terminal
}
fence, err := l.Claim(ctx, "scheduler", "task-1", "worker-a", time.Minute, now)
if err != nil {
    // ErrNoKey, ErrEmptyOwner, or ErrLeaseActive
}
if err := l.Complete(ctx, "scheduler", "task-1", "worker-a", fence, ledger.StatusCompleted, time.Now()); err != nil {
    // ErrFenced or ErrNotClaimed
}
```
