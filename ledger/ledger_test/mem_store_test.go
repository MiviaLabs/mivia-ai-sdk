package ledger_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestMemStoreCompareAndSwapComparesFourFields proves CompareAndSwap
// rejects a call whose old (Sequence, Status, Fence, Rev) tuple does
// not match the stored record's, even when other fields (Task) differ,
// and accepts a call whose old is the zero value against an absent key.
func TestMemStoreCompareAndSwapComparesFourFields(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()

	ok, err := store.CompareAndSwap(ctx, "k1", ledger.TaskState{}, ledger.TaskState{
		Key: "k1", Status: ledger.StatusPending, Sequence: 1, Task: "first",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !ok {
		t.Fatalf("insert against absent key with zero-value old: want true")
	}

	stored, found, err := store.Load(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}

	// old carries a matching (Sequence, Status, Fence) triple but a
	// different Task; a stale Rev must still reject the call.
	stale := stored
	stale.Rev = stored.Rev + 1
	stale.Task = "different"
	ok, err = store.CompareAndSwap(ctx, "k1", stale, ledger.TaskState{
		Key: "k1", Status: ledger.StatusPending, Sequence: 1, Task: "second",
	})
	if err != nil {
		t.Fatalf("stale CompareAndSwap: %v", err)
	}
	if ok {
		t.Fatalf("CompareAndSwap with a stale Rev: want false")
	}

	// A matching four-field tuple, with a differing Task, succeeds.
	ok, err = store.CompareAndSwap(ctx, "k1", stored, ledger.TaskState{
		Key: "k1", Status: ledger.StatusPending, Sequence: 1, Task: "third",
	})
	if err != nil {
		t.Fatalf("matching CompareAndSwap: %v", err)
	}
	if !ok {
		t.Fatalf("CompareAndSwap with a matching tuple: want true")
	}
}

// TestMemStoreCompareAndSwapBumpsRevOnEveryWrite proves Rev bumps by
// one on every successful call, including a Renew-shaped write that
// changes only LeaseUntil, and that a newly inserted record starts at
// Rev zero.
func TestMemStoreCompareAndSwapBumpsRevOnEveryWrite(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()

	if ok, err := store.CompareAndSwap(ctx, "k1", ledger.TaskState{}, ledger.TaskState{
		Key: "k1", Status: ledger.StatusPending, Sequence: 1,
	}); err != nil || !ok {
		t.Fatalf("insert: ok=%v err=%v", ok, err)
	}
	first, _, _ := store.Load(ctx, "k1")
	if first.Rev != 0 {
		t.Fatalf("Rev after insert = %d, want 0", first.Rev)
	}

	leaseWrite := first
	leaseWrite.LeaseUntil = fixedNow
	if ok, err := store.CompareAndSwap(ctx, "k1", first, leaseWrite); err != nil || !ok {
		t.Fatalf("lease write: ok=%v err=%v", ok, err)
	}
	second, _, _ := store.Load(ctx, "k1")
	if second.Rev != first.Rev+1 {
		t.Fatalf("Rev after lease write = %d, want %d", second.Rev, first.Rev+1)
	}

	// A second call whose old.Rev still carries the pre-write value
	// fails, even though it targets the same LeaseUntil field.
	staleWrite := second
	staleWrite.LeaseUntil = fixedNow.Add(fixedLease)
	ok, err := store.CompareAndSwap(ctx, "k1", first, staleWrite)
	if err != nil {
		t.Fatalf("stale second write: %v", err)
	}
	if ok {
		t.Fatalf("second write with a pre-write Rev: want false")
	}
}

// TestMemStoreCompareAndSwapRejectsNonZeroBaselineAgainstAbsentKey
// proves CompareAndSwap rejects an insert attempt whose old is not the
// zero value, even when the key has no stored record: only a
// zero-value old inserts against an absent key.
func TestMemStoreCompareAndSwapRejectsNonZeroBaselineAgainstAbsentKey(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()

	ok, err := store.CompareAndSwap(ctx, "ghost", ledger.TaskState{Sequence: 1}, ledger.TaskState{
		Key: "ghost", Status: ledger.StatusPending,
	})
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if ok {
		t.Fatalf("CompareAndSwap with a nonzero old against an absent key: want false")
	}
	if _, found, _ := store.Load(ctx, "ghost"); found {
		t.Fatalf("rejected CompareAndSwap must not create a record")
	}
}

// TestMemStoreRangeStopsEarlyWhenFnReturnsFalse proves Range stops
// iterating the first time fn returns false, matching its documented
// early-stop contract.
func TestMemStoreRangeStopsEarlyWhenFnReturnsFalse(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()
	for _, k := range []ledger.IdempotencyKey{"a", "b", "c"} {
		if ok, err := store.CompareAndSwap(ctx, k, ledger.TaskState{}, ledger.TaskState{
			Key: k, Status: ledger.StatusPending,
		}); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", k, ok, err)
		}
	}
	visits := 0
	err := store.Range(ctx, func(ledger.TaskState) bool {
		visits++
		return false
	})
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if visits != 1 {
		t.Fatalf("visits = %d, want 1 (Range must stop on the first false)", visits)
	}
}

// TestMemStoreRangeVisitsEveryRecordOnce proves a Range call whose fn
// populates a slice from a store with several records visits every
// record exactly once and returns nil.
func TestMemStoreRangeVisitsEveryRecordOnce(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()
	keys := []ledger.IdempotencyKey{"a", "b", "c"}
	for _, k := range keys {
		if ok, err := store.CompareAndSwap(ctx, k, ledger.TaskState{}, ledger.TaskState{
			Key: k, Status: ledger.StatusPending, Sequence: 1,
		}); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", k, ok, err)
		}
	}
	visits := map[ledger.IdempotencyKey]int{}
	err := store.Range(ctx, func(t ledger.TaskState) bool {
		visits[t.Key]++
		return true
	})
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	for _, k := range keys {
		if visits[k] != 1 {
			t.Fatalf("key %s visited %d times, want 1", k, visits[k])
		}
	}
	if len(visits) != len(keys) {
		t.Fatalf("visited %d distinct keys, want %d", len(visits), len(keys))
	}
}
