package ledger_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// loadHoldStore wraps a ledger.Store and holds one targeted Load call
// open on a channel, after the wrapped call already returned its
// result. This pins a fixed read order across two goroutines: the
// held caller observes a snapshot taken before a later, unrelated
// write, without relying on incidental goroutine timing. Only the
// first Load call whose key matches holdKey, once armed, holds; every
// other call, including a later Load of the same key, passes through.
type loadHoldStore struct {
	ledger.Store
	holdKey ledger.IdempotencyKey
	release chan struct{}
	entered chan struct{}
	armed   atomic.Bool
	held    atomic.Bool
}

// arm enables the hold for the next matching Load call.
func (h *loadHoldStore) arm() { h.armed.Store(true) }

// Load passes every call through to the wrapped Store first, then
// holds the first post-arm call matching holdKey until release closes.
func (h *loadHoldStore) Load(ctx context.Context, key ledger.IdempotencyKey) (ledger.TaskState, bool, error) {
	st, found, err := h.Store.Load(ctx, key)
	if h.armed.Load() && key == h.holdKey && h.held.CompareAndSwap(false, true) {
		close(h.entered)
		<-h.release
	}
	return st, found, err
}

// TestAdmitCompleteRaceDependentStillBlocks proves the post-insert
// need recheck closes the race between Admit's blockingNeed read and
// Complete's blockDependents Range snapshot. Dependent B's Admit call
// reads need A's record before A's Complete(StatusFailed) call, and
// B's own admitting write lands only after Complete's Range snapshot
// already returned without B in it. B must still end StatusBlocked,
// naming A, and never claim. Run under go test -race.
func TestAdmitCompleteRaceDependentStillBlocks(t *testing.T) {
	ctx := context.Background()
	store := &loadHoldStore{
		Store:   ledger.NewMemStore(),
		holdKey: "A",
		release: make(chan struct{}),
		entered: make(chan struct{}),
	}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "A", 1)
	fenceA := mustClaim(t, l, ctx, "A", "owner-a")

	store.arm()
	var admitOK bool
	var admitErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		admitOK, admitErr = l.Admit(ctx, testActor, "B", 1, nil, fixedNow, "A")
	}()

	// Wait for B's Admit call to read A's pre-failure record and hold
	// there, before A fails.
	<-store.entered

	if err := l.Complete(ctx, testActor, "A", "owner-a", fenceA, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete(A): %v", err)
	}

	// Release B's held read only after A's Complete call, and its
	// blockDependents Range snapshot, has fully returned.
	close(store.release)
	<-done

	if admitErr != nil {
		t.Fatalf("Admit(B): %v", admitErr)
	}
	if !admitOK {
		t.Fatalf("Admit(B) = false, want true")
	}

	st, found, err := l.State(ctx, "B")
	if err != nil {
		t.Fatalf("State(B): %v", err)
	}
	if !found {
		t.Fatalf("B: want found")
	}
	if st.Status != ledger.StatusBlocked {
		t.Fatalf("B.Status = %q, want %q: the dependent escaped the failure", st.Status, ledger.StatusBlocked)
	}
	if st.BlockedBy != "A" {
		t.Fatalf("B.BlockedBy = %q, want A", st.BlockedBy)
	}
	if _, err := l.Claim(ctx, testActor, "B", "owner-b", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Claim on blocked B = %v, want ErrNotClaimed", err)
	}
}
