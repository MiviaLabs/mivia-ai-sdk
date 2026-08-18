package ledger_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestTakeoverRaceAgainstClaimExactlyOneWinner admits and claims a
// key with a short lease, computes a now past LeaseUntil, then races
// one goroutine calling Claim against another calling Takeover on the
// same key at the same now. Exactly one call succeeds; the other
// returns its own rejection error, proving the single underlying
// Store.CompareAndSwap call decides the winner. Run under
// go test -race.
func TestTakeoverRaceAgainstClaimExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	mustClaim(t, l, ctx, "k1", "owner-a")
	stale := fixedNow.Add(fixedLease)

	var wg sync.WaitGroup
	var claimErr, takeoverErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, claimErr = l.Claim(ctx, "k1", "owner-b", fixedLease, stale)
	}()
	go func() {
		defer wg.Done()
		_, takeoverErr = l.Takeover(ctx, "k1", "owner-c", fixedLease, stale)
	}()
	wg.Wait()

	claimWon := claimErr == nil
	takeoverWon := takeoverErr == nil
	if claimWon == takeoverWon {
		t.Fatalf("want exactly one winner: claimErr=%v takeoverErr=%v", claimErr, takeoverErr)
	}
	if !claimWon && !errors.Is(claimErr, ledger.ErrLeaseActive) {
		t.Fatalf("losing Claim error = %v, want ErrLeaseActive", claimErr)
	}
	if !takeoverWon && !errors.Is(takeoverErr, ledger.ErrNotStale) {
		t.Fatalf("losing Takeover error = %v, want ErrNotStale", takeoverErr)
	}
}
