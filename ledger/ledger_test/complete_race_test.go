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

// TestCompleteRaceExactlyOneWinner admits and claims a key, then
// races two goroutines both calling Complete with the same owner and
// fence: one with StatusCompleted, the other with StatusFailed.
// Exactly one call returns nil; the other returns ErrNotClaimed. The
// final stored Status matches whichever call won, never the loser's
// requested status. CompletedEvent fires exactly once. Run under
// go test -race.
func TestCompleteRaceExactlyOneWinner(t *testing.T) {
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
	l, err := ledger.New(store, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")

	store.arm()
	var wg sync.WaitGroup
	var completedErr, failedErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		completedErr = l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusCompleted)
	}()
	go func() {
		defer wg.Done()
		failedErr = l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusFailed)
	}()
	wg.Wait()

	completedWon := completedErr == nil
	failedWon := failedErr == nil
	if completedWon == failedWon {
		t.Fatalf("want exactly one winner: completedErr=%v failedErr=%v", completedErr, failedErr)
	}
	if !completedWon && !errors.Is(completedErr, ledger.ErrNotClaimed) {
		t.Fatalf("losing StatusCompleted call error = %v, want ErrNotClaimed", completedErr)
	}
	if !failedWon && !errors.Is(failedErr, ledger.ErrNotClaimed) {
		t.Fatalf("losing StatusFailed call error = %v, want ErrNotClaimed", failedErr)
	}

	st, _, err := l.State(ctx, "k1")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	wantStatus := ledger.StatusFailed
	if completedWon {
		wantStatus = ledger.StatusCompleted
	}
	if st.Status != wantStatus {
		t.Fatalf("final Status = %q, want %q", st.Status, wantStatus)
	}
	if atomic.LoadInt64(&completed) != 1 {
		t.Fatalf("CompletedEvent fired %d times, want 1", completed)
	}
}
