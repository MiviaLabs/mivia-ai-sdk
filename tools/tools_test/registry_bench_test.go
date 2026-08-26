package tools_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// buildHundredToolRegistry creates a Registry with one hundred
// registered tools.
func buildHundredToolRegistry() *tools.Registry {
	r := tools.New()
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("tool-%03d", i)
		if err := r.Add(&stubTool{name: name, result: i}); err != nil {
			panic("buildHundredToolRegistry: " + err.Error())
		}
	}
	return r
}

// BenchmarkRunHundredTools benchmarks Run against a Registry already
// holding one hundred tools.
// Target: under one microsecond per call.
// Measured: ~760 ns/op, 608 B/op, 8 allocs/op: the bounded dispatch
// in registry_timeout.go adds the derived timeout context, the
// handoff channel, and the producer goroutine over the map lookup and
// stub's Run.
func BenchmarkRunHundredTools(b *testing.B) {
	r := buildHundredToolRegistry()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Run(ctx, "tool-050", in); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRunAllocBudget guards the allocation floor for Run over a
// registry of one hundred tools. The pre-backstop baseline was zero;
// the bounded dispatch costs eight allocations for this InOut/Out
// shape: four for the derived timeout context, two for the handoff
// channel and its producer goroutine, two for the goroutine's closure.
// The budget allows one allocation above the measured baseline to
// absorb a small, legitimate change without masking a real regression.
func TestRunAllocBudget(t *testing.T) {
	r := buildHundredToolRegistry()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	alloc := testing.AllocsPerRun(100, func() {
		if _, err := r.Run(ctx, "tool-050", in); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 9 {
		t.Fatalf("Run allocated %v times per call; budget is 9", alloc)
	}
}
