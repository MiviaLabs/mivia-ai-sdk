package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// movingClock is a mutex-guarded clock a test hands to
// MemStoreOptions.Now. MemStore calls Now under its own lock, and the
// test advances the clock from other goroutines, so the closure needs
// its own guard.
type movingClock struct {
	mu sync.Mutex
	at time.Time
}

// now reads the current clock value.
func (c *movingClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// advance moves the clock forward by d.
func (c *movingClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// TestReclaimUnderConcurrentClaims races claims and abandoned claims
// against a moving clock on a capped MemStore, then drives quiescent
// writes and asserts the cap holds. Run under go test -race.
func TestReclaimUnderConcurrentClaims(t *testing.T) {
	const maxEntries = 4
	ctx := context.Background()
	clock := &movingClock{at: fixedNow}
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{
		MaxEntries: maxEntries,
		Now:        clock.now,
	})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
	l := ledgerOver(t, store)

	const writers = 8
	const advancers = 4
	writeErrs := make([]error, writers)
	var wg sync.WaitGroup

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			writeErrs[n] = runClaimWriter(ctx, l, n)
		}(i)
	}
	for i := 0; i < advancers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clock.advance(2 * fixedLease)
		}()
	}
	wg.Wait()

	for i, err := range writeErrs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	// Reclamation is lazy: it runs inside CompareAndSwap only. A
	// record stranded while the live count was high needs a later
	// write, never the clock alone. So advance past every lease,
	// then drive quiescent writes before asserting the cap.
	clock.advance(2 * fixedLease)
	for i := 0; i < 2*maxEntries; i++ {
		admitClaimComplete(t, l, ctx, ledger.IdempotencyKey(fmt.Sprintf("q%d", i)), 1)
	}

	snap := mustSnapshot(t, l, ctx)
	if len(snap.Tasks) > maxEntries+1 {
		t.Fatalf("snapshot holds %d records, want at most %d", len(snap.Tasks), maxEntries+1)
	}
	for _, rec := range snap.Tasks {
		if err := rec.Validate(); err != nil {
			t.Fatalf("surviving record %+v: %v", rec, err)
		}
	}
}

// runClaimWriter admits and claims one key. An even n completes the
// claim; an odd n abandons it, leaving the residue an aborted request
// leaves behind. ErrNoKey is a documented outcome under a cap, so it
// is not an error here.
func runClaimWriter(ctx context.Context, l *ledger.Ledger, n int) error {
	key := ledger.IdempotencyKey(fmt.Sprintf("w%d", n))
	owner := ledger.OwnerID(fmt.Sprintf("owner-w%d", n))
	if ok, err := l.Admit(ctx, testActor, key, 1, "payload", fixedNow); err != nil || !ok {
		return fmt.Errorf("Admit(%s): ok=%v err=%v", key, ok, err)
	}
	fence, err := l.Claim(ctx, testActor, key, owner, fixedLease, fixedNow)
	switch {
	case errors.Is(err, ledger.ErrNoKey):
		return nil
	case err != nil:
		return fmt.Errorf("Claim(%s): %w", key, err)
	}
	if n%2 == 1 {
		return nil
	}
	err = l.Complete(ctx, testActor, key, owner, fence, ledger.StatusCompleted, fixedNow)
	if errors.Is(err, ledger.ErrNoKey) {
		return nil
	}
	return err
}
