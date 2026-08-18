package taskrun_test

import (
	"context"
	"slices"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/taskrun"
)

// TestCeremonyEmitsOrder proves a successful Run emits Admitted,
// Claimed, then Completed on one real bus.
func TestCeremonyEmitsOrder(t *testing.T) {
	ctx := context.Background()
	var got []events.Name
	bus := events.New()
	if err := bus.Subscribe(ledger.AdmittedEvent, recordNames(&got)); err != nil {
		t.Fatalf("Subscribe Admitted: %v", err)
	}
	if err := bus.Subscribe(ledger.ClaimedEvent, recordNames(&got)); err != nil {
		t.Fatalf("Subscribe Claimed: %v", err)
	}
	if err := bus.Subscribe(ledger.CompletedEvent, recordNames(&got)); err != nil {
		t.Fatalf("Subscribe Completed: %v", err)
	}
	l, err := ledger.New(nil, bus)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	task := taskrun.Task{Key: "key", Seq: 1, Description: "build"}
	if err := taskrun.Run(ctx, runOpts(l), task, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []events.Name{ledger.AdmittedEvent, ledger.ClaimedEvent, ledger.CompletedEvent}
	if !slices.Equal(got, want) {
		t.Fatalf("event order = %v, want %v", got, want)
	}
}

// recordNames returns a Handler appending each received event name.
func recordNames(s *[]events.Name) events.Handler {
	return func(_ context.Context, e events.Event) error {
		*s = append(*s, e.Name)
		return nil
	}
}
