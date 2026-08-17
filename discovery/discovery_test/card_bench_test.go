package discovery_test

import (
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
)

// buildManyCapabilitiesCard returns a Card with one hundred
// capabilities. The sought need is the last entry, the worst case for
// a linear scan.
func buildManyCapabilitiesCard() discovery.Card {
	caps := make([]string, 100)
	for i := range caps {
		caps[i] = fmt.Sprintf("capability-%03d", i)
	}
	return discovery.Card{Name: "Agent A", Capabilities: caps}
}

// BenchmarkMatch benchmarks Match over a card of one hundred
// capabilities. Target: under one microsecond per call. Measured
// baseline: 409 ns/op, 0 allocs/op, well under budget.
func BenchmarkMatch(b *testing.B) {
	card := buildManyCapabilitiesCard()
	need := "capability-099"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := card.Match(need); !ok {
			b.Fatal("Match() = false, want true")
		}
	}
}

// TestMatchAllocBudget guards the allocation floor for Match.
// AllocsPerRun must stay at zero: EqualFold and a range loop over a
// string slice allocate nothing.
func TestMatchAllocBudget(t *testing.T) {
	card := buildManyCapabilitiesCard()
	need := "capability-099"
	alloc := testing.AllocsPerRun(1000, func() {
		if _, ok := card.Match(need); !ok {
			t.Fatal("Match() = false, want true")
		}
	})
	const budget = 0
	if alloc > budget {
		t.Fatalf("Match allocated %v times per call; budget is %d", alloc, budget)
	}
}
