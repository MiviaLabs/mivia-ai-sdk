package flow_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// benchLinearGraph builds the linear three-step Definition and its
// matching machine Definition for BenchmarkRun and the alloc budget
// check. tb accepts either a *testing.B or a *testing.T.
func benchLinearGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	const (
		s1 = machine.Status("s1")
		s2 = machine.Status("s2")
		s3 = machine.Status("s3")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(s1)},
		{ID: "b", Needs: []string{"a"}, To: string(s2)},
		{ID: "c", Needs: []string{"b"}, To: string(s3)},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: s1, Trigger: triggerGo},
		machine.Transition{From: s1, To: s2, Trigger: triggerGo},
		machine.Transition{From: s2, To: s3, Trigger: triggerGo},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// BenchmarkRun measures Run on a linear three-step graph with a
// no-op confirm. Target: under one millisecond for three steps.
//
// Original phase 5 baseline, before the outcomes map existed:
// 217.4 ns/op, 336 B/op, 6 allocs/op (AMD Ryzen 9 9900X, go test
// -bench). Phase 21 added the outcomes map to every Run call; the
// current measured allocation count is 8 (see outcomes_bench_test.go
// for the up-to-date baseline and its 50 percent margin). The budget
// below stays at the phase 5 value of 9 allocs, so it still traps a
// regression, with a smaller margin than the original 50 percent.
func BenchmarkRun(b *testing.B) {
	d, m := benchLinearGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
	elapsed := time.Since(start)
	if b.N > 0 {
		perOp := elapsed / time.Duration(b.N)
		if perOp > time.Millisecond {
			b.Fatalf("Run took %v per op, want under 1ms", perOp)
		}
	}
}

// TestRunAllocBudget asserts the allocation budget for Run on the
// linear three-step graph: no more than 9 allocations. This covers
// the ready-step scan and the record copies Fire already makes, with
// a 50% margin over the measured 6-alloc baseline (see BenchmarkRun).
func TestRunAllocBudget(t *testing.T) {
	d, m := benchLinearGraph(t)
	ctx := context.Background()
	const budget = 9
	got := testing.AllocsPerRun(10, func() {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if got > budget {
		t.Fatalf("AllocsPerRun = %v, want at most %d", got, budget)
	}
}
