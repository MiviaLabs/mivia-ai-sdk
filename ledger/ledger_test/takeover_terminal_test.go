package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestTakeoverAgainstCompletedWithLiveLeaseRejected proves Takeover
// checks the claimed-status precondition before staleness. Complete
// leaves LeaseUntil on the record, so a completed record inside its
// lease window reaches both checks. The test asserts the live-lease
// precondition before it acts, so a fixture whose lease had already
// expired could not pass vacuously.
func TestTakeoverAgainstCompletedWithLiveLeaseRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	buildCompleted(t, l, ctx)

	st, found, err := l.State(ctx, "k1")
	if err != nil {
		t.Fatalf("State(k1): %v", err)
	}
	if !found {
		t.Fatalf("State(k1): want found")
	}
	if st.Status != ledger.StatusCompleted {
		t.Fatalf("k1.Status = %q, want %q", st.Status, ledger.StatusCompleted)
	}
	if !st.LeaseUntil.After(fixedNow) {
		t.Fatalf("k1.LeaseUntil = %v, want after %v", st.LeaseUntil, fixedNow)
	}

	if _, err := l.Takeover(ctx, testActor, "k1", "owner-b", fixedLease, fixedNow); !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Takeover(k1) = %v, want ErrNotClaimed", err)
	}
}
