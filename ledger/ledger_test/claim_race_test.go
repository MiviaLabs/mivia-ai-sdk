package ledger_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestClaimRaceExactlyOneWinner races two goroutines calling Claim on
// the same freshly admitted key. Exactly one goroutine's Claim
// succeeds and the other observes ErrLeaseActive. Run under
// go test -race.
func TestClaimRaceExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	fences := make([]ledger.FenceToken, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := ledger.OwnerID("owner-a")
			if i == 1 {
				owner = "owner-b"
			}
			fence, err := l.Claim(ctx, testActor, "k1", owner, fixedLease, fixedNow)
			errs[i] = err
			fences[i] = fence
		}(i)
	}
	wg.Wait()

	successes, activeErrors := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ledger.ErrLeaseActive):
			activeErrors++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want 1 (errs: %v)", successes, errs)
	}
	if activeErrors != 1 {
		t.Fatalf("ErrLeaseActive count = %d, want 1 (errs: %v)", activeErrors, errs)
	}
}
