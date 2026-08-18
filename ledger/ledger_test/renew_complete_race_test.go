package ledger_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestRenewCompleteRaceLeavesOneConsistentState admits and claims a
// key, then races Renew against Complete on it. The barrier store
// forces both goroutines to read the identical starting record, so
// the collision is genuine rather than incidental.
//
// Exactly one of two orderings wins. Either Renew lands first and
// Complete then succeeds against the post-renew record, or Complete
// lands first and Renew loses. A lost Renew reports ErrNotClaimed:
// the task is no longer claimed once it completes. Either way the key
// ends StatusCompleted and CompletedEvent fires exactly once.
//
// Run under go test -race. renew_race_test.go covers Renew against
// Renew; complete_race_test.go covers Complete against Complete. This
// file closes the remaining Renew-against-Complete pair.
func TestRenewCompleteRaceLeavesOneConsistentState(t *testing.T) {
	ctx := context.Background()
	store := newBarrierStore(ledger.NewMemStore())
	bus := events.New()
	var completed int64
	if err := bus.Subscribe(ledger.CompletedEvent, func(_ context.Context, _ events.Event) error {
		atomic.AddInt64(&completed, 1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Subscribe(ledger.RenewedEvent, func(_ context.Context, _ events.Event) error {
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l, err := ledger.New(store, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")

	store.arm()

	var wg sync.WaitGroup
	var renewErr, completeErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		renewErr = l.Renew(ctx, testActor, "k1", "owner-a", fence, fixedLease, fixedNow)
	}()
	go func() {
		defer wg.Done()
		completeErr = l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, fixedNow)
	}()
	wg.Wait()

	// Complete must win outright. It is the terminal write; nothing in
	// this race can push the task back out of a completed state.
	if completeErr != nil {
		t.Fatalf("Complete: %v", completeErr)
	}
	// Renew either landed before Complete, or lost to it. A lost Renew
	// reports ErrNotClaimed. No other outcome is consistent.
	if renewErr != nil && !errors.Is(renewErr, ledger.ErrNotClaimed) {
		t.Fatalf("Renew error = %v, want nil or errors.Is match for ledger.ErrNotClaimed", renewErr)
	}

	state, ok, err := l.State(ctx, "k1")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !ok {
		t.Fatal("State reported no such key after the race")
	}
	if state.Status != ledger.StatusCompleted {
		t.Fatalf("final status = %q, want %q", state.Status, ledger.StatusCompleted)
	}
	if got := atomic.LoadInt64(&completed); got != 1 {
		t.Fatalf("CompletedEvent fired %d times, want exactly 1", got)
	}
}
