# Phase 42b: MemStore bounded entry cap

Status: future. Plan-only; it has not yet gone through plan review.
`ledger` (phase 34) has shipped; see `docs/plans/ledger.md`. This
phase adds one configuration knob to the existing `MemStore`. It does
not touch `Ledger`, `TaskState`, `Store`, `Snapshot`, or any existing
sentinel error's meaning.

This phase split out of `docs/plans/agents/phase42_ledger_durable_
store.md` (round 1 of that plan's review), which originally bundled
this knob with the unrelated, tag-gated `SQLiteStore` addition. The
two concerns now ship as separate, independently reviewable and
revertible phases. See `phase42_ledger_durable_store.md` for
`SQLiteStore`; this document covers `MemStoreOptions` only.

## Revision note (first revision, answers phase42 plan-review round 1)

The bundled draft this phase came from proposed evicting a terminal
record from `MemStore.tasks` by deleting it outright once `MaxEntries`
was exceeded. The plan-reviewer found this breaks idempotency: `ledger
/ledger.go`'s `Admit` only rejects a re-admission at an already-
terminal key through `admitEligible`, which reads the stored record
off `Load`. A `Load` against a deleted key reports `found == false`,
so `admitEligible` never runs, and a later `Admit` at the same
idempotency key silently re-admits and reprocesses a task the caller
already finished. This defeats the exact guarantee `ledger` exists to
give (`AGENTS.md`'s `ledger/` bullet: "idempotency-keyed admission").

This revision keeps a tombstone for every evicted key instead of
deleting it, so idempotency holds for every key `MemStore` ever
admitted, evicted or not. See "Eviction rule: tombstone, not delete"
below for the exact fields a tombstone keeps and drops.

## Revision note (round 3, closes the remaining two findings)

Two gaps remained after round 3 review, both plan-text only. First,
the eviction loop's stop condition named only "`liveCount` back at or
under the cap," omitting the already-documented "terminal queue
empty" case; a builder coding the count check alone would pop from an
empty FIFO queue. "Eviction rule: tombstone, not delete" now states
both stop conditions together, and also names and defines `liveCount`
itself, since the prior text drove the trigger off `len(tasks)`, a
counter that never shrinks once tombstoning replaces rather than
deletes a map entry. Second, the plan never addressed
`Ledger.Restore`'s interaction with `liveCount` or the eviction queue
on a `MaxEntries`-bounded `MemStore`. The Scope section's "Outside"
list now states this out of scope, with the reasoning: `Restore`
targets cold-start recovery into a fresh `Store`, not replay into an
already-bounded, already-evicting instance.

## Goal

Give `MemStore` an optional bound on how many records it holds in
memory, so a caller running `Ledger` over `MemStore` for the lifetime
of a long-running process can cap its heap growth, without losing the
idempotency guarantee `ledger` exists to give. `NewMemStore()` is
unchanged: its existing zero-argument signature and its existing
unbounded behavior both stay exactly as shipped. The knob is opt-in
through a new constructor, `NewMemStoreWithOptions`.

## Why a tombstone, not a delete

`ledger/store.go`'s real `MemStore` and `NewMemStore` today:

```go
type MemStore struct {
	mu    sync.Mutex
	tasks map[IdempotencyKey]TaskState
}

func NewMemStore() *MemStore {
	return &MemStore{tasks: make(map[IdempotencyKey]TaskState)}
}
```

No capacity or eviction knob exists. `MemStore.tasks` grows without
bound: a long-running caller sees every admitted key stay in the map
forever, including one whose task finished (`StatusCompleted`,
`StatusFailed`, or `StatusBlocked`) and will never be read or mutated
again, since `Complete` already rejects any further write to a
terminal record (`docs/plans/ledger.md`'s API section). `memory.Store`
already ships a matching, shipped precedent for this exact shape in
this module: an in-memory store that evicts the oldest-inserted entry
once a byte budget is spent (`docs/plans/memory.md`). This phase gives
`MemStore` the equivalent bound for entry count, holding to the
narrower rule `memory.Store` does not need to hold to: `ledger`'s own
idempotency contract, stated in `ledger/ledger.go:38`'s
`admitEligible` comment, must survive eviction.

A full delete of an evicted key's map entry cannot hold that contract:
`admitEligible` decides only from what `Load` returns, and a deleted
key reports `found == false`, indistinguishable from a key `Admit`
never saw. The fix keeps a tombstone: a reduced `TaskState` that still
satisfies `Load`'s `found == true` contract and still carries the
`Status` and `Sequence` fields `admitEligible` reads, while dropping
the fields that hold the bulk of a record's memory.

### What a tombstone keeps and what it drops

A tombstone keeps: `Key`, `Status`, `Sequence`, `Fence`, `Rev`, and
`BlockedBy`. `BlockedBy` stays because `TaskState.Validate` requires a
non-empty `BlockedBy` on a `StatusBlocked` record and rejects a
non-empty `BlockedBy` on any other status
(`ledger/task_state.go:94-99`); dropping it would make a tombstoned
`StatusBlocked` record fail `Validate`, breaking `Snapshot.Validate`
(`ledger/snapshot.go:29`) for any snapshot taken after an eviction.

A tombstone drops: `Task` (set to `nil`), `Needs` (set to `nil`), and
`Owner` and `LeaseUntil` (set to their zero values). `Task` is the
field eviction exists to free: it is a caller-owned `any` value with
no size bound `MemStore` can otherwise cap. `Needs` is small but
serves no purpose once a record is terminal: `Admit` never re-reads
`Needs` off a terminal record, and `Complete`'s own dependent-blocking
scan (`docs/plans/ledger.md`'s API section) walks forward from a
failing key's own dependents, not backward through a completed
record's `Needs`. `Owner` and `LeaseUntil` are safe to drop because
`TaskState.Validate` requires them only for `StatusClaimed`
(`ledger/task_state.go:100-107`), and only a terminal record is ever
tombstoned; a `StatusClaimed` or `StatusPending` record is never
evicted (see "Eviction rule" below), so `Validate`'s `StatusClaimed`
branch never sees a tombstone missing them.

`CompareAndSwap`'s compare tuple, `(Sequence, Status, Fence, Rev)`
(`ledger/store.go:20`, `ledger/store.go:73`), is unaffected: every
field that tuple reads survives tombstoning unchanged. A terminal
record is never rewritten again by `Complete`'s own existing rule
(`docs/plans/ledger.md`'s API section), so no caller ever attempts a
`CompareAndSwap` against a tombstoned key in practice; the tuple
staying intact is a defense against a caller that tries anyway, not a
path this phase expects to exercise in normal operation.

### `Load` and `Range` behavior for a tombstoned key

`Load(ctx, key)` against a tombstoned key returns the tombstone
`TaskState` and `found == true`, matching its documented contract
exactly (`ledger/store.go:48-49`): a tombstone is a real, present
record, not an absent one. A caller reading `Task` off that result
gets `nil`, not the original value: this is a real, disclosed
consequence of enabling `MaxEntries`, stated here and in the
`MemStoreOptions.MaxEntries` doc comment.

`Range(ctx, fn)` visits a tombstoned record exactly once, like any
other record, with `Task == nil` and `Needs == nil`: no special case
in `Range`'s iteration. `Snapshot` (`ledger/snapshot.go`), which walks
`Range`, correctly captures a tombstoned key's `Status`, `Sequence`,
`Fence`, `Rev`, and `BlockedBy`, but not its original `Task` or
`Needs`. `Snapshot.Validate` passes for a tombstoned `StatusBlocked`
entry because `BlockedBy` survives; it never sees a tombstoned
`StatusClaimed` entry, since only a terminal record is ever
tombstoned. This is the same disclosed limitation
`SQLiteStore`'s design in `phase42_ledger_durable_store.md` already
states for its own `Task`-serialization boundary: a caller who needs
every evicted record's original `Task` payload preserved must not
enable `MaxEntries`, or must snapshot before enough terminal records
accumulate to trigger eviction.

## Scope

Inside: `MemStoreOptions`, `NewMemStoreWithOptions`, the
`ErrInvalidMaxEntries` sentinel, and the tombstone-based eviction rule
described above, all shipped in the default build with no third-party
import.

Outside: any change to `NewMemStore`'s existing zero-argument
signature or its existing unbounded behavior. Outside: any change to
`Store`, `Ledger`, `TaskState`, `Snapshot`, or any existing sentinel
error's meaning. Outside: `SQLiteStore` and any change to
`ledger`'s tag-gated code; see
`docs/plans/agents/phase42_ledger_durable_store.md`. Outside: an
eviction or capacity knob on `SQLiteStore`; a durable, disk-backed
store has no equivalent in-process-growth concern the way `MemStore`
does, since its cost lives on disk, not in the process's own heap.
Outside: any way to recover a tombstoned record's original `Task` or
`Needs` value; once evicted, that data is gone, by design, and a
caller who cannot accept that must not enable `MaxEntries`. Outside:
`Ledger.Restore`'s (`ledger/snapshot.go:36-47`) interaction with
`liveCount` or the eviction queue on a `MaxEntries`-bounded `MemStore`.
`Restore` is for cold-start recovery into a fresh `Store`
(`ledger/snapshot.go:36-37`'s own doc comment), not for replaying a
snapshot into the same, already-bounded, already-evicting store
instance. Every restored key inserts through the same zero-old
`CompareAndSwap` path `Admit` uses, so `liveCount` would count a
restored tombstone-shaped record as live: no `TaskState` field marks a
record as an original tombstone versus a genuine live one. `Restore`
also has no original-insertion-order signal to rebuild the FIFO queue
in true admission order. A caller who calls `Restore` against a
`MaxEntries`-bounded `MemStore` accepts that `liveCount` and eviction
order may not reflect the pre-snapshot state.

## API

Locked in `api/ledger.txt` through a plain `make api-update`; no build
tag applies to any symbol in this phase.

- `type MemStoreOptions struct { MaxEntries int }` — `MaxEntries` caps
  the number of records `MemStore` holds; zero means unbounded,
  matching `NewMemStore`'s existing behavior exactly. A positive
  `MaxEntries` evicts a terminal record's `Task` and `Needs` payload
  once the cap is exceeded, replacing it with a tombstone that still
  answers `Load` and `Range` and still rejects re-admission through
  `Admit`.
- `func NewMemStoreWithOptions(opts MemStoreOptions) (*MemStore, error)`
  — builds an empty `MemStore` honoring `opts`. Returns a wrapped
  `ErrInvalidMaxEntries` for a negative `MaxEntries`.
- `var ErrInvalidMaxEntries error` — in `ledger/errors.go`, alongside
  the package's existing sentinels. Returned, wrapped with context, by
  `NewMemStoreWithOptions` for a negative `MaxEntries`. Checked with
  `errors.Is`, matching every other sentinel in this package
  (`ErrNoKey`, `ErrFenced`, `ErrNotStale`, `ErrNotClaimed`,
  `ErrUnknownStatus`); this phase adds the sentinel instead of
  returning a bare `errors.New` string, since nothing else in `ledger`
  does that.

No change to `NewMemStore`'s existing zero-argument signature or its
existing unbounded behavior. No change to `Store`, `Ledger`,
`TaskState`, `Snapshot`, or any existing sentinel error's meaning.

## Eviction rule: tombstone, not delete

Scoped to stay safe under `Complete`'s own already-shipped invariants:
only a record already at a terminal status (`StatusCompleted`,
`StatusFailed`, `StatusBlocked`) is ever tombstoned. `CompareAndSwap`
never tombstones a `StatusPending` or `StatusClaimed` record: an
active claim or an unclaimed-but-live task must never silently lose
its `Task` payload out from under a caller mid-lifecycle, the same
safety bar `Complete`'s own terminal-status immutability rule already
sets.

`MemStore` keeps a small FIFO queue of keys that reached a terminal
status, appended to exactly once per key, on the same `CompareAndSwap`
call that writes the terminal status. A terminal record is never
rewritten again, by `Complete`'s own existing rule, so no key is ever
queued twice. `MemStore` also keeps a `liveCount` counter, separate
from `len(tasks)`: `liveCount` counts every key with a real, non-
tombstoned record, incremented on each new key `Admit` inserts and
decremented on each key `CompareAndSwap` tombstones. `len(tasks)`
alone cannot drive eviction: tombstoning replaces a map entry rather
than deleting it, so `len(tasks)` never shrinks and would leave the
cap permanently exceeded after the first breach, forcing every
terminal record to tombstone immediately instead of the bounded set
`MaxEntries` promises.

When `MaxEntries` is positive and `liveCount` exceeds it after a
successful write, `CompareAndSwap` pops the oldest-queued key and
replaces its map entry with the tombstone described above (`Task` and
`Needs` cleared, `Owner` and `LeaseUntil` zeroed, every other field
unchanged), decrementing `liveCount`. The loop repeats until one of
two stop conditions is met, whichever comes first: `liveCount` is back
at or under the cap, or the terminal queue is empty. A builder must
code both conditions, not just the count check, or the loop pops from
an empty queue once terminal keys run out before the cap is satisfied.
Eviction order mirrors `memory.Store`'s own oldest-inserted eviction
order. Once a key is tombstoned, it is never queued or tombstoned
again: replacing its map entry does not touch the FIFO queue further
for that key.

When every current record is still `StatusPending` or `StatusClaimed`
(the terminal queue is empty) and the cap is still exceeded, `MemStore`
does not tombstone anything: `MaxEntries` bounds what it safely can,
not a hard ceiling `CompareAndSwap` would otherwise have to fail
against. This is a documented, deliberate limit, not a bug: a caller
who needs a hard ceiling even over live claims needs a different
admission-throttling layer above `Ledger`, outside `Store`'s own
contract.

`NewMemStoreWithOptions` rejects a negative `MaxEntries` with
`ErrInvalidMaxEntries`, wrapped with the field name and the negative
value passed, matching `contextbudget.Limits.Validate`'s own
negative-field rejection style but using a checkable sentinel instead
of a bare string, per the sentinel-error convention noted in API
above. `NewMemStore()` is defined in terms of
`NewMemStoreWithOptions` only internally
(`NewMemStoreWithOptions(MemStoreOptions{})`, ignoring the impossible
error a zero-value, non-negative `MaxEntries` can never produce), not
as two independent implementations.

## Tests

`ledger/ledger_test/mem_store_options_test.go` (default build, no
tag, runs under the module's existing `go test ./...`):

- `NewMemStoreWithOptions(MemStoreOptions{})` behaves identically to
  `NewMemStore()`: unbounded, no tombstoning under any load.
- A negative `MaxEntries` returns `ErrInvalidMaxEntries` (asserted with
  `errors.Is`) and a nil `*MemStore`.
- A `MemStore` built with a small positive `MaxEntries`, driven through
  a sequence of `Admit`-then-`Complete` calls (via a `Ledger`) that
  pushes the terminal-record count past the cap: the oldest-terminal
  key tombstones first, and `MemStore` never tombstones a
  `StatusPending` or `StatusClaimed` record, asserted by keeping a
  claimed key alive throughout and confirming `Load` still returns its
  full, non-tombstoned record after the cap is exceeded several times
  over.
- A `MemStore` whose every current record is still `StatusPending` or
  `StatusClaimed` (no terminal entries to tombstone) keeps accepting
  further `Admit` calls past the cap, proving the documented "bounds
  what it safely can" limit rather than a hard failure.
- The idempotency case the plan-reviewer required: admit a key,
  complete it (terminal status), force eviction by driving further
  `Admit`-then-`Complete` cycles until `MaxEntries` is exceeded and the
  original key's record tombstones, then call `Admit` again at the
  same idempotency key and at a sequence higher than the original.
  Assert `Admit` returns `false, nil`: the tombstone's surviving
  `Status` and `Sequence` fields make `admitEligible` reject the
  re-admission exactly as it would against a non-evicted terminal
  record, proving idempotency holds across eviction. A parallel case
  asserts `Load` against the same evicted key still reports
  `found == true` with a `nil` `Task`, distinguishing this from a
  key `Admit` never saw.
- `Range` over a `MemStore` holding one tombstoned key and one live
  key visits both, with the tombstoned entry's `Task` and `Needs`
  reported as `nil` and every other field matching the pre-eviction
  values; a `Snapshot` taken after eviction round-trips through
  `Encode`/`Decode` without a `Validate` error, including a
  tombstoned `StatusBlocked` entry with its `BlockedBy` intact.

## Verification

- `make verify` passes for the default build, with `MemStoreOptions`,
  `NewMemStoreWithOptions`, `ErrInvalidMaxEntries`, and the
  tombstone-based eviction path compiled in and covered like any other
  default-build code: gofmt, vet, the default test run (including
  `mem_store_options_test.go`), the doc gate, the structure gate, the
  Semgrep scan and probes, and the coverage floor all run as they do
  today, with the new lines and their test file counted into the
  coverage computation.
- The coverage floor of 85 holds for `ledger` and for the total, with
  `mem_store_options_test.go`'s new lines counted in, under the
  default `go test -cover ./...` run.
- `python3 scripts/check_structure.py` passes: every touched file
  stays at or below 500 lines and every function at or below 80
  lines.
- `api/ledger.txt` gains `MemStoreOptions`, `NewMemStoreWithOptions`,
  and `ErrInvalidMaxEntries` through a plain `make api-update`,
  committed in the same change as the code.
- `policy/layers.json`'s `"ledger": ["machine", "events"]` row is
  unchanged: this phase adds no new internal or third-party import.
- `docs/plans/ledger.md` gains a note naming `MemStoreOptions`'s
  bounded-entry-cap knob next to `MemStore`, including the tombstone
  behavior and its `Task`/`Needs`-loss consequence. `docs/architecture.
  md` needs no change: no package or message-flow edge changes.
  `docs/protocol-design.md` does not change: this phase adds no
  message-semantics rule to the envelope wire format.
