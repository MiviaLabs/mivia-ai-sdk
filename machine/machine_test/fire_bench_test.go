package machine_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// buildTenFireTable creates ten transitions from s0 on distinct triggers.
func buildTenFireTable() *machine.Definition {
	ts := make([]machine.Transition, 10)
	for i := 0; i < 10; i++ {
		ts[i] = machine.Transition{
			From:    machine.Status("s0"),
			To:      machine.Status("s1"),
			Trigger: machine.Trigger(string(rune('a' + i))),
		}
	}
	d, err := machine.New(machine.Status("s0"), ts...)
	if err != nil {
		panic("buildTenFireTable: " + err.Error())
	}
	return d
}

// BenchmarkFireTen benchmarks Fire on a ten-row table.
// Target: under one microsecond.
// Baseline (empty implementation): no benchmark; Fire did not exist.
// Measured with implementation: ~43 ns/op, 32 B/op, 1 allocs/op.
// The single allocation is the output record escaping so that an
// Action can write its Output field.
func BenchmarkFireTen(b *testing.B) {
	d := buildTenFireTable()
	ctx := context.Background()
	in := machine.InOut{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := d.Fire(ctx, "s0", machine.Trigger("j"), in); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFireAllocBudget guards the allocation floor for Fire.
// AllocsPerRun must stay at or below one; the single allowed
// allocation is the escaped output record. A regression that allocates
// more than once inside the hot dispatch loop fails here.
func TestFireAllocBudget(t *testing.T) {
	d := buildTenFireTable()
	ctx := context.Background()
	in := machine.InOut{}
	alloc := testing.AllocsPerRun(1000, func() {
		if _, _, err := d.Fire(ctx, "s0", machine.Trigger("j"), in); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > 1 {
		t.Fatalf("Fire allocated %v times per call; budget is 1", alloc)
	}
}
