package ledger_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// newLedger builds a Ledger over a fresh MemStore and bus for a test.
func newLedger(t *testing.T, bus *events.Bus) *ledger.Ledger {
	t.Helper()
	l, err := ledger.New(nil, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l
}

// TestAdmitFirstSucceeds proves the first Admit for a key stores a
// StatusPending record and reports success.
func TestAdmitFirstSucceeds(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	ok, err := l.Admit(ctx, "k1", 1, "payload")
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !ok {
		t.Fatalf("Admit: want true, got false")
	}
	st, found, err := l.State(ctx, "k1")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !found {
		t.Fatalf("State: want found")
	}
	if st.Status != ledger.StatusPending {
		t.Fatalf("Status = %q, want StatusPending", st.Status)
	}
	if st.Sequence != 1 {
		t.Fatalf("Sequence = %d, want 1", st.Sequence)
	}
}

// TestAdmitDuplicateSequenceRejected proves a resubmission at the
// same or a lower sequence is a no-op, not an error.
func TestAdmitDuplicateSequenceRejected(t *testing.T) {
	ctx := context.Background()
	cases := []ledger.Sequence{2, 1}
	for _, seq := range cases {
		l := newLedger(t, nil)
		if _, err := l.Admit(ctx, "k1", 2, "first"); err != nil {
			t.Fatalf("Admit: %v", err)
		}
		ok, err := l.Admit(ctx, "k1", seq, "second")
		if err != nil {
			t.Fatalf("Admit: %v", err)
		}
		if ok {
			t.Fatalf("Admit at sequence %d: want false, got true", seq)
		}
		st, _, err := l.State(ctx, "k1")
		if err != nil {
			t.Fatalf("State: %v", err)
		}
		if st.Task != "first" {
			t.Fatalf("Task = %v, want unchanged %q", st.Task, "first")
		}
	}
}

// TestAdmitRebasesPendingOrClaimed proves a higher-sequence Admit
// rebases a StatusPending or a StatusClaimed record.
func TestAdmitRebasesPendingOrClaimed(t *testing.T) {
	ctx := context.Background()

	t.Run("pending", func(t *testing.T) {
		l := newLedger(t, nil)
		if _, err := l.Admit(ctx, "k1", 1, "first"); err != nil {
			t.Fatalf("Admit: %v", err)
		}
		ok, err := l.Admit(ctx, "k1", 2, "second")
		if err != nil {
			t.Fatalf("Admit: %v", err)
		}
		if !ok {
			t.Fatalf("Admit: want true, got false")
		}
		st, _, _ := l.State(ctx, "k1")
		if st.Task != "second" || st.Sequence != 2 {
			t.Fatalf("state after rebase = %+v", st)
		}
	})

	t.Run("claimed", func(t *testing.T) {
		l := newLedger(t, nil)
		if _, err := l.Admit(ctx, "k1", 1, "first"); err != nil {
			t.Fatalf("Admit: %v", err)
		}
		if _, err := l.Claim(ctx, "k1", "owner-a", fixedLease, fixedNow); err != nil {
			t.Fatalf("Claim: %v", err)
		}
		ok, err := l.Admit(ctx, "k1", 2, "second")
		if err != nil {
			t.Fatalf("Admit: %v", err)
		}
		if !ok {
			t.Fatalf("Admit: want true, got false")
		}
		st, _, _ := l.State(ctx, "k1")
		if st.Status != ledger.StatusPending {
			t.Fatalf("Status after rebase = %q, want StatusPending", st.Status)
		}
	})
}

// TestAdmitTerminalNeverRebases proves a higher-sequence Admit
// against a terminal record leaves it unchanged.
func TestAdmitTerminalNeverRebases(t *testing.T) {
	statuses := []struct {
		name  string
		build func(t *testing.T, l *ledger.Ledger, ctx context.Context)
	}{
		{"completed", buildCompleted},
		{"failed", buildFailed},
		{"blocked", buildBlocked},
	}
	for _, sc := range statuses {
		t.Run(sc.name, func(t *testing.T) {
			ctx := context.Background()
			l := newLedger(t, nil)
			sc.build(t, l, ctx)
			before, _, _ := l.State(ctx, "k1")
			ok, err := l.Admit(ctx, "k1", before.Sequence+10, "resubmit")
			if err != nil {
				t.Fatalf("Admit: %v", err)
			}
			if ok {
				t.Fatalf("Admit against terminal record: want false, got true")
			}
			after, _, _ := l.State(ctx, "k1")
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("record changed: before %+v, after %+v", before, after)
			}
		})
	}
}

// TestAdmitRecordsNeeds proves Admit stores the Needs list for later
// blocking lookups.
func TestAdmitRecordsNeeds(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	if _, err := l.Admit(ctx, "root", 1, nil); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if _, err := l.Admit(ctx, "dep", 1, nil, "root"); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	st, _, _ := l.State(ctx, "dep")
	if len(st.Needs) != 1 || st.Needs[0] != "root" {
		t.Fatalf("Needs = %v, want [root]", st.Needs)
	}
}

// TestAdmitEventFiresOnceOnSuccess proves AdmittedEvent fires only on
// a successful admission, never on a rejected duplicate or a rejected
// post-completion resubmission.
func TestAdmitEventFiresOnceOnSuccess(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	count := 0
	if err := bus.Subscribe(ledger.AdmittedEvent, func(_ context.Context, _ events.Event) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l := newLedger(t, bus)
	if _, err := l.Admit(ctx, "k1", 1, nil); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if _, err := l.Admit(ctx, "k1", 1, nil); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if count != 1 {
		t.Fatalf("AdmittedEvent fired %d times, want 1", count)
	}

	if _, err := l.Claim(ctx, "k1", "owner-a", fixedLease, fixedNow); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	fence, _, _ := stateFence(t, l, ctx, "k1")
	if err := l.Complete(ctx, "k1", "owner-a", fence, ledger.StatusCompleted); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := l.Admit(ctx, "k1", 100, nil); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if count != 1 {
		t.Fatalf("AdmittedEvent fired %d times after terminal resubmit, want 1", count)
	}
}

func stateFence(t *testing.T, l *ledger.Ledger, ctx context.Context, key ledger.IdempotencyKey) (ledger.FenceToken, ledger.OwnerID, bool) {
	t.Helper()
	st, found, err := l.State(ctx, key)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	return st.Fence, st.Owner, found
}
