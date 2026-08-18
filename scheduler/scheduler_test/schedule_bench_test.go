package scheduler_test

import (
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// buildFiftyEntryAt builds an At schedule with fifty ascending times,
// starting one hour after base.
func buildFiftyEntryAt(base time.Time) scheduler.Schedule {
	times := make([]time.Time, 0, 50)
	for i := 0; i < 50; i++ {
		times = append(times, base.Add(time.Duration(i+1)*time.Hour))
	}
	return scheduler.At(times...)
}

// BenchmarkEveryNext benchmarks Every(d).Next.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~2 ns/op, 0 B/op, 0 allocs/op (pure addition).
func BenchmarkEveryNext(b *testing.B) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched := scheduler.Every(time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sched.Next(base)
	}
}

// TestEveryNextAllocBudget guards the allocation floor for
// Every.Next. The measured baseline is zero allocations, since
// time.Time.Add performs no heap allocation. The budget allows one
// extra allocation above the baseline to absorb a small, legitimate
// runtime change without masking a real regression.
func TestEveryNextAllocBudget(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched := scheduler.Every(time.Minute)
	alloc := testing.AllocsPerRun(1000, func() {
		_ = sched.Next(base)
	})
	if alloc > 1 {
		t.Fatalf("Every.Next allocated %v times per call; budget is 1", alloc)
	}
}

// BenchmarkAtNextFiftyEntries benchmarks At(times...).Next against a
// fifty-entry times slice.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~40 ns/op, 0 B/op, 0 allocs/op (linear scan, no alloc).
func BenchmarkAtNextFiftyEntries(b *testing.B) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched := buildFiftyEntryAt(base)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sched.Next(base)
	}
}

// TestAtNextAllocBudget guards the allocation floor for At.Next over
// a fifty-entry schedule. The measured baseline is zero allocations,
// since Next only scans the already-sorted slice. The budget allows
// one extra allocation above the baseline to absorb a small,
// legitimate runtime change without masking a real regression.
func TestAtNextAllocBudget(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sched := buildFiftyEntryAt(base)
	alloc := testing.AllocsPerRun(1000, func() {
		_ = sched.Next(base)
	})
	if alloc > 1 {
		t.Fatalf("At.Next allocated %v times per call; budget is 1", alloc)
	}
}
