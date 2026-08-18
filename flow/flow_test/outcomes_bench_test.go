package flow_test

// Performance: the phase 5 three-step linear baseline, rebenchmarked
// after phase 21 added the outcomes map to Run.
//
// Measured on the build machine (go1.26.0, linux/amd64, AMD Ryzen 9
// 9900X, go test -bench -benchmem -count=3): 281.6 ns/op, 592 B/op,
// 8 allocs/op. The outcomes map raises the phase 5 baseline of 6
// allocs/op to 8; the budget below allows up to 12 allocs (a 50%
// margin over the measured 8), so a small regression in the outcomes
// bookkeeping trips the check.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// BenchmarkRunReport measures Run's Report-returning path on a linear
// three-step graph with a no-op confirm.
func BenchmarkRunReport(b *testing.B) {
	d, m := benchLinearGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// TestRunReportAllocBudget asserts the allocation budget for Run's
// Report-returning path on the linear three-step graph: no more than
// 12 allocations, a 50% margin over the measured 8-alloc baseline
// (see the file's leading comment).
func TestRunReportAllocBudget(t *testing.T) {
	d, m := benchLinearGraph(t)
	ctx := context.Background()
	const budget = 12
	got := testing.AllocsPerRun(10, func() {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if got > budget {
		t.Fatalf("AllocsPerRun = %v, want at most %d", got, budget)
	}
}
