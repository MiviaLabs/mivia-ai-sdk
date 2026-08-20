package ledger

import (
	"context"
	"testing"
	"time"
)

// storeClock is the pinned clock every fixed-Now case in this file
// uses. It is not the wall clock, so a lease built from it is live or
// expired by construction, never by test timing.
var storeClock = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// evictionStore builds a MemStore capped at maxEntries with its clock
// pinned to storeClock.
func evictionStore(t *testing.T, maxEntries int) *MemStore {
	t.Helper()
	m, err := NewMemStoreWithOptions(MemStoreOptions{
		MaxEntries: maxEntries,
		Now:        func() time.Time { return storeClock },
	})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	return m
}

// insertRecord inserts key through the insert branch of
// CompareAndSwap and fails the test when the write is rejected.
func insertRecord(t *testing.T, m *MemStore, key IdempotencyKey, rec TaskState) {
	t.Helper()
	rec.Key = key
	ok, err := m.CompareAndSwap(context.Background(), key, TaskState{}, rec)
	if err != nil || !ok {
		t.Fatalf("CompareAndSwap(insert %s): ok=%v err=%v", key, ok, err)
	}
}

// terminal builds a StatusCompleted record carrying a non-nil Task.
func terminal() TaskState {
	return TaskState{Status: StatusCompleted, Task: "payload"}
}

// claimed builds a StatusClaimed record whose lease ends at until.
func claimed(until time.Time) TaskState {
	return TaskState{Status: StatusClaimed, Owner: "owner", LeaseUntil: until, Task: "payload"}
}

// TestEvictionDeletesRecords proves eviction deletes the map entry
// and its queue slot, so the entry count really tracks MaxEntries.
func TestEvictionDeletesRecords(t *testing.T) {
	m := evictionStore(t, 2)
	for _, k := range []IdempotencyKey{"k1", "k2", "k3", "k4", "k5"} {
		insertRecord(t, m, k, terminal())
	}
	if len(m.tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(m.tasks))
	}
	if len(m.evictQueue) != 2 {
		t.Fatalf("len(evictQueue) = %d, want 2", len(m.evictQueue))
	}
}

// TestEvictionReclaimsExpiredLease proves a claimed record whose
// lease already expired is an eviction candidate.
func TestEvictionReclaimsExpiredLease(t *testing.T) {
	m := evictionStore(t, 2)
	expired := storeClock.Add(-time.Hour)
	for _, k := range []IdempotencyKey{"k1", "k2", "k3"} {
		insertRecord(t, m, k, claimed(expired))
	}
	if len(m.tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(m.tasks))
	}
	if _, ok := m.tasks["k1"]; ok {
		t.Fatalf("k1 still present, want the oldest expired claim deleted")
	}
}

// TestEvictionKeepsLiveLease proves a live lease is never deleted,
// even when every stored record holds one and the cap is breached.
func TestEvictionKeepsLiveLease(t *testing.T) {
	m := evictionStore(t, 1)
	live := storeClock.Add(time.Hour)
	keys := []IdempotencyKey{"k1", "k2", "k3"}
	for _, k := range keys {
		insertRecord(t, m, k, claimed(live))
	}
	if len(m.tasks) != 3 {
		t.Fatalf("len(tasks) = %d, want 3: a live lease is never deleted", len(m.tasks))
	}
	for _, k := range keys {
		rec, ok := m.tasks[k]
		if !ok {
			t.Fatalf("%s missing, want every live lease kept", k)
		}
		if rec.Task == nil {
			t.Fatalf("%s Task is nil, want the payload intact", k)
		}
	}
}

// TestEvictionEvictsPendingRecord proves a pending record is an
// eviction candidate, which the tombstone rule never allowed.
func TestEvictionEvictsPendingRecord(t *testing.T) {
	m := evictionStore(t, 1)
	insertRecord(t, m, "k1", TaskState{Status: StatusPending, Task: "payload"})
	insertRecord(t, m, "k2", TaskState{Status: StatusPending, Task: "payload"})
	if len(m.tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(m.tasks))
	}
	if _, ok := m.tasks["k1"]; ok {
		t.Fatalf("k1 still present, want the pending head deleted")
	}
}

