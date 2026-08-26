package tools_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// buildFiftyNameAllowlistScope builds a Scope allowlisting the first
// fifty of buildHundredToolRegistry's tool names.
func buildFiftyNameAllowlistScope() *tools.Scope {
	allow := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		allow = append(allow, fmt.Sprintf("tool-%03d", i))
	}
	return tools.NewScope(tools.ScopeOptions{Allowlist: allow})
}

// BenchmarkRunScopedHundredTools benchmarks RunScoped against a
// Registry of one hundred tools behind a Scope with a fifty-name
// allowlist.
// Target: under one microsecond per call, next to Run's baseline in
// registry_bench_test.go.
// Measured: ~810 ns/op, 608 B/op, 8 allocs/op: the bounded dispatch
// in registry_timeout.go adds the derived timeout context, the
// handoff channel, and the producer goroutine over the map lookup,
// the Scope.Allowed check, and the stub's Run.
func BenchmarkRunScopedHundredTools(b *testing.B) {
	r := buildHundredToolRegistry()
	scope := buildFiftyNameAllowlistScope()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.RunScoped(ctx, "tool-025", in, scope); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRunScopedAllocBudget guards the allocation floor for RunScoped
// over a registry of one hundred tools behind a fifty-name allowlist
// Scope. The pre-backstop baseline was zero allocations; the bounded
// dispatch costs eight for this InOut/Out shape, matching Run's cost
// in registry_bench_test.go. The budget allows one allocation above
// the measured baseline, matching Run's budget in
// registry_bench_test.go, to absorb a small, legitimate change
// without masking a real regression.
func TestRunScopedAllocBudget(t *testing.T) {
	r := buildHundredToolRegistry()
	scope := buildFiftyNameAllowlistScope()
	ctx := context.Background()
	in := tools.InOut{Value: "fixed"}
	alloc := testing.AllocsPerRun(100, func() {
		if _, err := r.RunScoped(ctx, "tool-025", in, scope); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 9 {
		t.Fatalf("RunScoped allocated %v times per call; budget is 9", alloc)
	}
}
