package machine_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// buildTenTransitionTable creates ten distinct valid transitions from
// the initial status. Each row gets its own trigger so the table is
// unambiguous.
func buildTenTransitionTable() *machine.Definition {
	initial := machine.Status("s0")
	ts := make([]machine.Transition, 10)
	for i := 0; i < 10; i++ {
		ts[i] = machine.Transition{
			From:    machine.Status("s0"),
			To:      machine.Status("s1"),
			Trigger: machine.Trigger(string(rune('a' + i))),
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
// Measured: ~435 ns/op, 0 B/op, 0 allocs/op after the ambiguity check.
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

// TestAllowedRowsAllocBudget guards the allocation floor for the row
// accessors. Both accessors use two passes: a count pass and a fill
// pass with one exact-size make. The budget is one allocation for a
// non-empty result and zero for an empty result. Triggers may
// stack-allocate when the result escapes analysis; the budget is
// the non-escaping floor.
func TestAllowedRowsAllocBudget(t *testing.T) {
	d, err := machine.New(
		"idle",
		machine.Transition{From: "idle", To: "running", Trigger: "start"},
		machine.Transition{From: "running", To: "done", Trigger: "finish"},
		machine.Transition{From: "running", To: "idle", Trigger: "cancel"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	tests := []struct {
		name  string
		fn    func(d machine.Definition)
		limit int
	}{
		{"transitions from running (2 rows)", func(d machine.Definition) {
			d.AllowedTransitions("running")
		}, 1},
		{"transitions from absent (0 rows)", func(d machine.Definition) {
			d.AllowedTransitions("absent")
		}, 0},
		{"triggers from running (2 triggers)", func(d machine.Definition) {
			d.AllowedTriggers("running")
		}, 1},
		{"triggers from absent (0 triggers)", func(d machine.Definition) {
			d.AllowedTriggers("absent")
		}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			alloc := testing.AllocsPerRun(1000, func() {
				tc.fn(*d)
			})
			if int(alloc) > tc.limit {
				t.Fatalf("allocated %v times per call; budget is %d", alloc, tc.limit)
			}
		})
	}
}
