package flow_test

// Baseline (AMD Ryzen 9 9900X, go test -bench, 50000 iterations):
// the ten-separate-steps shape, measured on the currently shipped
// code through phase 30, before this phase landed: 5475 ns/op, 13080
// B/op, 65 allocs/op.
//
// Measured on this phase's code (same machine, same iteration count):
// BenchmarkRunTenSeparateChainedSteps 5750 ns/op, 13080 B/op, 65
// allocs/op (within noise of the pre-phase-38 baseline, since this
// phase touches no non-looped Sub path); BenchmarkRunTenIterationLoopStep
// 7112 ns/op, 15360 B/op, 152 allocs/op — roughly 1.24x the ns/op and
// 2.3x the allocs/op of the ten-separate-steps shape on this phase's
// own code, from the per-iteration LoopState construction and context
// injection the ten-separate-steps shape does not pay.
//
// See loop_test.go for the shared loopMachine and loopChild fixtures
// and why the child's final status alternates between statusA and
// statusB instead of self-looping.

import (
	"context"
	"strconv"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// benchTenSeparateChainedSteps builds ten separate, non-looped chained
// steps in a linear chain, each running the same one-step child
// workflow, targeting ten distinct statuses. Each child's own
// internal run always starts at m.Initial(), so every child needs its
// own statusStart-to-statuses[i] row, distinct from the outer chain's
// statuses[i-1]-to-statuses[i] row the parent step's own fireFromChild
// fires.
func benchTenSeparateChainedSteps(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	statuses := make([]machine.Status, 11)
	statuses[0] = statusStart
	for i := 1; i <= 10; i++ {
		statuses[i] = machine.Status("s" + strconv.Itoa(i))
	}
	var steps []flow.Step
	var trans []machine.Transition
	for i := 1; i <= 10; i++ {
		child, err := flow.New([]flow.Step{{ID: "c", To: string(statuses[i])}}, nil)
		if err != nil {
			tb.Fatalf("child flow.New: %v", err)
		}
		id := "step" + strconv.Itoa(i)
		var needs []string
		if i > 1 {
			needs = []string{"step" + strconv.Itoa(i-1)}
		}
		steps = append(steps, flow.Step{ID: id, Needs: needs, Sub: child})
		trans = append(trans, machine.Transition{
			From: statusStart, To: statuses[i], Trigger: machine.Trigger("child" + strconv.Itoa(i)),
		})
		if i > 1 {
			trans = append(trans, machine.Transition{
				From: statuses[i-1], To: statuses[i], Trigger: machine.Trigger("outer" + strconv.Itoa(i)),
			})
		}
	}
	d, err := flow.New(steps, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart, trans...)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// benchTenIterationLoopStep builds a single looped step whose Guard
// always returns true until Max, capping the loop at ten iterations,
// running the same child workflow the ten-separate-steps baseline
// runs once per step.
func benchTenIterationLoopStep(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	var parity int32
	guard := func(ctx context.Context) (bool, error) { return true, nil }
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(tb, &parity), Loop: &flow.LoopPolicy{Guard: guard, Max: 10}},
	}, nil)
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(tb)
	return d, m
}

// BenchmarkRunTenSeparateChainedSteps measures the ten-separate-steps
// baseline this benchmark compares against, on the current code, so
// the two numbers come from one benchmark run.
func BenchmarkRunTenSeparateChainedSteps(b *testing.B) {
	d, m := benchTenSeparateChainedSteps(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// BenchmarkRunTenIterationLoopStep measures a single looped step
// running ten iterations of the same child workflow the baseline
// runs across ten separate steps. The loop path's per-iteration
// LoopState construction and context injection vary the allocation
// count, so this benchmark reports the ns/op and allocs/op ratio
// against BenchmarkRunTenSeparateChainedSteps instead of asserting a
// fixed allocation budget.
func BenchmarkRunTenIterationLoopStep(b *testing.B) {
	d, m := benchTenIterationLoopStep(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}
