package machine_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// buildTenTransitionTable creates ten identical transitions from the
// initial status.
func buildTenTransitionTable() *machine.Definition {
	initial := machine.Status("s0")
	ts := make([]machine.Transition, 10)
	for i := 0; i < 10; i++ {
		ts[i] = machine.Transition{
			From:    machine.Status("s0"),
			To:      machine.Status("s1"),
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
}

// TestValidateAllocBudget guards the allocation floor for Validate.
// AllocsPerRun must stay at zero; a regression that allocates inside
// the hot loop fails here.
func TestValidateAllocBudget(t *testing.T) {
	d := buildTenTransitionTable()
	alloc := testing.AllocsPerRun(1000, func() {
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
	})
	if alloc != 0 {
		t.Fatalf("Validate allocated %v times per call; budget is 0", alloc)
	}
}
