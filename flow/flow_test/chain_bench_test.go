package flow_test

// Performance: three-level chained workflow vs. an equivalent flat
// workflow. The chain must stay under two milliseconds and must not
// allocate more than 1.5 times the flat baseline.
//
// Measured after phase 24 review fixes (go1.26.0, linux/amd64,
// AMD Ryzen 9 9900X, go test -bench -benchmem -count=3):
// BenchmarkRunFlatThreeSteps: 348 ns/op, 448 B/op, 8 allocs/op.
// BenchmarkRunChainedThreeLevels: 281 ns/op, 704 B/op, 8 allocs/op.
// Ratio: allocs/op 1.0x. The alloc count carries four allocations of
// margin against the budget: 8 against a limit of 12. The counts
// depend on the toolchain and its escape analysis. Re-measure on the
// local toolchain before you judge a failure.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// flatThreeGraph builds a four-step linear workflow with no nesting.
// It matches the four steps of the three-level chained workflow.
func flatThreeGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	const (
		statusA = machine.Status("a")
		statusB = machine.Status("b")
		statusC = machine.Status("c")
	)
	d, err := flow.New([]flow.Step{
		{ID: "s1", To: string(statusA)},
		{ID: "s2", Needs: []string{"s1"}, To: string(statusB)},
		{ID: "s3", Needs: []string{"s2"}, To: string(statusC)},
		{ID: "s4", Needs: []string{"s3"}, To: string(statusDone)},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusA, Trigger: triggerGo},
		machine.Transition{From: statusA, To: statusB, Trigger: triggerGo},
		machine.Transition{From: statusB, To: statusC, Trigger: triggerGo},
		machine.Transition{From: statusC, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// chainedThreeGraph builds a three-level nested workflow. Each level
// has one step, so the total step count matches the flat baseline.
func chainedThreeGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	const (
		statusA = machine.Status("a")
		statusB = machine.Status("b")
	)
	level0, err := flow.New([]flow.Step{
		{ID: "l0", To: string(statusDone)},
	}, nil)
	if err != nil {
		tb.Fatalf("level0 New: %v", err)
	}
	level1, err := flow.New([]flow.Step{
		{ID: "l1", Sub: level0},
	}, nil)
	if err != nil {
		tb.Fatalf("level1 New: %v", err)
	}
	level2, err := flow.New([]flow.Step{
		{ID: "l2", Sub: level1},
	}, nil)
	if err != nil {
		tb.Fatalf("level2 New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "root", Sub: level2},
	}, nil)
	if err != nil {
		tb.Fatalf("root New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusA, Trigger: triggerGo},
		machine.Transition{From: statusA, To: statusB, Trigger: triggerGo},
		machine.Transition{From: statusB, To: statusDone, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: machine.Trigger("shortcut")},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// BenchmarkRunFlatThreeSteps measures the flat baseline.
func BenchmarkRunFlatThreeSteps(b *testing.B) {
	d, m := flatThreeGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// BenchmarkRunChainedThreeLevels measures the three-level chain.
func BenchmarkRunChainedThreeLevels(b *testing.B) {
	d, m := chainedThreeGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// TestChainedAllocBudget asserts the chained workflow stays under the
// budget when run once outside the benchmark harness.
func TestChainedAllocBudget(t *testing.T) {
	t.Parallel()
	d, m := chainedThreeGraph(t)
	ctx := context.Background()

	// Warm up to stabilize any one-time allocation.
	_, _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil)
	if err != nil {
		t.Fatalf("warm-up Run: %v", err)
	}

	start := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
				b.Fatalf("Run: %v", err)
			}
		}
	})

	flat := testing.Benchmark(func(b *testing.B) {
		d, m := flatThreeGraph(b)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
				b.Fatalf("Run: %v", err)
			}
		}
	})

	if start.NsPerOp() > 2_000_000 {
		t.Fatalf("chained ns/op = %d, want <= 2ms", start.NsPerOp())
	}
	if flat.AllocsPerOp() == 0 {
		t.Skip("flat baseline made zero allocations; ratio is undefined")
	}
	if start.AllocsPerOp() > flat.AllocsPerOp()*3/2 {
		t.Fatalf("chained allocs/op = %d, want <= 1.5x flat = %d",
			start.AllocsPerOp(), flat.AllocsPerOp()*3/2)
	}
}
