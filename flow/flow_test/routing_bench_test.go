package flow_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// benchFiveStepBranchGraph builds a five-step branch Definition and
// its matching machine Definition: root, branch, two alternatives,
// join. The route keeps the "left" alternative and skips "right".
func benchFiveStepBranchGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "root", To: "r"},
		{ID: "branch", Needs: []string{"root"}, To: "b", Route: keeping("left")},
		{ID: "left", Needs: []string{"branch"}, To: "l"},
		{ID: "right", Needs: []string{"branch"}, To: "rt"},
		{ID: "join", Needs: []string{"left", "right"}, To: "j"},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("r"), To: machine.Status("b"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("b"), To: machine.Status("l"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("l"), To: machine.Status("j"), Trigger: triggerGo},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// BenchmarkRunFiveStepBranch measures Run on a five-step branch graph:
// root, branch, two alternatives (one skipped by Route), join.
//
// Baseline, measured on the phase 21 code before this phase landed,
// on a five-step linear graph with no branching (AMD Ryzen 9 9900X,
// go test -bench, -benchtime=200000x): 557.1 ns/op, 816 B/op,
// 12 allocs/op.
//
// This phase's five-step branch graph, same hardware, same run:
// 616.0 ns/op, 992 B/op, 12 allocs/op. Ratio against the linear
// baseline: 1.11x time, 1.22x bytes, 1.00x allocs. The route closure
// call adds non-deterministic overhead on top of the admission-verdict
// scan every step now goes through, so this file sets no fixed
// allocation budget; PHASES.md permits reporting the allocs/op ratio
// against the linear baseline instead.
func BenchmarkRunFiveStepBranch(b *testing.B) {
	d, m := benchFiveStepBranchGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}
