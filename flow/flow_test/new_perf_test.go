package flow_test

import (
	"strconv"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// buildHundredSteps builds a linear chain of one hundred steps.
// Each step after the head needs the prior step, so the graph has a
// single root and no cycles.
func buildHundredSteps() []flow.Step {
	steps := make([]flow.Step, 0, 100)
	for i := 0; i < 100; i++ {
		id := "s" + strconv.Itoa(i)
		s := flow.Step{ID: id}
		if i > 0 {
			s.Needs = []string{"s" + strconv.Itoa(i-1)}
		}
		steps = append(steps, s)
	}
	return steps
}

// BenchmarkNewHundred benchmarks New on a one-hundred-step graph.
// Target: under one millisecond.
// Baseline (empty implementation): no benchmark; New and the flow
// package did not exist.
// Measured with implementation: ~10.8 us/op, ~20 kB/op, 114 allocs/op.
// New copies the steps and builds the adjacency and in-degree slices
// for Kahn's algorithm. The cost scales with the steps and the edges.
func BenchmarkNewHundred(b *testing.B) {
	steps := buildHundredSteps()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := flow.New(steps, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// TestNewAllocBudget guards the allocation budget for New on one
// hundred steps. AllocsPerRun must stay at or below the measured
// baseline. A regression that allocates a new map or slice per step
// fails here.
func TestNewAllocBudget(t *testing.T) {
	steps := buildHundredSteps()
	alloc := testing.AllocsPerRun(1000, func() {
		if _, err := flow.New(steps, nil); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > float64(newAllocBudget()) {
		t.Fatalf("New allocated %.0f times per call; budget is %d", alloc, newAllocBudget())
	}
}

// newAllocBudget states the allocation budget for New on one hundred
// steps. Measured at 114 allocs/op; the budget holds headroom above
// that so a regression to quadratic allocation still fails.
func newAllocBudget() int {
	return 200
}
