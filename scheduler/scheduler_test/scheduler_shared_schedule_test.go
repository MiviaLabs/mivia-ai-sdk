package scheduler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// statefulSchedule is a Schedule with mutable state, the kind of
// custom implementation a caller might reuse across Add calls. calls
// is atomic only so this test can observe it without racing Next
// itself; Next's own body is not internally synchronized, because it
// relies on the scheduler package's guarantee that only Run's own
// loop goroutine ever calls Next.
type statefulSchedule struct{ calls int32 }

// Next records a call and returns after plus a short fixed step.
func (s *statefulSchedule) Next(after time.Time) time.Time {
	atomic.AddInt32(&s.calls, 1)
	return after.Add(5 * time.Millisecond)
}

// TestSharedScheduleInstanceNoRace proves Add never calls Next
// itself: a stateful Schedule value shared across two Add calls,
// where the second Add lands only after Run's loop goroutine has
// already called Next once on its own, sees every Next call funneled
// through that one goroutine. Run under go test -race must report no
// race.
func TestSharedScheduleInstanceNoRace(t *testing.T) {
	sh := &statefulSchedule{}
	s := scheduler.New()
	noop := func(ctx context.Context) error { return nil }
	if err := s.Add("a", sh, noop); err != nil {
		t.Fatalf("Add(a) error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&sh.calls) < 1 {
		select {
		case <-deadline:
			t.Fatal("Run never called Next on the shared Schedule")
		case <-time.After(2 * time.Millisecond):
		}
	}

	if err := s.Add("b", sh, noop); err != nil {
		t.Fatalf("Add(b) error = %v, want nil", err)
	}
	<-done
}
