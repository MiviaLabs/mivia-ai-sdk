package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// ledgerOver builds a Ledger over store for a test.
func ledgerOver(t *testing.T, store ledger.Store) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// admitClaimComplete drives key through Admit, Claim, then Complete
// as StatusCompleted, using a distinct owner per key.
func admitClaimComplete(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, seq ledger.Sequence) {
	t.Helper()
	ok, err := l.Admit(ctx, testActor, key, seq, "payload", fixedNow)
	if err != nil || !ok {
		t.Fatalf("Admit(%s): ok=%v err=%v", key, ok, err)
	}
	owner := ledger.OwnerID("owner-" + string(key))
	fence, err := l.Claim(ctx, testActor, key, owner, fixedLease, fixedNow)
	if err != nil {
		t.Fatalf("Claim(%s): %v", key, err)
	}
	if err := l.Complete(ctx, testActor, key, owner, fence, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete(%s): %v", key, err)
	}
}

// TestNewMemStoreWithOptionsZeroValueUnbounded proves a MemStore built
// with the zero-value MemStoreOptions behaves like NewMemStore:
// unbounded, no eviction under load.
func TestNewMemStoreWithOptionsZeroValueUnbounded(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)
	for i := 1; i <= 10; i++ {
		key := ledger.IdempotencyKey(rune('a' + i))
		admitClaimComplete(t, l, ctx, key, ledger.Sequence(i))
	}
	for i := 1; i <= 10; i++ {
		key := ledger.IdempotencyKey(rune('a' + i))
		st, found, err := l.State(ctx, key)
		if err != nil || !found {
			t.Fatalf("State(%s): found=%v err=%v", key, found, err)
		}
		if st.Task == nil {
			t.Fatalf("key %s Task is nil, want unbounded store to keep every Task", key)
		}
	}
}

// TestNewMemStoreWithOptionsNegativeMaxEntriesRejected proves a
// negative MaxEntries returns ErrInvalidMaxEntries and a nil
// *MemStore.
func TestNewMemStoreWithOptionsNegativeMaxEntriesRejected(t *testing.T) {
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: -1})
	if !errors.Is(err, ledger.ErrInvalidMaxEntries) {
		t.Fatalf("NewMemStoreWithOptions(-1): err = %v, want ErrInvalidMaxEntries", err)
	}
	if store != nil {
		t.Fatalf("NewMemStoreWithOptions(-1): store = %v, want nil", store)
	}
}

// TestMemStoreEvictionBoundaryNoTombstone proves a MemStore driven to
// exactly MaxEntries records deletes nothing and blanks nothing. The
// name is historical: MemStore deletes records now and writes no
// tombstone.
func TestMemStoreEvictionBoundaryNoTombstone(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 3)
	l := ledgerOver(t, store)
	keys := []ledger.IdempotencyKey{"k1", "k2", "k3"}
	for i, key := range keys {
		admitClaimComplete(t, l, ctx, key, ledger.Sequence(i+1))
	}
	for _, key := range keys {
		st, found, err := l.State(ctx, key)
		if err != nil || !found {
			t.Fatalf("State(%s): found=%v err=%v", key, found, err)
		}
		if st.Task == nil {
			t.Fatalf("key %s Task is nil at the exact boundary, want no eviction", key)
		}
	}
}

// TestMemStoreEvictionOldestTerminalFirst proves the oldest queued
// key is deleted first, and a record holding a live lease is never
// deleted regardless of how many times the cap is exceeded.
func TestMemStoreEvictionOldestTerminalFirst(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 2)
	l := ledgerOver(t, store)

	// "kept" holds a live lease at fixedNow for the whole test: it
	// must never be deleted, no matter how many records evict
	// around it.
	if ok, err := l.Admit(ctx, testActor, "kept", 1, "kept-payload", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(kept): ok=%v err=%v", ok, err)
	}
	mustClaim(t, l, ctx, "kept", "owner-kept")

	keys := []ledger.IdempotencyKey{"k1", "k2", "k3", "k4", "k5"}
	for i, key := range keys {
		admitClaimComplete(t, l, ctx, key, ledger.Sequence(i+1))
	}

	keptState, found, err := l.State(ctx, "kept")
	if err != nil || !found {
		t.Fatalf("State(kept): found=%v err=%v", found, err)
	}
	if keptState.Status != ledger.StatusClaimed || keptState.Task == nil {
		t.Fatalf("kept = %+v, want StatusClaimed with a live Task", keptState)
	}

	oldest, found, err := l.State(ctx, "k1")
	if err != nil || found {
		t.Fatalf("State(k1) = %+v found=%v err=%v, want found false: the oldest queued key must be deleted first", oldest, found, err)
	}

	newest, found, err := l.State(ctx, "k5")
	if err != nil || !found {
		t.Fatalf("State(k5): found=%v err=%v", found, err)
	}
	if newest.Task == nil {
		t.Fatalf("k5 Task is nil, want the most recently completed key to still be live")
	}
}

