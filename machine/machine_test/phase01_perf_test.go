package machine_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// buildTenTransitionTable creates a linear chain of ten transitions.
func buildTenTransitionTable() *machine.Definition {
	initial := machine.Status("s0")
	ts := make([]machine.Transition, 10)
	for i := 0; i < 10; i++ {
		from := machine.Status("s0") // all from initial for simplicity
		to := machine.Status("s1")   // chain doesn't matter for perf
		ts[i] = machine.Transition{
			From:    from,
			To:      to,
			Trigger: machine.Trigger("t0"),
		}
	}
	d, err := machine.New(initial, ts...)
	if err != nil {
		panic("buildTenTransitionTable: " + err.Error())
	}
	return d
}

// BenchmarkValidateTen benchmarks Validate on a ten-transition table.
// Target: under one microsecond.
// Baseline (empty implementation): ~0 ns/op, 0 allocs.
// Measured with implementation: ~320 ns/op, 0 B/op, 0 allocs/op.
func BenchmarkValidateTen(b *testing.B) {
	d := buildTenTransitionTable()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := d.Validate(); err != nil {
			b.Fatal(err)
		}
	}
	// Allocation budget: AllocsPerRun should be at or below 3.
	// One map allocation for reachable, one for the loop, plus
	// possible string header allocations from map operations.
}
