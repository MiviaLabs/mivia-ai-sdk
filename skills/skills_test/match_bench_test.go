package skills_test

import (
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

// buildManySkillsRegistry returns a Registry of one hundred skills,
// five triggers each. The sought query is a trigger on the last
// registered skill, the worst case for a linear scan.
func buildManySkillsRegistry() (*skills.Registry, string) {
	r := skills.New()
	const n = 100
	var wantQuery string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("skill-%03d", i)
		triggers := make([]string, 5)
		for j := range triggers {
			triggers[j] = fmt.Sprintf("skill-%03d-trigger-%d", i, j)
		}
		if i == n-1 {
			wantQuery = triggers[2]
		}
		if err := r.Add(skills.Skill{Name: name, Instructions: "x", Triggers: triggers}); err != nil {
			panic(err)
		}
	}
	return r, wantQuery
}

// BenchmarkMatch benchmarks Match over a registry of one hundred
// skills, five triggers each: 500 EqualFold comparisons in the worst
// case. Measured baseline: 2556 ns/op, 2 allocs/op.
func BenchmarkMatch(b *testing.B) {
	r, query := buildManySkillsRegistry()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := r.Match(query); len(got) != 1 {
			b.Fatalf("Match() = %v, want exactly one hit", got)
		}
	}
}

// TestMatchAllocBudget guards the allocation floor for Match.
// AllocsPerRun must stay at or below 2: one slice grow for the
// single-match result and one for the sort call's internal state.
func TestMatchAllocBudget(t *testing.T) {
	r, query := buildManySkillsRegistry()
	alloc := testing.AllocsPerRun(1000, func() {
		if got := r.Match(query); len(got) != 1 {
			t.Fatalf("Match() = %v, want exactly one hit", got)
		}
	})
	const budget = 2
	if alloc > budget {
		t.Fatalf("Match allocated %v times per call; budget is %d", alloc, budget)
	}
}
