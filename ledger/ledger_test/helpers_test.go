package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// fixedNow and fixedLease give every deterministic test a shared,
// non-wall-clock time base. No test in this package sleeps; every
// clock comparison flows through caller-supplied time.Time values.
var fixedNow = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

const fixedLease = time.Hour

// testActor is the default Actor every shared fixture builder passes,
// so a test opts into asserting a specific Actor only where it cares.
const testActor ledger.Actor = "test-actor"

// cappedStore builds a MemStore capped at maxEntries with its clock
// pinned to fixedNow, so a fixedLease claim reads as live. Every
// capped-store test in this package builds through this helper: a
// store left on time.Now reads every fixedNow lease as long expired,
// which drops live-lease protection with no visible failure.
func cappedStore(t *testing.T, maxEntries int) *ledger.MemStore {
	t.Helper()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{
		MaxEntries: maxEntries,
		Now:        func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions(%d): %v", maxEntries, err)
	}
	return store
}

// mustSnapshot takes a Snapshot and fails the test on any error.
func mustSnapshot(t *testing.T, l *ledger.Ledger, ctx context.Context) ledger.Snapshot {
	t.Helper()
	snap, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snap
}

// mustAdmit admits key at seq and fails the test on any error or a
// rejected admission.
func mustAdmit(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, seq ledger.Sequence, needs ...ledger.IdempotencyKey) {
	t.Helper()
	ok, err := l.Admit(ctx, testActor, key, seq, nil, fixedNow, needs...)
	if err != nil {
		t.Fatalf("Admit(%s): %v", key, err)
	}
	if !ok {
		t.Fatalf("Admit(%s): want true", key)
	}
}

// mustClaim claims key for owner at fixedNow and fails the test on
// any error.
func mustClaim(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey, owner ledger.OwnerID) ledger.FenceToken {
	t.Helper()
	fence, err := l.Claim(ctx, testActor, key, owner, fixedLease, fixedNow)
	if err != nil {
		t.Fatalf("Claim(%s): %v", key, err)
	}
	return fence
}

// buildCompleted admits, claims, and completes key "k1" as
// StatusCompleted.
func buildCompleted(t *testing.T, l *ledger.Ledger, ctx context.Context) {
	t.Helper()
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	if err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

// buildFailed admits, claims, and completes key "k1" as StatusFailed.
func buildFailed(t *testing.T, l *ledger.Ledger, ctx context.Context) {
	t.Helper()
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	if err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

// buildBlocked admits "root" and "k1" (which needs "root"), then
// fails "root" so "k1" ends StatusBlocked.
func buildBlocked(t *testing.T, l *ledger.Ledger, ctx context.Context) {
	t.Helper()
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "k1", 1, "root")
	fence := mustClaim(t, l, ctx, "root", "owner-a")
	if err := l.Complete(ctx, testActor, "root", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}
