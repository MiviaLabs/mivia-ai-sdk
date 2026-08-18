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

// dependentCasBlockStore wraps a *ledger.MemStore and makes every
// CompareAndSwap call against one fixed key report a losing race
// (false, nil), while every other key passes through to the real
// MemStore logic. Every forwarded call uses context.Background()
// internally, so MemStore's own ctx.Err() checks in Load,
// CompareAndSwap, and Range never observe the caller's context. This
// isolates a caller's explicit ctx.Err() check, such as blockOne's
// own retry loop, from the internal checks MemStore performs on every
// call: without this isolation, a canceled context reaching MemStore
// through the primary-key CompareAndSwap or an ordinary Load returns
// context.Canceled from that unrelated internal check first, before
// the target loop's own check ever runs, making a test built on the
// shared-counter flakyCtx pattern pass for the wrong reason.
type dependentCasBlockStore struct {
	base     *ledger.MemStore
	blockKey ledger.IdempotencyKey
}

func (d dependentCasBlockStore) Load(_ context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	return d.base.Load(context.Background(), key)
}

func (d dependentCasBlockStore) CompareAndSwap(_ context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	if key == d.blockKey {
		return false, nil
	}
	return d.base.CompareAndSwap(context.Background(), key, old, new)
}

func (d dependentCasBlockStore) Range(_ context.Context, fn func(ledger.TaskState) bool) error {
	return d.base.Range(context.Background(), fn)
}

// TestBlockOneReturnsCtxErrOnLosingCompareAndSwap proves blockOne's
// own retry loop returns ctx.Err() once the caller's context is
// canceled after a losing CompareAndSwap against a dependent key,
// distinct from TestRetryLoopsReturnCtxErrOnLosingCompareAndSwap,
// which only drives Complete's primary-key loop. dependentCasBlockStore
// isolates MemStore's internal ctx.Err() checks from flakyCtx's shared
// call counter, so the only two Err() calls the test observes are
// blockOne's own two iterations, not an internal check reached earlier
// through the primary key or an ordinary Load.
func TestBlockOneReturnsCtxErrOnLosingCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "dep", 1, "root")
	fence := mustClaim(t, l, ctx, "root", "owner-a")

	stuck, err := ledger.New(dependentCasBlockStore{base: base, blockKey: "dep"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := stuck.Complete(newFlakyCtx(), "root", "owner-a", fence, ledger.StatusFailed); !errors.Is(err, context.Canceled) {
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

// casErrorStore wraps a ledger.Store and makes every CompareAndSwap
// call fail with errStoreBoom, forcing a caller to observe a genuine
// Store failure instead of the "losing race" (false, nil) shape
// rejectingStore produces.
type casErrorStore struct {
	ledger.Store
}

func (casErrorStore) CompareAndSwap(ctx context.Context, key ledger.IdempotencyKey, old, new ledger.TaskState) (bool, error) {
	return false, errStoreBoom
}

// rangeErrorStore wraps a ledger.Store and makes every Range call fail
// with errStoreBoom.
type rangeErrorStore struct {
	ledger.Store
}

func (rangeErrorStore) Range(ctx context.Context, fn func(ledger.TaskState) bool) error {
	return errStoreBoom
}

// TestMutatingMethodsPropagateLoadError proves Admit, Claim, Renew,
// Release, Takeover, and Complete each return a genuine Store.Load
// failure unchanged, instead of masking it as a sentinel rejection.
// errorStore only exercised this for Blocked; every mutating method's
// own "if err != nil { return err }" right after Store.Load was
// otherwise unreachable by any test in this package.
func TestMutatingMethodsPropagateLoadError(t *testing.T) {
	ctx := context.Background()
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")

	lf, err := ledger.New(errorStore{base}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := lf.Admit(ctx, "k2", 1, nil); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Admit: got %v, want errStoreBoom", err)
	}
	if _, err := lf.Claim(ctx, "k1", "owner-b", fixedLease, fixedNow); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Claim: got %v, want errStoreBoom", err)
	}
	if err := lf.Renew(ctx, "k1", "owner-a", fence, fixedLease, fixedNow); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Renew: got %v, want errStoreBoom", err)
	}
	if err := lf.Release(ctx, "k1", "owner-a", fence); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Release: got %v, want errStoreBoom", err)
	}
	if _, err := lf.Takeover(ctx, "k1", "owner-b", fixedLease, fixedNow); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Takeover: got %v, want errStoreBoom", err)
	}
	if err := lf.Complete(ctx, "k1", "owner-a", fence, ledger.StatusCompleted); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Complete: got %v, want errStoreBoom", err)
	}
}

// TestMutatingMethodsPropagateCompareAndSwapError proves Admit, Claim,
// Renew, Release, Takeover, Complete, and Restore each return a
// genuine Store.CompareAndSwap failure unchanged. rejectingStore only
// exercises the "losing race" (false, nil) shape that drives the
// retry loop; no test previously drove a real (false, err) result
// through any of these methods.
func TestMutatingMethodsPropagateCompareAndSwapError(t *testing.T) {
	ctx := context.Background()
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	mustAdmit(t, l, ctx, "k3", 1)

	lf, err := ledger.New(casErrorStore{base}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := lf.Admit(ctx, "k2", 1, nil); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Admit: got %v, want errStoreBoom", err)
	}
	if _, err := lf.Claim(ctx, "k3", "owner-b", fixedLease, fixedNow); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Claim: got %v, want errStoreBoom", err)
	}
	if err := lf.Renew(ctx, "k1", "owner-a", fence, fixedLease, fixedNow); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Renew: got %v, want errStoreBoom", err)
	}
	if err := lf.Release(ctx, "k1", "owner-a", fence); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Release: got %v, want errStoreBoom", err)
	}
	stale := fixedNow.Add(fixedLease)
	if _, err := lf.Takeover(ctx, "k1", "owner-b", fixedLease, stale); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Takeover: got %v, want errStoreBoom", err)
	}
	if err := lf.Complete(ctx, "k1", "owner-a", fence, ledger.StatusCompleted); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Complete: got %v, want errStoreBoom", err)
	}
	restoreSnap := ledger.Snapshot{Tasks: []ledger.TaskState{{Key: "new", Status: ledger.StatusPending}}}
	if err := lf.Restore(ctx, restoreSnap); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Restore: got %v, want errStoreBoom", err)
	}
}

// TestRangeErrorPropagatesThroughSnapshotAndBlockDependents proves
// Snapshot and Complete's failure-triggered blockDependents walk each
// return a genuine Store.Range failure unchanged.
// TestStoreMethodsReturnCtxErrWhenCanceled only proved MemStore.Range
// itself returns an error; neither Ledger method that calls Range was
// previously driven with an erroring Store.
func TestRangeErrorPropagatesThroughSnapshotAndBlockDependents(t *testing.T) {
	ctx := context.Background()
	base := ledger.NewMemStore()
	l, err := ledger.New(base, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "root", 1)
	fence := mustClaim(t, l, ctx, "root", "owner-a")

	lf, err := ledger.New(rangeErrorStore{base}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := lf.Snapshot(ctx); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Snapshot: got %v, want errStoreBoom", err)
	}
	if err := lf.Complete(ctx, "root", "owner-a", fence, ledger.StatusFailed); !errors.Is(err, errStoreBoom) {
		t.Fatalf("Complete: got %v, want errStoreBoom", err)
	}
}
