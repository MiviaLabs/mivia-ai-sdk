package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestClaimOnPendingSucceeds proves Claim on a pending record
// succeeds and returns a nonzero FenceToken.
func TestClaimOnPendingSucceeds(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence, err := l.Claim(ctx, "k1", "owner-a", fixedLease, fixedNow)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if fence == 0 {
		t.Fatalf("Claim: fence must be nonzero")
	}
}

// TestClaimWhileLeaseLiveRejected proves a second Claim while the
// first lease is live returns ErrLeaseActive and leaves the fence
// unchanged.
func TestClaimWhileLeaseLiveRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	first := mustClaim(t, l, ctx, "k1", "owner-a")

	_, err := l.Claim(ctx, "k1", "owner-b", fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrLeaseActive) {
		t.Fatalf("Claim: got %v, want ErrLeaseActive", err)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Fence != first {
		t.Fatalf("Fence = %d, want unchanged %d", st.Fence, first)
	}
}

// TestClaimAfterExpiryByDifferentOwner proves Claim after the lease
// expires, called by a different owner, succeeds and bumps the fence.
func TestClaimAfterExpiryByDifferentOwner(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	first, err := l.Claim(ctx, "k1", "owner-a", time.Minute, fixedNow)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	later := fixedNow.Add(2 * time.Minute)
	second, err := l.Claim(ctx, "k1", "owner-b", fixedLease, later)
	if err != nil {
		t.Fatalf("Claim after expiry: %v", err)
	}
	if second <= first {
		t.Fatalf("fence %d did not bump past %d", second, first)
	}
}

// TestRenewExtendsLease proves Renew with the current fence extends
// LeaseUntil.
func TestRenewExtendsLease(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	later := fixedNow.Add(time.Hour)
	if err := l.Renew(ctx, "k1", "owner-a", fence, fixedLease, later); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	st, _, _ := l.State(ctx, "k1")
	want := later.Add(fixedLease)
	if !st.LeaseUntil.Equal(want) {
		t.Fatalf("LeaseUntil = %v, want %v", st.LeaseUntil, want)
	}
}

// TestRenewStaleFenceRejected proves Renew with a stale fence returns
// ErrFenced.
func TestRenewStaleFenceRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	err := l.Renew(ctx, "k1", "owner-a", fence+1, fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrFenced) {
		t.Fatalf("Renew: got %v, want ErrFenced", err)
	}
}

// TestRenewAgainstPendingRejected proves Renew against a
// never-claimed record returns ErrNotClaimed.
func TestRenewAgainstPendingRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	err := l.Renew(ctx, "k1", "owner-a", 0, fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Renew: got %v, want ErrNotClaimed", err)
	}
}

// TestReleaseReturnsRecordToPending proves Release with the current
// fence returns the record to StatusPending, and a subsequent Claim
// by any owner succeeds.
func TestReleaseReturnsRecordToPending(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	if err := l.Release(ctx, "k1", "owner-a", fence); err != nil {
		t.Fatalf("Release: %v", err)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Status != ledger.StatusPending {
		t.Fatalf("Status = %q, want StatusPending", st.Status)
	}
	if _, err := l.Claim(ctx, "k1", "owner-b", fixedLease, fixedNow); err != nil {
		t.Fatalf("Claim after Release: %v", err)
	}
}

// TestReleaseStaleFenceRejected proves Release with a stale fence
// returns ErrFenced.
func TestReleaseStaleFenceRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	err := l.Release(ctx, "k1", "owner-a", fence+1)
	if !errors.Is(err, ledger.ErrFenced) {
		t.Fatalf("Release: got %v, want ErrFenced", err)
	}
}

// TestReleaseAgainstPendingRejected proves Release against a
// never-claimed record returns ErrNotClaimed.
func TestReleaseAgainstPendingRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	err := l.Release(ctx, "k1", "owner-a", 0)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Release: got %v, want ErrNotClaimed", err)
	}
}

// TestClaimAgainstUnknownKeyRejected proves Claim against a
// never-admitted key returns ErrNoKey and creates no record, proving
// Claim never uses the insert-if-absent CompareAndSwap convention.
func TestClaimAgainstUnknownKeyRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	_, err := l.Claim(ctx, "ghost", "owner-a", fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Claim: got %v, want ErrNoKey", err)
	}
	_, found, _ := l.State(ctx, "ghost")
	if found {
		t.Fatalf("Claim against unknown key must not create a record")
	}
}

// TestRenewAgainstUnknownKeyRejected proves Renew against a
// never-admitted key returns ErrNoKey, not ErrNotClaimed.
func TestRenewAgainstUnknownKeyRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	err := l.Renew(ctx, "ghost", "owner-a", 0, fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Renew: got %v, want ErrNoKey", err)
	}
}

// TestReleaseAgainstUnknownKeyRejected proves Release against a
// never-admitted key returns ErrNoKey, not ErrNotClaimed.
func TestReleaseAgainstUnknownKeyRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	err := l.Release(ctx, "ghost", "owner-a", 0)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Release: got %v, want ErrNoKey", err)
	}
}

// TestClaimAgainstTerminalRejected proves Claim against a terminal or
// blocked record returns ErrNotClaimed: Claim only ever admits a
// StatusPending record or a stale StatusClaimed one.
func TestClaimAgainstTerminalRejected(t *testing.T) {
	statuses := []struct {
		name  string
		build func(t *testing.T, l *ledger.Ledger, ctx context.Context)
	}{
		{"completed", buildCompleted},
		{"failed", buildFailed},
		{"blocked", buildBlocked},
	}
	for _, sc := range statuses {
		t.Run(sc.name, func(t *testing.T) {
			ctx := context.Background()
			l := newLedger(t, nil)
			sc.build(t, l, ctx)
			later := fixedNow.Add(2 * fixedLease)
			_, err := l.Claim(ctx, "k1", "owner-b", fixedLease, later)
			if !errors.Is(err, ledger.ErrNotClaimed) {
				t.Fatalf("Claim: got %v, want ErrNotClaimed", err)
			}
		})
	}
}
