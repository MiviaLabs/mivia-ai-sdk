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
// Measured: ~14 ns/op, 0 B/op, 0 allocs/op (map lookup plus a stub's
// Run, no allocation in either path).
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
// registry of one hundred tools. The measured baseline is zero
// allocations: the map lookup and the stub's Run both allocate
// nothing for this InOut/Out shape. The budget allows one allocation
// above the baseline to absorb a small, legitimate change without
// masking a real regression.
func TestRunAllocBudget(t *testing.T) {
	r := buildHundredToolRegistry()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	alloc := testing.AllocsPerRun(100, func() {
		if _, err := r.Run(ctx, "tool-050", in); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 1 {
		t.Fatalf("Run allocated %v times per call; budget is 1", alloc)
	}
}
