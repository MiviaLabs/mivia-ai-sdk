package events_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// TestObserveThenVetoIntegration runs two handlers at PointPreTool as
// one flow: the first observes and records, the second vetoes. Fire
// reports ErrVetoed, the observation landed, and no handler ran
// twice.
func TestObserveThenVetoIntegration(t *testing.T) {
	r := events.NewRegistry()
	var observed []string
	observedCalls := 0
	vetoCalls := 0

	observer := func(_ context.Context, payload any) (bool, error) {
		observedCalls++
		name, _ := payload.(string)
		observed = append(observed, name)
		return true, nil
	}
	vetoer := func(context.Context, any) (bool, error) {
		vetoCalls++
		return false, nil
	}
	if err := r.Add(events.PointPreTool, "audit-log", observer); err != nil {
		t.Fatalf("Add(audit-log): %v", err)
	}
	if err := r.Add(events.PointPreTool, "policy-gate", vetoer); err != nil {
		t.Fatalf("Add(policy-gate): %v", err)
	}

	err := r.Fire(context.Background(), events.PointPreTool, "rm -rf")
	if !errors.Is(err, events.ErrVetoed) {
		t.Fatalf("Fire = %v, want ErrVetoed", err)
	}
	if len(observed) != 1 || observed[0] != "rm -rf" {
		t.Fatalf("observed = %v, want one entry %q", observed, "rm -rf")
	}
	if observedCalls != 1 {
		t.Fatalf("observer ran %d times, want 1", observedCalls)
	}
	if vetoCalls != 1 {
		t.Fatalf("vetoer ran %d times, want 1", vetoCalls)
	}
}
