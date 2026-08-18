package ledger_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
)

// TestAdmitClaimRenewCompleteEndToEnd runs an end-to-end sequence
// through one Ledger backed by MemStore: admit, claim, renew twice,
// complete failed, and a dependent's resulting blocked state. A real
// events.Bus collects every emitted event in order.
func TestAdmitClaimRenewCompleteEndToEnd(t *testing.T) {
	ctx := context.Background()
	bus := events.New()
	var order []events.Name
	record := func(_ context.Context, e events.Event) error {
		order = append(order, e.Name)
		return nil
	}
	for _, name := range []events.Name{
		ledger.AdmittedEvent, ledger.ClaimedEvent, ledger.RenewedEvent,
		ledger.CompletedEvent, ledger.BlockedEvent,
	} {
		if err := bus.Subscribe(name, record); err != nil {
			t.Fatalf("Subscribe(%s): %v", name, err)
		}
	}

	l, err := ledger.New(ledger.NewMemStore(), bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if ok, err := l.Admit(ctx, testActor, "root", 1, "root-task", fixedNow); err != nil || !ok {
		t.Fatalf("Admit(root): ok=%v err=%v", ok, err)
	}
	if ok, err := l.Admit(ctx, testActor, "dep", 1, "dep-task", fixedNow, "root"); err != nil || !ok {
		t.Fatalf("Admit(dep): ok=%v err=%v", ok, err)
	}

	fence, err := l.Claim(ctx, testActor, "root", "owner-a", time.Minute, fixedNow)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	afterFirstRenew := fixedNow.Add(30 * time.Second)
	if err := l.Renew(ctx, testActor, "root", "owner-a", fence, time.Minute, afterFirstRenew); err != nil {
		t.Fatalf("Renew 1: %v", err)
	}
	afterSecondRenew := afterFirstRenew.Add(30 * time.Second)
	if err := l.Renew(ctx, testActor, "root", "owner-a", fence, time.Minute, afterSecondRenew); err != nil {
		t.Fatalf("Renew 2: %v", err)
	}

	if err := l.Complete(ctx, testActor, "root", "owner-a", fence, ledger.StatusFailed, fixedNow); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	blockedBy, blocked, err := l.Blocked(ctx, "dep")
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	if !blocked {
		t.Fatalf("dep: want blocked")
	}
	if blockedBy != "root" {
		t.Fatalf("BlockedBy = %q, want root", blockedBy)
	}

	wantOrder := []events.Name{
		ledger.AdmittedEvent, ledger.AdmittedEvent,
		ledger.ClaimedEvent,
		ledger.RenewedEvent, ledger.RenewedEvent,
		ledger.CompletedEvent,
		ledger.BlockedEvent,
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("event order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("event %d = %q, want %q (full: %v)", i, order[i], wantOrder[i], order)
		}
	}
}
