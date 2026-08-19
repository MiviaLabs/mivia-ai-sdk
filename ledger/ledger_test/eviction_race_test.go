package ledger_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestMemStoreEvictionUnderConcurrentAdmission drives a capped MemStore
// with concurrent admit-claim-complete writers while other goroutines
// re-admit a tombstoned terminal key, asserting eviction stays race
// free and a tombstoned key still rejects re-admission. Run under
// go test -race.
func TestMemStoreEvictionUnderConcurrentAdmission(t *testing.T) {
	ctx := context.Background()
	store, err := ledger.NewMemStoreWithOptions(ledger.MemStoreOptions{MaxEntries: 4})
	if err != nil {
		t.Fatalf("NewMemStoreWithOptions: %v", err)
	}
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
			key := ledger.IdempotencyKey(fmt.Sprintf("w%d", n))
			owner := ledger.OwnerID(fmt.Sprintf("owner-w%d", n))
			if ok, err := l.Admit(ctx, testActor, key, 1, "payload", fixedNow); err != nil || !ok {
				writeErrs[n] = fmt.Errorf("Admit(%s): ok=%v err=%v", key, ok, err)
				return
			}
			f, err := l.Claim(ctx, testActor, key, owner, fixedLease, fixedNow)
			if err != nil {
				writeErrs[n] = fmt.Errorf("Claim(%s): %v", key, err)
				return
			}
			writeErrs[n] = l.Complete(ctx, testActor, key, owner, f, ledger.StatusCompleted, fixedNow)
		}(i)
	}
	for i := 0; i < reAdmitters; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < reAdmitRounds; j++ {
				ok, err := l.Admit(ctx, testActor, "seed", 99, "re-admitted", fixedNow)
				if err != nil {
					reAdmitErrs[n] = err
					return
				}
				if ok {
					reAdmitErrs[n] = fmt.Errorf("re-admit round %d of tombstoned seed returned true", j)
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

	st, found, err := l.State(ctx, "seed")
	if err != nil || !found {
		t.Fatalf("State(seed): found=%v err=%v", found, err)
	}
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("seed Status = %q, want StatusCompleted preserved across eviction", st.Status)
	}
}

// mustAdmitSeed admits "seed" and returns its key, keeping the test
// body on one readable line.
func mustAdmitSeed(t *testing.T, l *ledger.Ledger, ctx context.Context) ledger.IdempotencyKey {
	t.Helper()
	mustAdmit(t, l, ctx, "seed", 1)
	return "seed"
}
