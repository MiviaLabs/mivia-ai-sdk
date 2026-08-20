package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// admitClaimFail drives key through Admit, Claim, then Complete as
// StatusFailed, using a distinct owner per key.
func admitClaimFail(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey) {
	t.Helper()
	mustAdmit(t, l, ctx, key, 1)
	owner := ledger.OwnerID("owner-" + string(key))
	fence := mustClaim(t, l, ctx, key, owner)
	if err := l.Complete(ctx, testActor, key, owner, fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(%s): %v", key, err)
	}
}

// TestEvictedKeyReadmits proves idempotency is a bounded window: an
// evicted key reports found false and Admit accepts it again.
func TestEvictedKeyReadmits(t *testing.T) {
	ctx := context.Background()
	l := ledgerOver(t, cappedStore(t, 2))

	admitClaimComplete(t, l, ctx, "target", 1)
	admitClaimComplete(t, l, ctx, "k1", 1)
	admitClaimComplete(t, l, ctx, "k2", 1)

	st, found, err := l.State(ctx, "target")
	if err != nil {
		t.Fatalf("State(target): %v", err)
	}
	if found {
		t.Fatalf("target = %+v, want found false after eviction", st)
	}

	ok, err := l.Admit(ctx, testActor, "target", 1, "second run", fixedNow)
	if err != nil {
		t.Fatalf("Admit(target): %v", err)
	}
	if !ok {
		t.Fatalf("Admit(target) after eviction = false, want true")
	}
}

// TestEvictedFailureStopsBlocking proves a deleted failed need blocks
// nothing: its later dependent admits pending and claims.
func TestEvictedFailureStopsBlocking(t *testing.T) {
	ctx := context.Background()
	l := ledgerOver(t, cappedStore(t, 2))

	admitClaimFail(t, l, ctx, "dep")
	admitClaimComplete(t, l, ctx, "k1", 1)
	admitClaimComplete(t, l, ctx, "k2", 1)

	if _, found, err := l.State(ctx, "dep"); err != nil || found {
		t.Fatalf("State(dep): found=%v err=%v, want found false", found, err)
	}

	mustAdmit(t, l, ctx, "b", 1, "dep")
	st, found, err := l.State(ctx, "b")
	if err != nil || !found {
		t.Fatalf("State(b): found=%v err=%v", found, err)
	}
	if st.Status != ledger.StatusPending {
		t.Fatalf("b.Status = %q, want StatusPending: a deleted need blocks nothing", st.Status)
	}
	if _, err := l.Claim(ctx, testActor, "b", "owner-b", fixedLease, fixedNow); err != nil {
		t.Fatalf("Claim(b): %v, want a granted claim", err)
	}
}

// TestFenceMonotonicAcrossEviction proves the store-wide fence floor
// keeps a key's fence monotonic across deletion and re-admission.
func TestFenceMonotonicAcrossEviction(t *testing.T) {
	ctx := context.Background()
	l := ledgerOver(t, cappedStore(t, 2))

	mustAdmit(t, l, ctx, "k1", 1)
	first := mustClaim(t, l, ctx, "k1", "owner-a")
	if err := l.Complete(ctx, testActor, "k1", "owner-a", first, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete(k1): %v", err)
	}
	admitClaimComplete(t, l, ctx, "x1", 1)
	admitClaimComplete(t, l, ctx, "x2", 1)
	if _, found, err := l.State(ctx, "k1"); err != nil || found {
		t.Fatalf("State(k1): found=%v err=%v, want found false", found, err)
	}

	mustAdmit(t, l, ctx, "k1", 1)
	err := l.Complete(ctx, testActor, "k1", "owner-a", first, ledger.StatusCompleted, fixedNow)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Complete(k1) while pending: err = %v, want ErrNotClaimed", err)
	}

	second := mustClaim(t, l, ctx, "k1", "owner-b")
	if second <= first {
		t.Fatalf("second fence = %d, want strictly above the pre-eviction fence %d", second, first)
	}
	err = l.Complete(ctx, testActor, "k1", "owner-a", first, ledger.StatusCompleted, fixedNow)
	if !errors.Is(err, ledger.ErrFenced) {
		t.Fatalf("Complete(k1) under the stale fence: err = %v, want ErrFenced", err)
	}
}

// TestClaimAfterEvictedPendingRecord proves a pending record deleted
// between Admit and Claim answers ErrNoKey.
func TestClaimAfterEvictedPendingRecord(t *testing.T) {
	ctx := context.Background()
	l := ledgerOver(t, cappedStore(t, 1))

	mustAdmit(t, l, ctx, "a", 1)
	mustAdmit(t, l, ctx, "b", 1)

	_, err := l.Claim(ctx, testActor, "a", "owner-a", fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Claim(a) after eviction: err = %v, want ErrNoKey", err)
	}
}

// TestCompleteAfterEvictedExpiredClaim proves an owner whose lease
// expired can lose its record while it still works: Complete answers
// ErrNoKey.
func TestCompleteAfterEvictedExpiredClaim(t *testing.T) {
	ctx := context.Background()
	l := ledgerOver(t, cappedStore(t, 1))

	mustAdmit(t, l, ctx, "a", 1)
	// Claim in the past, so the lease is already expired at the
	// store's pinned clock and the record is evictable.
	fence, err := l.Claim(ctx, testActor, "a", "owner-a", fixedLease, fixedNow.Add(-2*fixedLease))
	if err != nil {
		t.Fatalf("Claim(a): %v", err)
	}
	mustAdmit(t, l, ctx, "b", 1)

	err = l.Complete(ctx, testActor, "a", "owner-a", fence, ledger.StatusCompleted, fixedNow)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Complete(a) after eviction: err = %v, want ErrNoKey", err)
	}
	if st, found, err := l.State(ctx, "a"); err != nil || found {
		t.Fatalf("State(a) = %+v found=%v err=%v, want found false", st, found, err)
	}
}
