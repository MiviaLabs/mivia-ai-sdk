package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestMemStoreEvictionUnderConcurrentAdmission drives a capped
// MemStore with concurrent admit-claim-complete writers while other
// goroutines re-admit an evictable key, asserting eviction stays race
// free and the cap holds once the writers quiesce. A re-admission may
// legitimately succeed, because eviction deletes the key. Run under
// go test -race.
func TestMemStoreEvictionUnderConcurrentAdmission(t *testing.T) {
	ctx := context.Background()
	store := cappedStore(t, 4)
	l := ledgerOver(t, store)

	fence := mustClaim(t, l, ctx, mustAdmitSeed(t, l, ctx), "owner-seed")
	if err := l.Complete(ctx, testActor, "seed", "owner-seed", fence, ledger.StatusCompleted, fixedNow); err != nil {
		t.Fatalf("Complete(seed): %v", err)
	}

	const writers = 8
	const reAdmitters = 4
	const reAdmitRounds = 50
	writeErrs := make([]error, writers)
	reAdmitErrs := make([]error, reAdmitters)
	var wg sync.WaitGroup

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			writeErrs[n] = runEvictionWriter(ctx, l, n)
		}(i)
	}
	for i := 0; i < reAdmitters; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < reAdmitRounds; j++ {
				// A re-admit may return true: eviction can
				// delete "seed" between two rounds.
				if _, err := l.Admit(ctx, testActor, "seed", 99, "re-admitted", fixedNow); err != nil {
					reAdmitErrs[n] = fmt.Errorf("re-admit round %d: %w", j, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	for i, err := range writeErrs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	for i, err := range reAdmitErrs {
		if err != nil {
			t.Fatalf("re-admitter %d: %v", i, err)
		}
	}

	// Every writer has returned, so no lease is live and the last
	// write's own round drove the entry count down to the cap.
	snap := mustSnapshot(t, l, ctx)
	if len(snap.Tasks) > 4 {
		t.Fatalf("snapshot holds %d records, want at most the cap of 4", len(snap.Tasks))
	}
	for _, rec := range snap.Tasks {
		if err := rec.Validate(); err != nil {
			t.Fatalf("surviving record %+v: %v", rec, err)
		}
	}
}

// runEvictionWriter admits, claims, and completes one writer key. A
// pending record can be deleted between Admit and Claim under cap
// pressure, so ErrNoKey is a documented outcome, not a failure.
func runEvictionWriter(ctx context.Context, l *ledger.Ledger, n int) error {
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
	return l.Complete(ctx, testActor, key, owner, fence, ledger.StatusCompleted, fixedNow)
}

// mustAdmitSeed admits "seed" and returns its key, keeping the test
// body on one readable line.
func mustAdmitSeed(t *testing.T, l *ledger.Ledger, ctx context.Context) ledger.IdempotencyKey {
	t.Helper()
	mustAdmit(t, l, ctx, "seed", 1)
	return "seed"
}
