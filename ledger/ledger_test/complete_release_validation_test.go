package ledger_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// plantBlockedByOnClaimed loads the current record for key, sets
// BlockedBy on it without changing Status away from StatusClaimed,
// and writes it straight through Store.CompareAndSwap. The result
// violates TaskState.Validate's rule that BlockedBy is set only under
// StatusBlocked, the same corruption shape a direct Store write can
// produce and a Ledger method must never carry forward.
func plantBlockedByOnClaimed(t *testing.T, store ledger.Store, ctx context.Context, key ledger.IdempotencyKey) ledger.TaskState {
	t.Helper()
	cur, found, err := store.Load(ctx, key)
	if err != nil || !found {
		t.Fatalf("Load(%s): found=%v err=%v", key, found, err)
	}
	next := cur
	next.BlockedBy = "some-other-key"
	ok, err := store.CompareAndSwap(ctx, key, cur, next)
	if err != nil {
		t.Fatalf("plantBlockedByOnClaimed(%s): %v", key, err)
	}
	if !ok {
		t.Fatalf("plantBlockedByOnClaimed(%s): want true", key)
	}
	// Re-load rather than return next: the Store may assign fields
	// (Rev) on write that the pre-write value does not carry, and the
	// caller compares against the record as actually stored.
	stored, found, err := store.Load(ctx, key)
	if err != nil || !found {
		t.Fatalf("Load after plant(%s): found=%v err=%v", key, found, err)
	}
	return stored
}

// TestCompleteRejectsInvalidRecord proves Complete calls
// TaskState.Validate before it writes: a record a direct Store write
// corrupted (StatusClaimed with BlockedBy set) fails Complete instead
// of being written forward, and the stored record stays unchanged.
func TestCompleteRejectsInvalidRecord(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()
	l := newLedgerOverStore(t, store)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	corrupted := plantBlockedByOnClaimed(t, store, ctx, "k1")

	err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, fixedNow)
	if err == nil {
		t.Fatalf("Complete on a corrupted record: want error, got nil")
	}
	if !strings.Contains(err.Error(), "names BlockedBy") {
		t.Fatalf("Complete error = %v, want a TaskState.Validate BlockedBy error", err)
	}

	after, found, loadErr := store.Load(ctx, "k1")
	if loadErr != nil || !found {
		t.Fatalf("Load after rejected Complete: found=%v err=%v", found, loadErr)
	}
	if !reflect.DeepEqual(after, corrupted) {
		t.Fatalf("record changed after a rejected Complete: got %+v, want %+v", after, corrupted)
	}
}

// TestReleaseRejectsInvalidRecord is TestCompleteRejectsInvalidRecord
// for Release: the same planted corruption must fail Release and
// leave the stored record unchanged.
func TestReleaseRejectsInvalidRecord(t *testing.T) {
	ctx := context.Background()
	store := ledger.NewMemStore()
	l := newLedgerOverStore(t, store)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	corrupted := plantBlockedByOnClaimed(t, store, ctx, "k1")

	err := l.Release(ctx, testActor, "k1", "owner-a", fence, fixedNow)
	if err == nil {
		t.Fatalf("Release on a corrupted record: want error, got nil")
	}
	if !strings.Contains(err.Error(), "names BlockedBy") {
		t.Fatalf("Release error = %v, want a TaskState.Validate BlockedBy error", err)
	}

	after, found, loadErr := store.Load(ctx, "k1")
	if loadErr != nil || !found {
		t.Fatalf("Load after rejected Release: found=%v err=%v", found, loadErr)
	}
	if !reflect.DeepEqual(after, corrupted) {
		t.Fatalf("record changed after a rejected Release: got %+v, want %+v", after, corrupted)
	}
}
