package ledger_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// assertMutatorsReturn asserts that Renew, Release, and Complete for
// owner under fence all return want. It is the shared sentinel probe
// for a dispossessed owner.
func assertMutatorsReturn(t *testing.T, ctx context.Context, l *ledger.Ledger, key ledger.IdempotencyKey, owner ledger.OwnerID, fence ledger.FenceToken, want error) {
	t.Helper()
	if err := l.Renew(ctx, testActor, key, owner, fence, fixedLease, fixedNow); !errors.Is(err, want) {
		t.Fatalf("Renew(%s, fence %d) = %v, want %v", owner, fence, err, want)
	}
	if err := l.Release(ctx, testActor, key, owner, fence, fixedNow); !errors.Is(err, want) {
		t.Fatalf("Release(%s, fence %d) = %v, want %v", owner, fence, err, want)
	}
	err := l.Complete(ctx, testActor, key, owner, fence, ledger.StatusCompleted, fixedNow)
	if !errors.Is(err, want) {
		t.Fatalf("Complete(%s, fence %d) = %v, want %v", owner, fence, err, want)
	}
}

// mustState reads key and fails the test when the key has no record.
func mustState(t *testing.T, ctx context.Context, l *ledger.Ledger, key ledger.IdempotencyKey) ledger.TaskState {
	t.Helper()
	st, found, err := l.State(ctx, key)
	if err != nil {
		t.Fatalf("State(%s): %v", key, err)
	}
	if !found {
		t.Fatalf("State(%s): want found", key)
	}
	return st
}

// assertRebasedRecord asserts the record after a pending rebase: the
// status is StatusPending, Owner and LeaseUntil are cleared, and Fence
// still equals the dispossessed owner's token.
func assertRebasedRecord(t *testing.T, st ledger.TaskState, fence ledger.FenceToken) {
	t.Helper()
	if st.Status != ledger.StatusPending {
		t.Fatalf("Status after rebase = %q, want StatusPending", st.Status)
	}
	if st.Owner != "" {
		t.Fatalf("Owner after rebase = %q, want empty", st.Owner)
	}
	if !st.LeaseUntil.IsZero() {
		t.Fatalf("LeaseUntil after rebase = %v, want zero", st.LeaseUntil)
	}
	if st.Fence != fence {
		t.Fatalf("Fence after rebase = %d, want carried %d", st.Fence, fence)
	}
}

// assertFenceMonotonicAcrossAdmit pins the fence carry-forward
// contract for one Store implementation. A rebase must not rewind the
// fence, so the next Claim hands out a token strictly above the
// dispossessed owner's token. key must be unused.
func assertFenceMonotonicAcrossAdmit(t *testing.T, ctx context.Context, l *ledger.Ledger, key ledger.IdempotencyKey) {
	t.Helper()
	mustAdmit(t, l, ctx, key, 1)
	fenceA := mustClaim(t, l, ctx, key, "owner-a")

	mustAdmit(t, l, ctx, key, 2)
	rebased := mustState(t, ctx, l, key)
	assertRebasedRecord(t, rebased, fenceA)

	assertMutatorsReturn(t, ctx, l, key, "owner-a", fenceA, ledger.ErrNotClaimed)
	if after := mustState(t, ctx, l, key); !reflect.DeepEqual(after, rebased) {
		t.Fatalf("record changed after refused calls:\n got %+v\nwant %+v", after, rebased)
	}

	fenceB := mustClaim(t, l, ctx, key, "owner-b")
	if fenceB <= fenceA {
		t.Fatalf("second fence = %d, want strictly above %d", fenceB, fenceA)
	}
	assertMutatorsReturn(t, ctx, l, key, "owner-a", fenceA, ledger.ErrFenced)

	if err := l.Complete(ctx, testActor, key, "owner-b", fenceB, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete(owner-b): %v", err)
	}
	if st := mustState(t, ctx, l, key); st.Status != ledger.StatusCompleted {
		t.Fatalf("Status = %q, want StatusCompleted", st.Status)
	}
}

// TestFenceMonotonicAcrossAdmitMemStore proves a rebase over a claimed
// record never lets two owners mutate one MemStore record.
func TestFenceMonotonicAcrossAdmitMemStore(t *testing.T) {
	assertFenceMonotonicAcrossAdmit(t, context.Background(), newLedger(t, nil), "k1")
}

// TestAdmitBlockedRebaseCarriesFence proves the blocked rebase path
// carries the fence forward too, and clears Owner and LeaseUntil.
func TestAdmitBlockedRebaseCarriesFence(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)

	mustAdmit(t, l, ctx, "k1", 1)
	fenceA := mustClaim(t, l, ctx, "k1", "owner-a")

	mustAdmit(t, l, ctx, "dep", 1)
	depFence := mustClaim(t, l, ctx, "dep", "owner-dep")
	if err := l.Complete(ctx, testActor, "dep", "owner-dep", depFence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(dep): %v", err)
	}

	mustAdmit(t, l, ctx, "k1", 2, "dep")
	st := mustState(t, ctx, l, "k1")
	if st.Status != ledger.StatusBlocked {
		t.Fatalf("Status = %q, want StatusBlocked", st.Status)
	}
	if st.BlockedBy != "dep" {
		t.Fatalf("BlockedBy = %q, want %q", st.BlockedBy, "dep")
	}
	if st.Fence != fenceA {
		t.Fatalf("Fence = %d, want carried %d", st.Fence, fenceA)
	}
	if st.Owner != "" {
		t.Fatalf("Owner = %q, want empty", st.Owner)
	}
	if !st.LeaseUntil.IsZero() {
		t.Fatalf("LeaseUntil = %v, want zero", st.LeaseUntil)
	}
}