// TestMemStoreEvictionSameCallTombstonesImmediately proves the
// same-call eviction edge case: admitting "b" breaches a cap of 1 and
// deletes the head "a" inside b's own write, because "a" holds no
// lease and "b" is the exempt current key. The name is historical:
// eviction deletes the record now and writes no tombstone.
func TestMemStoreEvictionSameCallTombstonesImmediately(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 1)
	l := ledgerOver(t, store)

	if ok, err := l.Admit(ctx, testActor, "a", 1, "a-payload", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(a): ok=%v err=%v", ok, err)
	}
	if ok, err := l.Admit(ctx, testActor, "b", 1, "b-payload", fixedNow, "a"); err != nil || !ok {
		t.Fatalf("Admit(b): ok=%v err=%v", ok, err)
	}

	gone, found, err := l.State(ctx, "a")
	if err != nil || found {
		t.Fatalf("State(a) = %+v found=%v err=%v, want found false: b's own admission deletes the head", gone, found, err)
	}

	// "b" still names the deleted "a" in Needs. A missing need
	// blocks nothing, so Claim(b) succeeds.
	fence := mustClaim(t, l, ctx, "b", "owner-b")
	if err := l.Complete(ctx, testActor, "b", "owner-b", fence, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete(b): %v", err)
	}

	st, found, err := l.State(ctx, "b")
	if err != nil || !found {
		t.Fatalf("State(b): found=%v err=%v", found, err)
	}
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("b.Status = %q, want StatusCompleted: b survives its own Complete", st.Status)
	}
	if st.Task == nil {
		t.Fatalf("b = %+v, want its Task intact: eviction deletes, it never blanks", st)
	}
}

// TestMemStoreEvictionTolerated proves a MemStore whose every record
// holds a live lease keeps accepting Admit past the cap and deletes
// nothing.
func TestMemStoreEvictionTolerated(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 1)
	l := ledgerOver(t, store)

	keys := []ledger.IdempotencyKey{"k1", "k2", "k3"}
	for i, key := range keys {
		if ok, err := l.Admit(ctx, testActor, key, ledger.Sequence(i+1), "payload", fixedNow); err != nil || !ok {
			t.Fatalf("Admit(%s): ok=%v err=%v", key, ok, err)
		}
		mustClaim(t, l, ctx, key, ledger.OwnerID("owner-"+string(key)))
	}
	for _, key := range keys {
		st, found, err := l.State(ctx, key)
		if err != nil || !found {
			t.Fatalf("State(%s): found=%v err=%v", key, found, err)
		}
		if st.Task == nil {
			t.Fatalf("key %s Task is nil, want every live lease kept whole", key)
		}
	}
}

// TestMemStoreEvictionPreservesIdempotency proves idempotency is a
// bounded window under a cap: an evicted key reports found false and
// admits again. The name is historical: eviction now ends the
// idempotency guarantee for the deleted key.
func TestMemStoreEvictionPreservesIdempotency(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 2)
	l := ledgerOver(t, store)

	admitClaimComplete(t, l, ctx, "target", 1)
	admitClaimComplete(t, l, ctx, "k1", 1)
	admitClaimComplete(t, l, ctx, "k2", 1)

	before, found, err := l.State(ctx, "target")
	if err != nil || found {
		t.Fatalf("State(target) before re-admit = %+v found=%v err=%v, want found false", before, found, err)
	}

	ok, err := l.Admit(ctx, testActor, "target", 99, "re-admitted", fixedNow)
	if err != nil {
		t.Fatalf("Admit(target) re-admission: %v", err)
	}
	if !ok {
		t.Fatalf("Admit(target) after eviction: want true, got false")
	}

	after, found, err := l.State(ctx, "target")
	if err != nil || !found {
		t.Fatalf("State(target) after re-admit: found=%v err=%v", found, err)
	}
	if after.Task != "re-admitted" || after.Status != ledger.StatusPending {
		t.Fatalf("target = %+v, want the re-admitted payload at StatusPending", after)
	}
}

