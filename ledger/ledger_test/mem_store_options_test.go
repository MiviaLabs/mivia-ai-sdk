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
// unbounded, no tombstoning under load.
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
// liveCount exactly at MaxEntries never tombstones.
func TestMemStoreEvictionBoundaryNoTombstone(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 3})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
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

// TestMemStoreEvictionOldestTerminalFirst proves the oldest-queued
// terminal key tombstones first, and a claimed record is never
// tombstoned regardless of how many times the cap is exceeded.
func TestMemStoreEvictionOldestTerminalFirst(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 2})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	// "kept" stays claimed for the whole test: it must never
	// tombstone, no matter how many terminal records evict around it.
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
	if err != nil || !found {
		t.Fatalf("State(k1): found=%v err=%v", found, err)
	}
	if oldest.Task != nil {
		t.Fatalf("k1 Task = %v, want nil: the oldest-queued terminal key must tombstone first", oldest.Task)
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
// same-call eviction edge case: a cap of 1 already breached by two
// pending records, then the second key's own Complete call tombstones
// it immediately, since it becomes the sole queued entry.
func TestMemStoreEvictionSameCallTombstonesImmediately(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 1})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	if ok, err := l.Admit(ctx, testActor, "a", 1, "a-payload", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(a): ok=%v err=%v", ok, err)
	}
	if ok, err := l.Admit(ctx, testActor, "b", 1, "b-payload", fixedNow, "a"); err != nil || !ok {
		t.Fatalf("Admit(b): ok=%v err=%v", ok, err)
	}

	fence := mustClaim(t, l, ctx, "b", "owner-b")
	if err := l.Complete(ctx, testActor, "b", "owner-b", fence, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete(b): %v", err)
	}

	st, found, err := l.State(ctx, "b")
	if err != nil || !found {
		t.Fatalf("State(b): found=%v err=%v", found, err)
	}
	if st.Task != nil || st.Needs != nil {
		t.Fatalf("b = %+v, want Task and Needs both nil immediately after Complete tombstoned it in the same call", st)
	}
	if st.Owner != "" || !st.LeaseUntil.IsZero() {
		t.Fatalf("b = %+v, want Owner and LeaseUntil both zeroed by tombstoning", st)
	}
}

// TestMemStoreEvictionTolerated proves a MemStore whose every current
// record is StatusPending or StatusClaimed (no terminal entries to
// tombstone) keeps accepting Admit past the cap.
func TestMemStoreEvictionTolerated(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 1})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	keys := []ledger.IdempotencyKey{"k1", "k2", "k3"}
	for i, key := range keys {
		if ok, err := l.Admit(ctx, testActor, key, ledger.Sequence(i+1), "payload", fixedNow); err != nil || !ok {
			t.Fatalf("Admit(%s): ok=%v err=%v", key, ok, err)
		}
	}
	for _, key := range keys {
		st, found, err := l.State(ctx, key)
		if err != nil || !found {
			t.Fatalf("State(%s): found=%v err=%v", key, found, err)
		}
		if st.Task == nil {
			t.Fatalf("key %s Task is nil, want no tombstone while the terminal queue is empty", key)
		}
	}
}

// TestMemStoreEvictionPreservesIdempotency proves idempotency holds
// across eviction: a re-admission at a tombstoned key's sequence
// still reports false, nil, and Load against that key still reports
// found true with a nil Task.
func TestMemStoreEvictionPreservesIdempotency(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 2})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	admitClaimComplete(t, l, ctx, "target", 1)
	admitClaimComplete(t, l, ctx, "k1", 1)
	admitClaimComplete(t, l, ctx, "k2", 1)

	before, found, err := l.State(ctx, "target")
	if err != nil || !found {
		t.Fatalf("State(target) before re-admit: found=%v err=%v", found, err)
	}
	if before.Task != nil {
		t.Fatalf("target Task = %v, want nil: it must have tombstoned once the cap was exceeded", before.Task)
	}

	ok, err := l.Admit(ctx, testActor, "target", 99, "re-admitted", fixedNow)
	if err != nil {
		t.Fatalf("Admit(target) re-admission: %v", err)
	}
	if ok {
		t.Fatalf("Admit(target) at a higher sequence against a tombstoned terminal record: want false, got true")
	}

	after, found, err := l.State(ctx, "target")
	if err != nil || !found {
		t.Fatalf("State(target) after re-admit attempt: found=%v err=%v", found, err)
	}
	if after.Task != nil {
		t.Fatalf("target Task = %v, want still nil: a rejected re-admission must not mutate the tombstone", after.Task)
	}
}

// TestMemStoreRangeAfterEviction proves Range visits a tombstoned
// record and a live record with the right fields.
func TestMemStoreRangeAfterEviction(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 1})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	admitClaimComplete(t, l, ctx, "k1", 1)
	// Admitting k2 pushes liveCount over the cap of 1 and
	// triggers k1's eviction, since k1 is the sole queued
	// terminal entry.
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
	if len(visited) != 2 {
		t.Fatalf("visited %d keys, want 2", len(visited))
	}
	tomb, ok := visited["k1"]
	if !ok {
		t.Fatalf("Range missed k1")
	}
	if tomb.Task != nil || tomb.Needs != nil {
		t.Fatalf("tombstoned k1 = %+v, want Task and Needs both nil", tomb)
	}
	if tomb.Status != ledger.StatusCompleted {
		t.Fatalf("tombstoned k1.Status = %q, want StatusCompleted preserved", tomb.Status)
	}
	live, ok := visited["k2"]
	if !ok {
		t.Fatalf("Range missed k2")
	}
	if live.Task == nil {
		t.Fatalf("live k2.Task is nil, want the un-evicted record's payload intact")
	}
}

// TestMemStoreSnapshotBlockedAfterEviction proves a Snapshot taken after
// eviction round-trips through Encode/Decode, including a tombstoned
// StatusBlocked entry with BlockedBy intact.
func TestMemStoreSnapshotBlockedAfterEviction(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 1})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	if ok, err := l.Admit(ctx, testActor, "root", 1, "root-payload", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(root): ok=%v err=%v", ok, err)
	}
	if ok, err := l.Admit(ctx, testActor, "dep", 1, "dep-payload", fixedNow, "root"); err != nil || !ok {
		t.Fatalf("Admit(dep): ok=%v err=%v", ok, err)
	}
	fence := mustClaim(t, l, ctx, "root", "owner-root")
	if err := l.Complete(ctx, testActor, "root", "owner-root", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(root): %v", err)
	}
	// "dep" is now StatusBlocked (terminal) but not yet evicted:
	// liveCount sat at the cap after root's own eviction. A
	// further admission pushes liveCount over the cap again and
	// evicts dep next.
	mustAdmit(t, l, ctx, "extra", 1)

	depState, found, err := l.State(ctx, "dep")
	if err != nil || !found {
		t.Fatalf("State(dep): found=%v err=%v", found, err)
	}
	if depState.Status != ledger.StatusBlocked || depState.BlockedBy != "root" {
		t.Fatalf("dep = %+v, want StatusBlocked with BlockedBy root", depState)
	}
	if depState.Task != nil || depState.Needs != nil {
		t.Fatalf("dep = %+v, want a tombstoned Task and Needs", depState)
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
}
