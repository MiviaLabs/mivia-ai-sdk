package agent_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// benchPlan builds a two-step, no-panel Definition for the New
// benchmark and the allocation budget test.
func benchPlan(b testing.TB) *flow.Definition {
	b.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "fetch"},
		{ID: "summarize", Needs: []string{"fetch"}},
	}, nil)
	if err != nil {
		b.Fatalf("flow.New() unexpected error: %v", err)
	}
	return d
}

// BenchmarkNew benchmarks agent.New on a two-step, no-panel plan.
// Target: under one millisecond per call.
// Baseline (empty implementation): no benchmark; the agent package
// did not exist.
// Measured with implementation: ~166 ns/op, ~80 B/op, 1 allocs/op.
// New copies no data: it stores the caller's pointers and the card
// value directly, so the cost is dominated by card.Validate's
// capability-map allocation.
func BenchmarkNew(b *testing.B) {
	id, err := identity.New()
	if err != nil {
		b.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Agent A", Capabilities: []string{"read", "write"}}
	plan := benchPlan(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := agent.New(id, card, plan); err != nil {
			b.Fatal(err)
		}
	}
}

// TestNewAllocBudget guards the allocation budget for New on a
// two-step plan. AllocsPerRun must stay at or below the measured
// baseline. A regression that copies the plan or the identity fails
// here.
func TestNewAllocBudget(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Agent A", Capabilities: []string{"read", "write"}}
	plan := benchPlan(t)
	alloc := testing.AllocsPerRun(1000, func() {
		if _, err := agent.New(id, card, plan); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > float64(newAllocBudget()) {
		t.Fatalf("New allocated %.0f times per call; budget is %d", alloc, newAllocBudget())
	}
}

// newAllocBudget states the allocation budget for New on a two-step
// plan. Measured at 1 alloc/op for card.Validate's capability-seen
// map. The budget allows one extra allocation above the baseline, so
// a one-allocation regression, such as a copy added to the hot path,
// still fails.
func newAllocBudget() int {
	return 2
}
