package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestTakeoverRejectsEmptyOwner proves Takeover returns ErrEmptyOwner
// and does not touch the store when owner is empty.
func TestTakeoverRejectsEmptyOwner(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	mustClaim(t, l, ctx, "k1", "owner-a")
	stale := fixedNow.Add(fixedLease)
	_, err := l.Takeover(ctx, testActor, "k1", "", fixedLease, stale)
	if !errors.Is(err, ledger.ErrEmptyOwner) {
		t.Fatalf("Takeover: got %v, want ErrEmptyOwner", err)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Owner != "owner-a" {
		t.Fatalf("Owner = %q, want owner-a", st.Owner)
	}
}

// TestTakeoverWhileLeaseLiveRejected proves Takeover while LeaseUntil
// is still after now returns ErrNotStale.
func TestTakeoverWhileLeaseLiveRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	mustClaim(t, l, ctx, "k1", "owner-a")
	_, err := l.Takeover(ctx, testActor, "k1", "owner-b", fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNotStale) {
		t.Fatalf("Takeover: got %v, want ErrNotStale", err)
	}
}

// TestTakeoverAtStaleDeadlineSucceeds proves Takeover called with a
// now at or after LeaseUntil succeeds, bumps the fence, and records
// the new owner.
func TestTakeoverAtStaleDeadlineSucceeds(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	first := mustClaim(t, l, ctx, "k1", "owner-a")
	stale := fixedNow.Add(fixedLease)
	takeoverActor := ledger.Actor("takeover-actor")
	second, err := l.Takeover(ctx, takeoverActor, "k1", "owner-b", fixedLease, stale)
	if err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if second <= first {
		t.Fatalf("fence %d did not bump past %d", second, first)
	}
	st, _, _ := l.State(ctx, "k1")
	if st.Owner != "owner-b" {
		t.Fatalf("Owner = %q, want owner-b", st.Owner)
	}
	if st.UpdatedBy != takeoverActor || !st.UpdatedAt.Equal(stale) {
		t.Fatalf("UpdatedBy/UpdatedAt = %q/%v, want %q/%v", st.UpdatedBy, st.UpdatedAt, takeoverActor, stale)
	}
	if st.CreatedBy != testActor || !st.CreatedAt.Equal(fixedNow) {
		t.Fatalf("CreatedBy/CreatedAt = %q/%v, want unchanged %q/%v", st.CreatedBy, st.CreatedAt, testActor, fixedNow)
	}
}

// TestTakeoverAgainstPendingRejected proves Takeover against a
// StatusPending record returns ErrNotClaimed: Claim, not Takeover, is
// required to admit ownership of a never-claimed record.
func TestTakeoverAgainstPendingRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	_, err := l.Takeover(ctx, testActor, "k1", "owner-b", fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Takeover: got %v, want ErrNotClaimed", err)
	}
}

// TestTakeoverAgainstTerminalRejected proves Takeover against a
// terminal record returns ErrNotClaimed.
func TestTakeoverAgainstTerminalRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	buildCompleted(t, l, ctx)
	later := fixedNow.Add(2 * fixedLease)
	_, err := l.Takeover(ctx, testActor, "k1", "owner-b", fixedLease, later)
	if !errors.Is(err, ledger.ErrNotClaimed) {
		t.Fatalf("Takeover: got %v, want ErrNotClaimed", err)
	}
}

// TestTakeoverAgainstUnknownKeyRejected proves Takeover against a
// never-admitted key returns ErrNoKey, not ErrNotClaimed.
func TestTakeoverAgainstUnknownKeyRejected(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	_, err := l.Takeover(ctx, testActor, "ghost", "owner-b", fixedLease, fixedNow)
	if !errors.Is(err, ledger.ErrNoKey) {
		t.Fatalf("Takeover: got %v, want ErrNoKey", err)
	}
}

// TestTakeoverFencesDispossessedOwner proves a Renew call from the
// dispossessed owner's fence, made after a successful Takeover,
// returns ErrFenced.
func TestTakeoverFencesDispossessedOwner(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t, nil)
	mustAdmit(t, l, ctx, "k1", 1)
	priorFence := mustClaim(t, l, ctx, "k1", "owner-a")
	stale := fixedNow.Add(fixedLease)
	if _, err := l.Takeover(ctx, testActor, "k1", "owner-b", fixedLease, stale); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	err := l.Renew(ctx, testActor, "k1", "owner-a", priorFence, fixedLease, stale)
	if !errors.Is(err, ledger.ErrFenced) {
		t.Fatalf("Renew from dispossessed owner: got %v, want ErrFenced", err)
	}
}

// TestTakeoverEventNamesNewOwner proves TakenOverEvent fires once,
// naming the new owner.
func TestTakeoverEventNamesNewOwner(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	var data string
	count := 0
	if err := bus.Subscribe(ledger.TakenOverEvent, func(_ context.Context, e events.Event) error {
		count++
		data = e.Data
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l := newLedger(t, bus)
	mustAdmit(t, l, ctx, "k1", 1)
	mustClaim(t, l, ctx, "k1", "owner-a")
	stale := fixedNow.Add(fixedLease)
	if _, err := l.Takeover(ctx, testActor, "k1", "owner-b", fixedLease, stale); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if count != 1 {
		t.Fatalf("TakenOverEvent fired %d times, want 1", count)
	}
	if !strings.Contains(data, "owner-b") {
		t.Fatalf("event data %q does not name the new owner", data)
	}
}
