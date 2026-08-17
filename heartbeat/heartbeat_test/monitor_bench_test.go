package heartbeat_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
)

// buildThousandIDMonitor creates a Monitor with one thousand tracked
// ids, half alive and half past the timeout at the returned now.
func buildThousandIDMonitor() (*heartbeat.Monitor, time.Time) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := time.Minute
	m, err := heartbeat.New(timeout)
	if err != nil {
		panic("buildThousandIDMonitor: " + err.Error())
	}
	now := base.Add(90 * time.Second)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("id-%04d", i)
		at := base
		if i%2 == 1 {
			at = now.Add(-time.Second)
		}
		if err := m.Beat(id, at); err != nil {
			panic("buildThousandIDMonitor: " + err.Error())
		}
	}
	return m, now
}

// BenchmarkBeatThousandIDs benchmarks Beat against a Monitor already
// holding one thousand tracked ids.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~18 ns/op, 0 B/op, 0 allocs/op (map key already exists).
func BenchmarkBeatThousandIDs(b *testing.B) {
	m, now := buildThousandIDMonitor()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.Beat("id-0000", now); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDeadThousandIDs benchmarks Dead over one thousand tracked
// ids, half of them past the timeout.
// Baseline (empty implementation): 0 ns/op, 0 allocs.
// Measured: ~39800 ns/op, one allocation for the result slice.
func BenchmarkDeadThousandIDs(b *testing.B) {
	m, now := buildThousandIDMonitor()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := m.Dead(now); len(got) != 500 {
			b.Fatalf("Dead(now) returned %d ids, want 500", len(got))
		}
	}
}

// TestDeadAllocBudget guards the allocation floor for Dead's sorted
// copy over one thousand tracked ids. The measured baseline is one
// allocation, for the result slice's backing array; sort.Strings
// allocates nothing extra for a []string of this size. The budget
// allows one extra allocation above the baseline to absorb a small,
// legitimate change in the standard library without masking a real
// regression.
func TestDeadAllocBudget(t *testing.T) {
	m, now := buildThousandIDMonitor()
	alloc := testing.AllocsPerRun(100, func() {
		if got := m.Dead(now); len(got) != 500 {
			t.Fatalf("Dead(now) returned %d ids, want 500", len(got))
		}
	})
	if alloc > 2 {
		t.Fatalf("Dead allocated %v times per call; budget is 2", alloc)
	}
}
