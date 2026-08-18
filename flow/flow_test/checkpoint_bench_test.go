package flow_test

// Performance: onCheckpoint's cost against a nil hook, on the same
// flat four-step graph flatThreeGraph builds for the phase 7 chain
// benchmark in chain_bench_test.go.
//
// Measured on this phase's implementation (go1.26.0, linux/amd64,
// AMD Ryzen 9 9900X, go test -bench -benchmem -count=1):
// BenchmarkRunNilOnCheckpoint: 428.8 ns/op, 704 B/op, 10 allocs/op
// (the nil-hook path runs the same code the pre-phase-25 Run ran;
// this is the pre-phase baseline, unchanged by adding the parameter).
// BenchmarkRunWithOnCheckpoint: 637.9 ns/op, 864 B/op, 14 allocs/op
// (the hook itself is a no-op closure, isolating doneFrom's sort and
// the Checkpoint literal's own allocation cost). Ratio: allocs/op
// 1.4x, ns/op 1.5x. A benchmark may skip a fixed allocation budget
// when goroutine and closure overhead vary; this file reports the
// allocs/op ratio instead of asserting a fixed budget, per
// docs/plans/agents/PHASES.md.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// BenchmarkRunNilOnCheckpoint measures Run with a nil onCheckpoint.
func BenchmarkRunNilOnCheckpoint(b *testing.B) {
	d, m := flatThreeGraph(b)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// BenchmarkRunWithOnCheckpoint measures Run with a non-nil
// onCheckpoint that builds no state, isolating the hook's own cost.
func BenchmarkRunWithOnCheckpoint(b *testing.B) {
	d, m := flatThreeGraph(b)
	ctx := context.Background()
	onCheckpoint := func(c flow.Checkpoint) {}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

// TestCheckpointHookAllocRatio reports the allocs/op ratio between a
// non-nil and a nil onCheckpoint, run once outside the benchmark
// harness. It asserts no fixed budget; goroutine and closure overhead
// vary across toolchains. See docs/plans/agents/PHASES.md.
func TestCheckpointHookAllocRatio(t *testing.T) {
	t.Parallel()
	d, m := flatThreeGraph(t)
	ctx := context.Background()

	nilHook := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
				b.Fatalf("Run: %v", err)
			}
		}
	})

	withHook := testing.Benchmark(func(b *testing.B) {
		onCheckpoint := func(c flow.Checkpoint) {}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, onCheckpoint); err != nil {
				b.Fatalf("Run: %v", err)
			}
		}
	})

	if nilHook.AllocsPerOp() == 0 {
		t.Skip("nil-hook baseline made zero allocations; ratio is undefined")
	}
	t.Logf("nil onCheckpoint: %d allocs/op; non-nil onCheckpoint: %d allocs/op",
		nilHook.AllocsPerOp(), withHook.AllocsPerOp())
}
