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
- `TaskState` — the full record for one key: `Key`, `Status`,
  `Sequence`, `Owner`, `Fence`, `LeaseUntil`, `Needs`, `BlockedBy`,
  `Task`, `Rev`. `Task` is caller-owned; `ledger` never inspects it.
  `Rev` is a `Store`-assigned revision counter.
- `Store` — the pluggable record backend: `Load`, `CompareAndSwap`,
  `Range`.
- `MemStore` — the shipped mutex-guarded `Store`.
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
- `NewMemStore()` — builds an empty `MemStore`.
- `Ledger.Admit(ctx, key, seq, task, needs...)` — records a task once
  per key. Returns `false, nil`, not an error, for a duplicate or a
  post-completion resubmission.
- `Ledger.Claim(ctx, key, owner, lease, now)` — claims a pending
  record, or a claimed record whose lease has expired.
- `Ledger.Renew(ctx, key, owner, fence, lease, now)` — extends the
  lease under the current fence.
- `Ledger.Release(ctx, key, owner, fence)` — returns a claimed record
  to pending.
- `Ledger.Takeover(ctx, key, owner, lease, now)` — claims a stale
  claimed record and fences the dispossessed owner's token.
- `Ledger.Complete(ctx, key, owner, fence, status)` — marks a claimed
  record `StatusCompleted` or `StatusFailed`. A failed completion
  blocks every dependent, transitively.
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

## Sentinel errors

Use `errors.Is` to test these.

- `ErrLeaseActive` — `Claim` found another owner's live lease.
- `ErrFenced` — `Renew`, `Release`, or `Complete` got a stale fence.
- `ErrNotStale` — `Takeover` found a lease that has not expired.
- `ErrNotClaimed` — the record's `Status` is not `StatusClaimed` when
  a claim-scoped operation needs it to be, or `Claim`/`Takeover` found
  a status outside their own eligible set.
- `ErrNoKey` — the key has no admitted record.
- `ErrUnknownStatus` — `Complete` got a `status` other than
  `StatusCompleted` or `StatusFailed`.

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
- `MemStore.CompareAndSwap` compares `old` against the stored record
  on `(Sequence, Status, Fence, Rev)`, and bumps `Rev` by one on every
  successful write, closing the blind spot two concurrent same-fence
  `Renew` calls would otherwise leave.

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
ok, err := l.Admit(ctx, "task-1", 1, "payload")
if err != nil || !ok {
    // err on a Store failure; !ok means duplicate or terminal
}
fence, err := l.Claim(ctx, "task-1", "worker-a", time.Minute, time.Now())
if err != nil {
    // ErrNoKey or ErrLeaseActive
}
if err := l.Complete(ctx, "task-1", "worker-a", fence, ledger.StatusCompleted); err != nil {
    // ErrFenced or ErrNotClaimed
}
```
