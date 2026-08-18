package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// rejectingStore wraps a ledger.Store and makes every CompareAndSwap
// call report a losing race (false, nil), forcing a caller's retry
// loop to observe ctx.Err() on its next iteration.
type rejectingStore struct {
	ledger.Store
}

func (r rejectingStore) CompareAndSwap(ctx context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	return false, nil
}

// flakyCtx reports Err() as nil on its first call and as
// context.Canceled on every call after, letting a test drive a
// method's retry loop past its first Load, into a losing
// CompareAndSwap, and into the loop's own ctx.Err() check, all
// without a real deadline or a background cancel.
type flakyCtx struct {
	context.Context
	calls int
}

func newFlakyCtx() *flakyCtx { return &flakyCtx{Context: context.Background()} }

func (f *flakyCtx) Err() error {
	f.calls++
	if f.calls >= 2 {
		return context.Canceled
	}
	return nil
}

// TestRetryLoopsReturnCtxErrOnLosingCompareAndSwap proves every
// method that loops on a losing CompareAndSwap returns ctx.Err() once
// the caller's context is canceled, instead of looping forever.
func TestRetryLoopsReturnCtxErrOnLosingCompareAndSwap(t *testing.T) {
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if _, err := l.Admit(ctx, "k1", 1, nil); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	if _, err := l.Admit(ctx, "k3", 1, nil); err != nil {
		t.Fatalf("Admit: %v", err)
	}

	stuck, err := ledger.New(rejectingStore{base}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := stuck.Admit(newFlakyCtx(), "k2", 1, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Admit: got %v, want context.Canceled", err)
	}
	if _, err := stuck.Claim(newFlakyCtx(), "k3", "owner-b", fixedLease, fixedNow); !errors.Is(err, context.Canceled) {
		t.Fatalf("Claim: got %v, want context.Canceled", err)
	}
	if err := stuck.Renew(newFlakyCtx(), "k1", "owner-a", fence, fixedLease, fixedNow); !errors.Is(err, context.Canceled) {
		t.Fatalf("Renew: got %v, want context.Canceled", err)
	}
	if err := stuck.Release(newFlakyCtx(), "k1", "owner-a", fence); !errors.Is(err, context.Canceled) {
		t.Fatalf("Release: got %v, want context.Canceled", err)
	}
	stale := fixedNow.Add(fixedLease)
	if _, err := stuck.Takeover(newFlakyCtx(), "k1", "owner-b", fixedLease, stale); !errors.Is(err, context.Canceled) {
		t.Fatalf("Takeover: got %v, want context.Canceled", err)
	}
	if err := stuck.Complete(newFlakyCtx(), "k1", "owner-a", fence, ledger.StatusCompleted); !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete: got %v, want context.Canceled", err)
	}
}

// TestStoreMethodsReturnCtxErrWhenCanceled proves MemStore's Load,
// CompareAndSwap, and Range each check ctx.Err() before doing any
// work.
func TestStoreMethodsReturnCtxErrWhenCanceled(t *testing.T) {
	store := ledger.NewMemStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := store.Load(ctx, "k1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load: got %v, want context.Canceled", err)
	}
	if _, err := store.CompareAndSwap(ctx, "k1", ledger.TaskState{}, ledger.TaskState{Key: "k1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CompareAndSwap: got %v, want context.Canceled", err)
	}
	if err := store.Range(ctx, func(ledger.TaskState) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Range: got %v, want context.Canceled", err)
	}
}

// TestSnapshotEncodeRejectsInvalidRecord proves Encode returns an
// error when the snapshot holds an invalid TaskState.
func TestSnapshotEncodeRejectsInvalidRecord(t *testing.T) {
	snap := ledger.Snapshot{Tasks: []ledger.TaskState{{Key: ""}}}
	if _, err := snap.Encode(); err == nil {
		t.Fatalf("Encode: want error for an invalid record")
	}
}

// TestRestoreRejectsAlreadyPresentKey proves Restore reports an error
// the first time a snapshot key already has a record.
func TestRestoreRejectsAlreadyPresentKey(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := l.Restore(ctx, snap); err == nil {
		t.Fatalf("Restore: want error for an already-present key")
	}
}

// errorStore always fails Load with a fixed error.
type errorStore struct {
	ledger.Store
}

var errStoreBoom = errors.New("boom")

func (errorStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	return ledger.TaskState{}, false, errStoreBoom
}

// TestBlockedPropagatesStoreLoadError proves Blocked propagates a
// Store.Load failure instead of masking it as "not blocked".
func TestBlockedPropagatesStoreLoadError(t *testing.T) {
	l, err := ledger.New(errorStore{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, err = l.Blocked(context.Background(), "k1")
	if !errors.Is(err, errStoreBoom) {
		t.Fatalf("Blocked: got %v, want errStoreBoom", err)
	}
}