// TestMemStoreRangeAfterEviction proves Range visits the surviving
// record only, and never the deleted key.
func TestMemStoreRangeAfterEviction(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 1)
	l := ledgerOver(t, store)

	admitClaimComplete(t, l, ctx, "k1", 1)
	// Admitting k2 pushes the entry count over the cap of 1 and
	// deletes k1, the queue head with no live lease.
	if ok, err := l.Admit(ctx, testActor, "k2", 1, "k2-payload", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(k2): ok=%v err=%v", ok, err)
	}

	visited := map[ledger.IdempotencyKey]ledger.TaskState{}
	if err := store.Range(ctx, func(ts ledger.TaskState) bool {
		visited[ts.Key] = ts
		return true
	}); err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(visited) != 1 {
		t.Fatalf("visited %d keys, want 1", len(visited))
	}
	if gone, ok := visited["k1"]; ok {
		t.Fatalf("Range visited deleted k1 = %+v, want it absent", gone)
	}
	live, ok := visited["k2"]
	if !ok {
		t.Fatalf("Range missed k2")
	}
	if live.Task == nil {
		t.Fatalf("live k2.Task is nil, want the un-evicted record's payload intact")
	}
}

// TestMemStoreSnapshotBlockedAfterEviction proves a Snapshot taken
// after eviction round-trips through Encode/Decode, including a
// StatusBlocked entry whose failed need was deleted.
func TestMemStoreSnapshotBlockedAfterEviction(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 1)
	l := ledgerOver(t, store)

	// Fail "root" before "dep" is admitted. Admit reads root as
	// failed, so dep inserts already StatusBlocked, and dep's own
	// insert then deletes root under the cap of 1.
	if ok, err := l.Admit(ctx, testActor, "root", 1, "root-payload", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(root): ok=%v err=%v", ok, err)
	}
	fence := mustClaim(t, l, ctx, "root", "owner-root")
	if err := l.Complete(ctx, testActor, "root", "owner-root", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(root): %v", err)
	}
	if ok, err := l.Admit(ctx, testActor, "dep", 1, "dep-payload", fixedNow, "root"); err != nil || !ok {
		t.Fatalf("Admit(dep): ok=%v err=%v", ok, err)
	}

	rootState, found, err := l.State(ctx, "root")
	if err != nil || found {
		t.Fatalf("State(root) = %+v found=%v err=%v, want found false: dep's insert deletes it", rootState, found, err)
	}

	depState, found, err := l.State(ctx, "dep")
	if err != nil || !found {
		t.Fatalf("State(dep): found=%v err=%v", found, err)
	}
	if depState.Status != ledger.StatusBlocked || depState.BlockedBy != "root" {
		t.Fatalf("dep = %+v, want StatusBlocked with BlockedBy root", depState)
	}
	if depState.Task == nil {
		t.Fatalf("dep = %+v, want its Task intact: eviction deletes, it never blanks", depState)
	}

	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := ledger.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var decodedDep *ledger.TaskState
	for i := range decoded.Tasks {
		if decoded.Tasks[i].Key == "dep" {
			decodedDep = &decoded.Tasks[i]
		}
	}
	if decodedDep == nil {
		t.Fatalf("decoded snapshot missing key dep")
	}
	if decodedDep.BlockedBy != "root" {
		t.Fatalf("decoded dep.BlockedBy = %q, want root", decodedDep.BlockedBy)
	}
	if decodedDep.Task == nil {
		t.Fatalf("decoded dep = %+v, want a non-nil Task", *decodedDep)
	}
}
