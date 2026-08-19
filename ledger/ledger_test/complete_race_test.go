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
		completedErr = l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, fixedNow)
	}()
	go func() {
		defer wg.Done()
		failedErr = l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusFailed, fixedNow)
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

// TestCompleteFailedBlocksDependentAfterConcurrentClaim proves a
// dependent never escapes unblocked when a Claim on it lands between
// blockDependents' Range snapshot and pass two's own write. Two rules
// combine here: Claim refuses the dependent, because the failed key is
// already in its Needs closure, and pass two then skips the record
// Claim's refusal already blocked.
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
		// A already holds StatusFailed when the walk's snapshot
		// returns, so Claim's blockingAncestor check refuses B and
		// blocks it itself. Pass two then finds B already terminal.
		if _, err := l.Claim(ctx, testActor, "B", "owner-b", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
			t.Errorf("concurrent Claim(B) = %v, want ErrNotClaimed", err)
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
	if st.BlockedBy != "A" {
		t.Fatalf("B.BlockedBy = %q, want A", st.BlockedBy)
	}
}

// TestCompleteFailedSkipsDependentCompletedAfterRangeSnapshot proves
// blockOne's terminal-status check catches a dependent that legitimately
// finishes between blockDependents' Range snapshot and blockOne's own
// CompareAndSwap against that dependent. A concurrent Complete on the
// dependent, fired right after the Range snapshot, must keep the
// dependent's finished status instead of losing it to StatusBlocked.
func TestCompleteFailedSkipsDependentCompletedAfterRangeSnapshot(t *testing.T) {
	ctx := context.Background()
	store := &rangeTriggerStore{Store: ledger.NewMemStore()}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustAdmit(t, l, ctx, "A", 1)
	mustAdmit(t, l, ctx, "B", 1, "A")
	fenceA := mustClaim(t, l, ctx, "A", "owner-a")
	fenceB := mustClaim(t, l, ctx, "B", "owner-b")

	store.trigger = func() {
		if err := l.Complete(ctx, testActor, "B", "owner-b", fenceB, ledger.StatusCompleted, fixedNow); err != nil {
			t.Errorf("concurrent Complete(B): %v", err)
		}
	}

	if err := l.Complete(ctx, testActor, "A", "owner-a", fenceA, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	st, found, err := l.State(ctx, "B")
	if err != nil {
		t.Fatalf("State(B): %v", err)
	}
	if !found {
		t.Fatalf("B: want found")
	}
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("B: Status = %q, want unchanged StatusCompleted", st.Status)
	}
	if st.BlockedBy != "" {
		t.Fatalf("B.BlockedBy = %q, want empty", st.BlockedBy)
	}
}
