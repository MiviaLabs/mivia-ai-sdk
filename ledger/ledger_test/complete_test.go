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
	completeActor := ledger.Actor("complete-actor")
	completeNow := fixedNow.Add(fixedLease)
	if err := l.Complete(ctx, completeActor, "k1", "owner-a", fence, ledger.StatusCompleted, completeNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("Status = %q, want StatusCompleted", st.Status)
	}
	if st.UpdatedBy != completeActor || !st.UpdatedAt.Equal(completeNow) {
		t.Fatalf("UpdatedBy/UpdatedAt = %q/%v, want %q/%v", st.UpdatedBy, st.UpdatedAt, completeActor, completeNow)
	}
	if st.CreatedBy != testActor || !st.CreatedAt.Equal(fixedNow) {
		t.Fatalf("CreatedBy/CreatedAt = %q/%v, want unchanged %q/%v", st.CreatedBy, st.CreatedAt, testActor, fixedNow)
	}
	if err := l.Renew(ctx, testActor, "k1", "owner-a", fence, fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
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
	blockActor := ledger.Actor("block-actor")
	blockNow := fixedNow.Add(fixedLease)
	if err := l.Complete(ctx, blockActor, "root", "owner-a", fence, ledger.StatusFailed, blockNow); err != nil {
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
	depState, _, err := l.State(ctx, "dep")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if depState.UpdatedBy != blockActor || !depState.UpdatedAt.Equal(blockNow) {
		t.Fatalf("dep UpdatedBy/UpdatedAt = %q/%v, want %q/%v", depState.UpdatedBy, depState.UpdatedAt, blockActor, blockNow)
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
	if err := l.Complete(ctx, testActor, "root", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
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
// (A.Needs contains B, B.Needs contains A) terminates. A keeps its
// own genuine StatusFailed, because the blocking walk's terminal-
// status check protects a record already terminal, even when the
// cycle routes the walk back to A. B, the only non-terminal member of
// the cycle, ends up StatusBlocked.
func TestCompleteFailedTwoCycleTerminates(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "A", 1, "B")
	mustAdmit(t, l, ctx, "B", 1, "A")
	fence := mustClaim(t, l, ctx, "A", "owner-a")
	if err := l.Complete(ctx, testActor, "A", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := map[ledger.IdempotencyKey]machine.Status{
		"A": ledger.StatusFailed,
		"B": ledger.StatusBlocked,
	}
	for key, wantStatus := range want {
		st, found, err := l.State(ctx, key)
		if err != nil {
			t.Fatalf("State(%s): %v", key, err)
		}
		if !found {
			t.Fatalf("%s: want found", key)
		}
		if st.Status != wantStatus {
			t.Fatalf("%s: Status = %q, want %q", key, st.Status, wantStatus)
		}
	}
}

// TestCompleteFailedThreeHopCycleTerminates proves a three-hop cycle
// (A->B->C->A) terminates. A keeps its own genuine StatusFailed for
// the same reason as the two-node cycle; B and C, the non-terminal
// members, end up StatusBlocked.
func TestCompleteFailedThreeHopCycleTerminates(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "A", 1, "B")
	mustAdmit(t, l, ctx, "B", 1, "C")
	mustAdmit(t, l, ctx, "C", 1, "A")
	fence := mustClaim(t, l, ctx, "A", "owner-a")
	if err := l.Complete(ctx, testActor, "A", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := map[ledger.IdempotencyKey]machine.Status{
		"A": ledger.StatusFailed,
		"B": ledger.StatusBlocked,
		"C": ledger.StatusBlocked,
	}
	for key, wantStatus := range want {
		st, _, err := l.State(ctx, key)
		if err != nil {
			t.Fatalf("State(%s): %v", key, err)
		}
		if st.Status != wantStatus {
			t.Fatalf("%s: Status = %q, want %q", key, st.Status, wantStatus)
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
	if err := l.Complete(ctx, testActor, "X", "owner-a", fenceX, ledger.StatusFailed, fixedNow); err != nil {
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
	if err := l.Complete(ctx, testActor, "Y", "owner-b", fenceY, ledger.StatusFailed, fixedNow); err != nil {
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
	err := l.Complete(ctx, testActor, "root", "owner-a", fence+1, ledger.StatusFailed, fixedNow)
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
	err := l.Complete(ctx, testActor, "k1", "owner-a", 0, ledger.StatusCompleted, fixedNow)
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
	if err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusFailed, fixedNow)
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
	err := l.Complete(ctx, testActor, "ghost", "owner-a", 0, ledger.StatusCompleted, fixedNow)
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
	err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusPending, fixedNow)
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
	err := l.Complete(ctx, testActor, "ghost", "owner-a", 0, machine.Status(""), fixedNow)
	if !errors.Is(err, ledger.ErrUnknownStatus) {
		t.Fatalf("Complete: got %v, want ErrUnknownStatus", err)
	}
}

// TestBlockDependentsSkipsDependentAlreadyBlockedDuringRetry proves
// blockOne's retry path skips a dependent that a concurrent write
// already moved to StatusBlocked between the losing CompareAndSwap
// and the reload, leaving that dependent's original BlockedBy
// unchanged instead of overwriting it. This drives blockOne's
// "!found || fresh.Status == StatusBlocked" reload branch directly,
// which the concurrent-Claim regression case in complete_race_test.go
// does not reach (that case retries into a still-unblocked record).
func TestBlockDependentsSkipsDependentAlreadyBlockedDuringRetry(t *testing.T) {
	ctx := context.Background()
	store := &rangeTriggerStore{Store: ledger.NewMemStore()}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "A", 1)
	mustAdmit(t, l, ctx, "B", 1, "A")
	fence := mustClaim(t, l, ctx, "A", "owner-a")

	store.trigger = func() {
		cur, found, err := store.Store.Load(ctx, "B")
		if err != nil || !found {
			t.Fatalf("Load(B): found=%v err=%v", found, err)
		}
		next := cur
		next.Status = ledger.StatusBlocked
		next.BlockedBy = "other"
		if ok, err := store.Store.CompareAndSwap(ctx, "B", cur, next); err != nil || !ok {
			t.Fatalf("CompareAndSwap(B): ok=%v err=%v", ok, err)
		}
	}

	if err := l.Complete(ctx, testActor, "A", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	st, found, err := l.State(ctx, "B")
	if err != nil {
		t.Fatalf("State(B): %v", err)
	}
	if !found {
		t.Fatalf("B: want found")
	}
	if st.Status != ledger.StatusBlocked {
		t.Fatalf("B: Status = %q, want StatusBlocked", st.Status)
	}
	if st.BlockedBy != "other" {
		t.Fatalf("B.BlockedBy = %q, want unchanged %q", st.BlockedBy, "other")
	}
}

// TestCompleteFailedSkipsDependentAlreadyCompleted proves a dependent
// that legitimately reaches StatusCompleted, independent of the
// failing key, before the failing key's blocking walk keeps its
// StatusCompleted status. blockOne must not overwrite a terminal
// outcome with StatusBlocked.
func TestCompleteFailedSkipsDependentAlreadyCompleted(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "D", 1, "root")

	fenceD := mustClaim(t, l, ctx, "D", "owner-d")
	if err := l.Complete(ctx, testActor, "D", "owner-d", fenceD, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete D: %v", err)
	}

	fenceRoot := mustClaim(t, l, ctx, "root", "owner-a")
	if err := l.Complete(ctx, testActor, "root", "owner-a", fenceRoot, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete root: %v", err)
	}

	st, found, err := l.State(ctx, "D")
	if err != nil {
		t.Fatalf("State(D): %v", err)
	}
	if !found {
		t.Fatalf("D: want found")
	}
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("D: Status = %q, want unchanged StatusCompleted", st.Status)
	}
	if st.BlockedBy != "" {
		t.Fatalf("D.BlockedBy = %q, want empty", st.BlockedBy)
	}
}

// TestCompleteFailedSkipsDependentAlreadyFailed proves a dependent
// that legitimately reaches StatusFailed, independent of the failing
// key, before the failing key's blocking walk keeps its StatusFailed
// status.
func TestCompleteFailedSkipsDependentAlreadyFailed(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "root", 1)
	mustAdmit(t, l, ctx, "D", 1, "root")

	fenceD := mustClaim(t, l, ctx, "D", "owner-d")
	if err := l.Complete(ctx, testActor, "D", "owner-d", fenceD, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete D: %v", err)
	}

	fenceRoot := mustClaim(t, l, ctx, "root", "owner-a")
	if err := l.Complete(ctx, testActor, "root", "owner-a", fenceRoot, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete root: %v", err)
	}

	st, found, err := l.State(ctx, "D")
	if err != nil {
		t.Fatalf("State(D): %v", err)
	}
	if !found {
		t.Fatalf("D: want found")
	}
	if st.Status != ledger.StatusFailed {
		t.Fatalf("D: Status = %q, want unchanged StatusFailed", st.Status)
	}
	if st.BlockedBy != "" {
		t.Fatalf("D.BlockedBy = %q, want empty", st.BlockedBy)
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
