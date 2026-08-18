package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestCompleteSucceeded proves Complete with StatusCompleted and the
// current fence marks the record done, and a Renew after completion
// returns ErrNotClaimed.
func TestCompleteSucceeded(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	if err := l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusCompleted); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("Status = %q, want StatusCompleted", st.Status)
	}
	if err := l.Renew(ctx, "k1", "owner-a", fence, fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Renew after completion: got %v, want ErrNotClaimed", err)
	}
}

// TestCompleteFailedBlocksDependent proves Complete with StatusFailed
// on a key another pending record names in Needs marks the dependent
// StatusBlocked, with BlockedBy set to the failed key.
func TestCompleteFailedBlocksDependent(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "dep", 1, "root")
	fence := mustClaim(t, l, ctx, "root", "owner-a")
	if err := l.Complete(ctx, "root", "owner-a", fence, ledger.StatusFailed); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	blockedBy, blocked, err := l.Blocked(ctx, "dep")
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	if !blocked {
		t.Fatalf("dep: want blocked")
	}
	if blockedBy != "root" {
		t.Fatalf("BlockedBy = %q, want root", blockedBy)
	}
}

// TestCompleteFailedBlocksTransitively proves a two-level dependency
// chain: failing the root blocks the direct dependent and the
// dependent's own dependent.
func TestCompleteFailedBlocksTransitively(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "mid", 1, "root")
	mustAdmit(t, l, ctx, "leaf", 1, "mid")
	fence := mustClaim(t, l, ctx, "root", "owner-a")
	if err := l.Complete(ctx, "root", "owner-a", fence, ledger.StatusFailed); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, key := range []ledger.IdempotencyKey{"mid", "leaf"} {
		blockedBy, blocked, err := l.Blocked(ctx, key)
		if err != nil {
			t.Fatalf("Blocked(%s): %v", key, err)
		}
		if !blocked {
			t.Fatalf("%s: want blocked", key)
		}
		if blockedBy != "root" {
			t.Fatalf("%s: BlockedBy = %q, want root", key, blockedBy)
		}
	}
}

// TestCompleteFailedTwoCycleTerminates proves a two-node cycle
// (A.Needs contains B, B.Needs contains A) terminates and both end up
// StatusBlocked exactly once.
func TestCompleteFailedTwoCycleTerminates(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "A", 1, "B")
	mustAdmit(t, l, ctx, "B", 1, "A")
	fence := mustClaim(t, l, ctx, "A", "owner-a")
	if err := l.Complete(ctx, "A", "owner-a", fence, ledger.StatusFailed); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, key := range []ledger.IdempotencyKey{"A", "B"} {
		st, found, err := l.State(ctx, key)
		if err != nil {
			t.Fatalf("State(%s): %v", key, err)
		}
		if !found {
			t.Fatalf("%s: want found", key)
		}
		if st.Status != ledger.StatusBlocked {
			t.Fatalf("%s: Status = %q, want StatusBlocked", key, st.Status)
		}
	}
}

// TestCompleteFailedThreeHopCycleTerminates proves a three-hop cycle
// (A->B->C->A) terminates and every node ends up StatusBlocked
// exactly once.
func TestCompleteFailedThreeHopCycleTerminates(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "A", 1, "B")
	mustAdmit(t, l, ctx, "B", 1, "C")
	mustAdmit(t, l, ctx, "C", 1, "A")
	fence := mustClaim(t, l, ctx, "A", "owner-a")
	if err := l.Complete(ctx, "A", "owner-a", fence, ledger.StatusFailed); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	for _, key := range []ledger.IdempotencyKey{"A", "B", "C"} {
		st, _, err := l.State(ctx, key)
		if err != nil {
			t.Fatalf("State(%s): %v", key, err)
		}
		if st.Status != ledger.StatusBlocked {
			t.Fatalf("%s: Status = %q, want StatusBlocked", key, st.Status)
		}
	}
}

