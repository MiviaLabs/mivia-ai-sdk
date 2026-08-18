package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// countingSchedule wraps an Every-style fixed interval and counts
// every Next call with an atomic counter, thread-safe against the
// single Run loop goroutine that calls it.
type countingSchedule struct {
	d     time.Duration
	calls int64
}

// Next records the call, then returns after.Add(d), matching
// everySchedule's own rule.
func (c *countingSchedule) Next(after time.Time) time.Time {
	atomic.AddInt64(&c.calls, 1)
	return after.Add(c.d)
}

// TestRunComputePendingSkipsAlreadyComputedEntries pins computePending's
// pending guard (run.go): once an entry's first Next call has run,
// computePending must never call Next again for that entry. Every
// further Next call comes only from fireDue, one per fire, so the
// total Next call count across a bounded run must equal fires+1 (the
// one pending computation from Add, plus one fireDue call per fire).
// Removing the "if !e.pending { continue }" guard calls Next on every
// loop iteration regardless of pending, roughly doubling this count;
// this test fails under that mutation.
func TestRunComputePendingSkipsAlreadyComputedEntries(t *testing.T) {
	sched := &countingSchedule{d: 5 * time.Millisecond}
	var fires int64
	job := func(ctx context.Context) error {
		atomic.AddInt64(&fires, 1)
		return nil
	}
	s := scheduler.New()
	if err := s.Add("counted", sched, job); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	deadline := time.After(2 * time.Second)
	for atomic.LoadInt64(&fires) < 10 {
		select {
		case <-deadline:
			t.Fatalf("counted fired %d times within window, want at least 10", atomic.LoadInt64(&fires))
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	// No further Next calls can happen once Run has returned: Run's
	// own defer waits for every spawned job goroutine, and no job
	// goroutine calls Next, so the counts below are final.
	gotCalls := atomic.LoadInt64(&sched.calls)
	gotFires := atomic.LoadInt64(&fires)
	wantCalls := gotFires + 1
	if gotCalls != wantCalls {
		t.Fatalf("Next called %d times for %d fires, want exactly %d (fires+1)", gotCalls, gotFires, wantCalls)
	}
}

// TestRunFireDueOnlyFiresEntriesActuallyDue pins fireDue's due guard
// (run.go's "if e.next.IsZero() || e.next.After(now) { continue }"):
// an entry not yet due must not fire alongside a different entry that
// is due. A fast Every job and a much slower Every job run together;
// the slow job's fire count over a short, bounded window gets a tight
// upper bound, not just a floor. Mutating the due guard to treat
// every entry as due fires the slow job on every fast tick, blowing
// past the bound; this test fails under that mutation.
func TestRunFireDueOnlyFiresEntriesActuallyDue(t *testing.T) {
	s := scheduler.New()
	var fastFires, slowFires int64
	fast := func(ctx context.Context) error {
		atomic.AddInt64(&fastFires, 1)
		return nil
	}
	slow := func(ctx context.Context) error {
		atomic.AddInt64(&slowFires, 1)
		return nil
	}
	if err := s.Add("fast", scheduler.Every(10*time.Millisecond), fast); err != nil {
		t.Fatalf("Add(fast) error = %v, want nil", err)
	}
	if err := s.Add("slow", scheduler.Every(60*time.Millisecond), slow); err != nil {
		t.Fatalf("Add(slow) error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	err := s.Run(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}

	gotFast := atomic.LoadInt64(&fastFires)
	gotSlow := atomic.LoadInt64(&slowFires)
	if gotFast < 2 {
		t.Fatalf("fast fired %d times, want at least 2", gotFast)
	}
	if gotSlow < 1 {
		t.Fatalf("slow fired %d times, want at least 1", gotSlow)
	}
	// 250ms / 60ms is a little over 4 due firings; allow a small
	// margin for scheduling jitter but stay far below the ~25 fires
	// the always-due mutation produces by tracking fast's own rate.
	const wantSlowMax = 6
	if gotSlow > wantSlowMax {
		t.Fatalf("slow fired %d times in 250ms at a 60ms interval, want at most %d", gotSlow, wantSlowMax)
	}
}
