package taskrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestPreclaimedLeaseRejected proves a key claimed under a live lease
// returns an error satisfying errors.Is(err, ledger.ErrLeaseActive),
// never runs work, and emits no CompletedEvent.
func TestPreclaimedLeaseRejected(t *testing.T) {
	ctx := context.Background()
	completed := 0
	bus := events.New()
	if err := bus.Subscribe(ledger.CompletedEvent, func(_ context.Context, _ events.Event) error {
		completed++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	l, err := ledger.New(nil, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := l.Admit(ctx, "actor", "key", 1, "boot", fixedNow); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if _, err := l.Claim(ctx, "actor", "key", "owner-a", fixedLease, fixedNow); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	called := 0
	err = taskrun.Run(ctx, runOpts(l), taskrun.Task{Key: "key", Seq: 1}, func(context.Context) error {
		called++
		return nil
	})
	if !errors.Is(err, ledger.ErrLeaseActive) {
		t.Fatalf("Run = %v, want ErrLeaseActive", err)
	}
	if called != 0 {
		t.Fatalf("work ran under a live lease (%d calls)", called)
	}
	if completed != 0 {
		t.Fatalf("CompletedEvent fired %d times", completed)
	}
}
