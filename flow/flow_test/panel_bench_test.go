package flow_test

// Fresh ten-step sequential baseline, measured before phase 6 code
// landed, on the phase 5 Run path (no panels): see
// BenchmarkRunTenStepsSequential. Phase 5's own benchmark measured a
// three-step chain at 217.4 ns/op, not comparable at ten steps.
//
// Measured baseline (AMD Ryzen 9 9900X, go test -bench, on the
// unmodified phase 5 Run and nextReady, before phase 6 code landed):
// 1465 ns/op, 1576 B/op, 23 allocs/op.
//
// Measured ten-step panel benchmark (one panel, ten members, one
// shared To), after phase 6 landed: 5712 ns/op, 7776 B/op, 51
// allocs/op.
//
// Ratio against the pre-phase-6 baseline: ns/op 3.90x, allocs/op
// 2.22x. Both ratios are report-only,
// per PHASES.md's amended perf-test contract for non-deterministic-
// overhead benchmarks. The panel path pays goroutine, channel, and
// WaitGroup overhead the sequential path never pays; the ns/op ratio
// tracks that fixed per-wave cost, not a per-step multiplier, since a
// wave of ten members still runs one Guard, one OnExit, and one
// OnEntry concurrently, the same count as a single sequential step.
// The allocs/op ratio is not asserted as a budget: goroutine and
// channel setup cost is not proportional to step count in a way a
// fixed multiplier over the sequential baseline can meaningfully gate.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// tenStepSequentialGraph builds a ten-step linear chain, run entirely
// on the phase 5 singleton Run path: no panel names any of these
// steps.
func tenStepSequentialGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	steps := make([]flow.Step, 10)
	trans := make([]machine.Transition, 10)
	prev := statusStart
	for i := 0; i < 10; i++ {
		to := machine.Status(statusName(i))
		id := stepName(i)
		needs := []string(nil)
		if i > 0 {
			needs = []string{stepName(i - 1)}
		}
		steps[i] = flow.Step{ID: id, Needs: needs, To: string(to)}
		trans[i] = machine.Transition{From: prev, To: to, Trigger: triggerGo}
		prev = to
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

// tenStepPanelGraph builds one panel of ten members, all sharing one
// To, run entirely through runWave.
func tenStepPanelGraph(tb testing.TB) (*flow.Definition, *machine.Definition) {
	tb.Helper()
	const to = machine.Status("panel-done")
	steps := make([]flow.Step, 10)
	panel := make(flow.Panel, 10)
	for i := 0; i < 10; i++ {
		id := stepName(i)
		steps[i] = flow.Step{ID: id, To: string(to)}
		panel[i] = id
	}
	d, err := flow.New(steps, []flow.Panel{panel})
	if err != nil {
		tb.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: to, Trigger: triggerGo},
	)
	if err != nil {
		tb.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// stepName returns the deterministic step ID for index i.
func stepName(i int) string {
	return "s" + string(rune('a'+i))
}

// statusName returns the deterministic status name for index i.
func statusName(i int) string {
	return "status" + string(rune('a'+i))
}

// BenchmarkRunTenStepsSequential measures Run on a ten-step linear
// chain, the phase 5 singleton path, with a no-op confirm. This is
// the baseline phase 6 compares the ten-step panel against.
func BenchmarkRunTenStepsSequential(b *testing.B) {
	d, m := tenStepSequentialGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// BenchmarkRunTenStepPanel measures Run on one panel of ten members
// sharing one To, run through runWave in a single wave. Compare its
// ns/op and allocs/op against BenchmarkRunTenStepsSequential; see the
// ratio recorded in this file's leading comment.
func BenchmarkRunTenStepPanel(b *testing.B) {
	d, m := tenStepPanelGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}