// TestCompleteFailedSharedDependentBlockedOnce proves two independent
// records, X and Y, both naming D in Needs: completing X then Y as
// failed blocks D exactly once, and the second call leaves D's
// BlockedBy unchanged.
func TestCompleteFailedSharedDependentBlockedOnce(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "X", 1)
	mustAdmit(t, l, ctx, "Y", 1)
	mustAdmit(t, l, ctx, "D", 1, "X", "Y")

	fenceX := mustClaim(t, l, ctx, "X", "owner-a")
	if err := l.Complete(ctx, "X", "owner-a", fenceX, ledger.StatusFailed); err != nil {
		t.Fatalf("Complete X: %v", err)
	}
	blockedBy, blocked, err := l.Blocked(ctx, "D")
	if err != nil || !blocked {
		t.Fatalf("D after X failed: blocked=%v err=%v", blocked, err)
	}
	if blockedBy != "X" {
		t.Fatalf("D.BlockedBy = %q, want X", blockedBy)
	}

	fenceY := mustClaim(t, l, ctx, "Y", "owner-b")
	if err := l.Complete(ctx, "Y", "owner-b", fenceY, ledger.StatusFailed); err != nil {
		t.Fatalf("Complete Y: %v", err)
	}
	blockedBy, blocked, err = l.Blocked(ctx, "D")
	if err != nil || !blocked {
		t.Fatalf("D after Y failed: blocked=%v err=%v", blocked, err)
	}
	if blockedBy != "X" {
		t.Fatalf("D.BlockedBy changed to %q, want unchanged X", blockedBy)
	}
}

// TestCompleteStaleFenceRejected proves Complete with a stale fence
// returns ErrFenced and blocks no dependent.
func TestCompleteStaleFenceRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "dep", 1, "root")
	fence := mustClaim(t, l, ctx, "root", "owner-a")
	err := l.Complete(ctx, "root", "owner-a", fence+1, ledger.StatusFailed)
	if !errors.Is(err, ledger.ErrFenced) {
		t.Fatalf("Complete: got %v, want ErrFenced", err)
	}
	_, blocked, _ := l.Blocked(ctx, "dep")
	if blocked {
		t.Fatalf("dep: want not blocked after a rejected Complete")
	}
}

// TestCompleteAgainstPendingRejected proves Complete against a
// StatusPending record returns ErrNotClaimed.
func TestCompleteAgainstPendingRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	err := l.Complete(ctx, "k1", "owner-a", 0, ledger.StatusCompleted)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Complete: got %v, want ErrNotClaimed", err)
	}
}

// TestCompleteAgainstTerminalRejected proves a second Complete call
// against a record already moved to StatusCompleted, with the same
// owner and fence the first call used, returns ErrNotClaimed and
// leaves the record at StatusCompleted.
func TestCompleteAgainstTerminalRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	if err := l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusCompleted); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	err := l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusFailed)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("second Complete: got %v, want ErrNotClaimed", err)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("Status = %q, want unchanged StatusCompleted", st.Status)
	}
}

// TestCompleteAgainstUnknownKeyRejected proves Complete against a
// never-admitted key returns ErrNoKey, not ErrNotClaimed.
func TestCompleteAgainstUnknownKeyRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	err := l.Complete(ctx, "ghost", "owner-a", 0, ledger.StatusCompleted)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Complete: got %v, want ErrNoKey", err)
	}
}

// TestCompleteUnknownStatusAgainstClaimedRejected proves Complete
// called with StatusPending (out of range for Complete) against an
// already-claimed record returns ErrUnknownStatus and leaves the
// stored record unchanged.
func TestCompleteUnknownStatusAgainstClaimedRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")
	before, _, _ := l.State(ctx, "k1")
	err := l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusPending)
	if !errors.Is(err, ledger.ErrUnknownStatus) {
		t.Fatalf("Complete: got %v, want ErrUnknownStatus", err)
	}
	after, _, _ := l.State(ctx, "k1")
	if after.Status != before.Status || after.Fence != before.Fence {
		t.Fatalf("record changed: before %+v, after %+v", before, after)
	}
}

// TestCompleteUnknownStatusAgainstUnknownKeyRejected proves Complete
// called with the zero-value machine.Status against a never-admitted
// key returns ErrUnknownStatus, not ErrNoKey: the status check runs
// first, before Store.Load.
func TestCompleteUnknownStatusAgainstUnknownKeyRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	err := l.Complete(ctx, "ghost", "owner-a", 0, machine.Status(""))
	if !errors.Is(err, ledger.ErrUnknownStatus) {
		t.Fatalf("Complete: got %v, want ErrUnknownStatus", err)
	}
}

// TestBlockedOnUnblockedKeyReturnsFalse proves Blocked on an
// unblocked key returns false.
func TestBlockedOnUnblockedKeyReturnsFalse(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	_, blocked, err := l.Blocked(ctx, "k1")
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	if blocked {
		t.Fatalf("k1: want not blocked")
	}
}

// TestBlockedOnUnknownKeyReturnsFalse proves Blocked on a
// never-admitted key returns false, nil, the same result as an
// admitted, unblocked key.
func TestBlockedOnUnknownKeyReturnsFalse(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	_, blocked, err := l.Blocked(ctx, "ghost")
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	if blocked {
		t.Fatalf("ghost: want not blocked")
	}
}
