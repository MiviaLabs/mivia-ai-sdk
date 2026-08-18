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

// rangeTriggerStore wraps a ledger.Store and runs trigger once, right
// after the first Range call returns, before its caller observes the
// result. This reproduces a write racing between blockDependents' Range
// snapshot and its later per-key CompareAndSwap deterministically,
// instead of relying on incidental goroutine timing.
type rangeTriggerStore struct {
	ledger.Store
	mu      sync.Mutex
	trigger func()
}

func (r *rangeTriggerStore) Range(ctx context.Context, fn func(ledger.TaskState) bool) error {
	err := r.Store.Range(ctx, fn)
	r.mu.Lock()
	trigger := r.trigger
	r.trigger = nil
	r.mu.Unlock()
	if trigger != nil {
		trigger()
	}
	return err
}

// TestCompleteFailedBlocksDependentAfterConcurrentClaim proves
// blockDependents retries a losing CompareAndSwap against a dependent
// whose record changed after the Range snapshot but before pass two
// reaches it. A concurrent Claim on the dependent, fired right after
// the Range snapshot, must not leave the dependent silently unblocked.
func TestCompleteFailedBlocksDependentAfterConcurrentClaim(t *testing.T) {
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
		if _, err := l.Claim(ctx, "B", "owner-b", fixedLease, fixedNow); err != nil {
			t.Errorf("concurrent Claim(B): %v", err)
		}
	}

	if err := l.Complete(ctx, "A", "owner-a", fence, ledger.StatusFailed); err != nil {
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
	if st.BlockedBy != "A" {
		t.Fatalf("B.BlockedBy = %q, want A", st.BlockedBy)
	}
}
