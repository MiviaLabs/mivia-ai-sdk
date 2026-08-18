package scheduler_test

import (
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// TestEveryNext covers Every(d).Next(t) returning t.Add(d) for
// several d values.
func TestEveryNext(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		d    time.Duration
	}{
		{"one second", time.Second},
		{"one minute", time.Minute},
		{"one hour", time.Hour},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched := scheduler.Every(tc.d)
			want := base.Add(tc.d)
			if got := sched.Next(base); !got.Equal(want) {
				t.Fatalf("Every(%v).Next(base) = %v, want %v", tc.d, got, want)
			}
		})
	}
}

// TestEveryNonPositiveNeverFires covers Every's non-positive-duration
// path: this build returns an inert Schedule instead of panicking
// (see schedule.go's Every doc comment for the rationale), so a zero
// or negative d must yield a Schedule whose Next always reports the
// zero time.Time, at every after value.
func TestEveryNonPositiveNeverFires(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		d    time.Duration
	}{
		{"zero", 0},
		{"negative", -time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sched := scheduler.Every(tc.d)
			if got := sched.Next(base); !got.IsZero() {
				t.Fatalf("Every(%v).Next(base) = %v, want zero time", tc.d, got)
			}
			if got := sched.Next(time.Time{}); !got.IsZero() {
				t.Fatalf("Every(%v).Next(zero) = %v, want zero time", tc.d, got)
			}
		})
	}
}

// TestAtNext covers At's earliest-after selection, exhaustion, and
// out-of-order sorting of a defensive copy.
func TestAtNext(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := base.Add(time.Hour)
	t2 := base.Add(2 * time.Hour)
	t3 := base.Add(3 * time.Hour)

	t.Run("earliest entry after t", func(t *testing.T) {
		sched := scheduler.At(t3, t1, t2)
		if got := sched.Next(base); !got.Equal(t1) {
			t.Fatalf("At(...).Next(base) = %v, want %v", got, t1)
		}
	})

	t.Run("skips entries at or before after", func(t *testing.T) {
		sched := scheduler.At(t1, t2, t3)
		if got := sched.Next(t1); !got.Equal(t2) {
			t.Fatalf("At(...).Next(t1) = %v, want %v", got, t2)
		}
	})

	t.Run("zero time once exhausted", func(t *testing.T) {
		sched := scheduler.At(t1, t2)
		if got := sched.Next(t2); !got.IsZero() {
			t.Fatalf("At(...).Next(t2) = %v, want zero time", got)
		}
	})

	t.Run("no arguments always returns zero time", func(t *testing.T) {
		sched := scheduler.At()
		if got := sched.Next(base); !got.IsZero() {
			t.Fatalf("At().Next(base) = %v, want zero time", got)
		}
		if got := sched.Next(t3); !got.IsZero() {
			t.Fatalf("At().Next(t3) = %v, want zero time", got)
		}
	})

	t.Run("caller mutation of input slice does not change schedule", func(t *testing.T) {
		times := []time.Time{t1, t2, t3}
		sched := scheduler.At(times...)
		times[0] = t3
		times[1] = t3
		times[2] = t3
		if got := sched.Next(base); !got.Equal(t1) {
			t.Fatalf("At(...).Next(base) = %v after mutation, want %v (copy must not alias)", got, t1)
		}
	})
}