// TestEvictionProtectsCurrentKey proves the write's own key survives
// its own eviction round, even when it is the only candidate.
func TestEvictionProtectsCurrentKey(t *testing.T) {
	m := evictionStore(t, 1)
	insertRecord(t, m, "held", claimed(storeClock.Add(time.Hour)))
	insertRecord(t, m, "fresh", TaskState{Status: StatusPending, Task: "payload"})
	if _, ok := m.tasks["fresh"]; !ok {
		t.Fatalf("fresh missing, want the current write's key exempt from its own round")
	}
	if _, ok := m.tasks["held"]; !ok {
		t.Fatalf("held missing, want the live lease kept")
	}
}

// TestEvictionScanBudgetStops proves one round rotates at most
// evictScanBudget protected heads, and that the next write resumes
// where the budget stopped.
func TestEvictionScanBudgetStops(t *testing.T) {
	m := evictionStore(t, 9)
	live := storeClock.Add(time.Hour)
	for _, k := range []IdempotencyKey{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9"} {
		insertRecord(t, m, k, claimed(live))
	}
	insertRecord(t, m, "done", terminal())
	if len(m.tasks) != 10 {
		t.Fatalf("len(tasks) = %d, want 10: the budget stops the round inside the live run", len(m.tasks))
	}
	if _, ok := m.tasks["done"]; !ok {
		t.Fatalf("done missing, want it kept until a later round reaches it")
	}

	cur := m.tasks["k1"]
	ok, err := m.CompareAndSwap(context.Background(), "k1", cur, cur)
	if err != nil || !ok {
		t.Fatalf("CompareAndSwap(update k1): ok=%v err=%v", ok, err)
	}
	if len(m.tasks) != 9 {
		t.Fatalf("len(tasks) = %d, want 9: the next round reaches the deletable key", len(m.tasks))
	}
	if _, ok := m.tasks["done"]; ok {
		t.Fatalf("done still present, want the terminal record deleted once the head passed the live run")
	}
}

// TestEvictionRaisesFenceFloor proves deletion raises the store-wide
// fence floor, and that the floor never lowers a higher fence.
func TestEvictionRaisesFenceFloor(t *testing.T) {
	m := evictionStore(t, 1)
	high := terminal()
	high.Fence = 7
	insertRecord(t, m, "a", high)
	insertRecord(t, m, "b", terminal())
	if _, ok := m.tasks["a"]; ok {
		t.Fatalf("a still present, want it deleted under the cap")
	}

	insertRecord(t, m, "a", terminal())
	if got := m.tasks["a"].Fence; got != 7 {
		t.Fatalf("re-admitted a.Fence = %d, want 7 from the fence floor", got)
	}

	above := terminal()
	above.Fence = 9
	insertRecord(t, m, "c", above)
	if got := m.tasks["c"].Fence; got != 9 {
		t.Fatalf("c.Fence = %d, want 9: the floor must not lower a higher fence", got)
	}
}

// TestNoEvictionWhenUnbounded proves a zero MaxEntries deletes
// nothing.
func TestNoEvictionWhenUnbounded(t *testing.T) {
	m := evictionStore(t, 0)
	for i := 0; i < 10; i++ {
		insertRecord(t, m, IdempotencyKey(rune('a'+i)), terminal())
	}
	if len(m.tasks) != 10 {
		t.Fatalf("len(tasks) = %d, want 10", len(m.tasks))
	}
	for i := 0; i < 10; i++ {
		rec, ok := m.tasks[IdempotencyKey(rune('a'+i))]
		if !ok || rec.Task == nil {
			t.Fatalf("record %d = %+v found=%v, want it kept with its Task", i, rec, ok)
		}
	}
}

// TestNilNowResolvesToWallClock proves a nil MemStoreOptions.Now
// reads the wall clock: a past lease evicts and a future lease does
// not.
func TestNilNowResolvesToWallClock(t *testing.T) {
	cases := []struct {
		name      string
		leaseFrom time.Duration
		wantKept  bool
	}{
		{name: "expired lease evicts", leaseFrom: -time.Hour, wantKept: false},
		{name: "live lease survives", leaseFrom: time.Hour, wantKept: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewMemStoreWithOptions(MemStoreOptions{MaxEntries: 1})
			if err != nil {
				t.Fatalf("NewMemStoreWithOptions: %v", err)
			}
			insertRecord(t, m, "held", claimed(time.Now().Add(tc.leaseFrom)))
			insertRecord(t, m, "fresh", terminal())
			if _, ok := m.tasks["held"]; ok != tc.wantKept {
				t.Fatalf("held kept = %v, want %v", ok, tc.wantKept)
			}
		})
	}
}
