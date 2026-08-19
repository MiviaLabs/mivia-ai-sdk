package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestLeaseExpiryAloneDoesNotFence proves a record whose lease has
// passed but that nobody has taken over still honors its owner's
// fence: Renew and Complete both succeed under the original token.
// Expiry is a staleness signal Claim and Takeover read, not a fence.
// Only a later Claim or Takeover bumps the token and fences the owner.
func TestLeaseExpiryAloneDoesNotFence(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	fence := mustClaim(t, l, ctx, "k1", "owner-a")

	pastLease := fixedNow.Add(fixedLease + time.Second)
	if err := l.Renew(ctx, testActor, "k1", "owner-a", fence, fixedLease, pastLease); err != nil {
		t.Fatalf("Renew after lease expiry without takeover: %v, want nil", err)
	}
	if err := l.Complete(ctx, testActor, "k1", "owner-a", fence, ledger.StatusCompleted, pastLease); err != nil {
		t.Fatalf("Complete after lease expiry without takeover: %v, want nil", err)
	}

	st, found, err := l.State(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("State: found=%v err=%v", found, err)
	}
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("Status = %q, want StatusCompleted", st.Status)
	}
}
