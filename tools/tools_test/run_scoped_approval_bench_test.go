package tools_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// buildAlwaysApproveScope builds a Scope whose Approve always
// approves and whose ApprovalThreshold is the zero value, so every
// resolved tool (including an unclassified stub) triggers the
// approval check.
func buildAlwaysApproveScope() *tools.Scope {
	return tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) { return true, nil },
	})
}

// BenchmarkRunScopedApprovalHundredTools benchmarks RunScoped against
// a Registry of one hundred tools behind a Scope whose Approve runs on
// every call, next to BenchmarkRunScopedHundredTools's no-approval
// baseline in registry_run_scoped_bench_test.go.
// Target: under one microsecond per call.
// Measured: ~27 ns/op, 0 B/op, 0 allocs/op (map lookup, the rank
// compare, and a stub's Run, no allocation in any path).
func BenchmarkRunScopedApprovalHundredTools(b *testing.B) {
	r := buildHundredToolRegistry()
	scope := buildAlwaysApproveScope()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.RunScoped(ctx, "tool-025", in, scope); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRunScopedApprovalAllocBudget guards the allocation floor for
// RunScoped's approval path over a registry of one hundred tools. The
// measured baseline is zero allocations: the map lookup, the rank
// compare, the Approve closure call, and the stub's Run all allocate
// nothing for this InOut/Out shape. The budget allows one allocation
// above the baseline, matching RunScoped's no-approval budget in
// registry_run_scoped_bench_test.go, to absorb a small, legitimate
// change without masking a real regression.
func TestRunScopedApprovalAllocBudget(t *testing.T) {
	r := buildHundredToolRegistry()
	scope := buildAlwaysApproveScope()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	alloc := testing.AllocsPerRun(100, func() {
		if _, err := r.RunScoped(ctx, "tool-025", in, scope); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 1 {
		t.Fatalf("RunScoped (approval path) allocated %v times per call; budget is 1", alloc)
	}
}
