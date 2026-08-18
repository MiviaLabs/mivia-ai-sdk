package scheduler_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// recorder collects Job invocations behind a mutex, safe for
// concurrent use across the goroutines Run spawns.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) record(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
}

func (r *recorder) count(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c == id {
			n++
		}
	}
	return n
}

// TestRunFiresRepeatedlyThenCancels covers a short Every interval
// firing at least twice within a bounded window, then an explicit
// cancel returning context.Canceled.
func TestRunFiresRepeatedlyThenCancels(t *testing.T) {
	s := scheduler.New()
	rec := &recorder{}
	job := func(ctx context.Context) error {
		rec.record("tick")
		return nil
	}
	if err := s.Add("tick", scheduler.Every(10*time.Millisecond), job); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	deadline := time.After(2 * time.Second)
	for rec.count("tick") < 2 {
		select {
		case <-deadline:
			t.Fatalf("tick fired %d times within window, want at least 2", rec.count("tick"))
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestRunFailingJobDoesNotStopOthers covers a failing Job leaving a
// second, separately added Job free to keep firing on its own
// schedule.
func TestRunFailingJobDoesNotStopOthers(t *testing.T) {
	s := scheduler.New()
	rec := &recorder{}
	failing := func(ctx context.Context) error {
		rec.record("failing")
		return errors.New("boom")
	}
	ok := func(ctx context.Context) error {
		rec.record("ok")
		return nil
	}
	if err := s.Add("failing", scheduler.Every(10*time.Millisecond), failing); err != nil {
		t.Fatalf("Add(failing) error = %v, want nil", err)
	}
	if err := s.Add("ok", scheduler.Every(10*time.Millisecond), ok); err != nil {
		t.Fatalf("Add(ok) error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	deadline := time.After(2 * time.Second)
	for rec.count("ok") < 2 || rec.count("failing") < 2 {
		select {
		case <-deadline:
			t.Fatalf("ok=%d failing=%d within window, want both at least 2", rec.count("ok"), rec.count("failing"))
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-done
}

// TestRunEmitsJobFailedEvent covers a failing Job, with a subscribed
// bus, emitting JobFailedEvent with Data naming the job's id.
func TestRunEmitsJobFailedEvent(t *testing.T) {
	s := scheduler.New()
	bus := events.New()
	var mu sync.Mutex
	var seen []string
	if err := bus.Subscribe(scheduler.JobFailedEvent, func(ctx context.Context, e events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, e.Data)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe error = %v, want nil", err)
	}

	failing := func(ctx context.Context) error { return errors.New("boom") }
	if err := s.Add("failing-job", scheduler.Every(10*time.Millisecond), failing); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := s.Run(ctx, bus)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no JobFailedEvent observed, want at least one")
	}
	for _, data := range seen {
		if !strings.Contains(data, "failing-job") {
			t.Fatalf("JobFailedEvent.Data = %q, want it to name failing-job", data)
		}
	}
}

// TestRunNilBusFailingJobNoPanic covers a failing Job with a nil bus:
// no panic, no emit, and later occurrences of that job still fire.
func TestRunNilBusFailingJobNoPanic(t *testing.T) {
	s := scheduler.New()
	rec := &recorder{}
	failing := func(ctx context.Context) error {
		rec.record("failing")
		return errors.New("boom")
	}
	if err := s.Add("failing", scheduler.Every(10*time.Millisecond), failing); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	deadline := time.After(2 * time.Second)
	for rec.count("failing") < 2 {
		select {
		case <-deadline:
			t.Fatalf("failing fired %d times within window, want at least 2", rec.count("failing"))
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-done
}

// TestRunAddWakesBlockedSleep covers Add called after Run has
// started, targeting an At time earlier than any currently scheduled
// entry, still firing at its own time, proving the wake-on-Add
// signal reaches a blocked sleep.
func TestRunAddWakesBlockedSleep(t *testing.T) {
	s := scheduler.New()
	rec := &recorder{}
	slow := func(ctx context.Context) error {
		rec.record("slow")
		return nil
	}
	if err := s.Add("slow", scheduler.Every(5*time.Second), slow); err != nil {
		t.Fatalf("Add(slow) error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	// slow's five-second interval means Run is still blocked in its
	// long sleep whenever the wake-testing Add below lands: either it
	// lands before Run's first sleepTimer call, in which case Run
	// reads the new entry on its first pass, or after, in which case
	// the wake channel interrupts the five-second sleep. Either path
	// exercises the wake path deterministically; no fixed delay needed.
	fast := func(ctx context.Context) error {
		rec.record("fast")
		return nil
	}
	fireAt := time.Now().Add(10 * time.Millisecond)
	if err := s.Add("fast", scheduler.At(fireAt), fast); err != nil {
		t.Fatalf("Add(fast) error = %v, want nil", err)
	}

	deadline := time.After(2 * time.Second)
	for rec.count("fast") < 1 {
		select {
		case <-deadline:
			t.Fatal("fast job never fired; Add did not wake the blocked sleep in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	<-done
}

// TestRunAtScheduleFullyInPastNeverFires covers an At schedule with
// every entry already in the past: Run cancels cleanly with zero
// recorded calls for that entry.
func TestRunAtScheduleFullyInPastNeverFires(t *testing.T) {
	s := scheduler.New()
	rec := &recorder{}
	job := func(ctx context.Context) error {
		rec.record("past")
		return nil
	}
	past := time.Now().Add(-time.Hour)
	if err := s.Add("past", scheduler.At(past), job); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := s.Run(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}
	if got := rec.count("past"); got != 0 {
		t.Fatalf("past fired %d times, want 0", got)
	}
}

// TestRunRemoveMidRunStopsFutureFirings covers Remove called while
// Run is blocked: the removed job's future firings stop.
func TestRunRemoveMidRunStopsFutureFirings(t *testing.T) {
	s := scheduler.New()
	rec := &recorder{}
	job := func(ctx context.Context) error {
		rec.record("job")
		return nil
	}
	if err := s.Add("job", scheduler.Every(15*time.Millisecond), job); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	deadline := time.After(2 * time.Second)
	for rec.count("job") < 1 {
		select {
		case <-deadline:
			t.Fatal("job never fired before Remove")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if got := s.Remove("job"); !got {
		t.Fatal("Remove(job) = false, want true")
	}
	countAtRemove := rec.count("job")

	// Poll across several of the job's own 15ms intervals: if Remove
	// had not taken effect, the count would grow during this window.
	settleDeadline := time.After(150 * time.Millisecond)
poll:
	for {
		select {
		case <-settleDeadline:
			break poll
		case <-time.After(15 * time.Millisecond):
			if got := rec.count("job"); got != countAtRemove {
				t.Fatalf("job fired %d more times after Remove, want 0 more", got-countAtRemove)
			}
		}
	}

	cancel()
	<-done

	if got := rec.count("job"); got != countAtRemove {
		t.Fatalf("job fired %d more times after Remove, want 0 more", got-countAtRemove)
	}
}

// TestRunDeadlineExceeded covers Run started under a real
// context.WithTimeout returning context.DeadlineExceeded once the
// deadline passes, distinct from the explicit-cancel case above,
// which asserts context.Canceled.
func TestRunDeadlineExceeded(t *testing.T) {
	s := scheduler.New()
	noop := func(ctx context.Context) error { return nil }
	if err := s.Add("job", scheduler.Every(time.Second), noop); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Run(ctx, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}
}

// TestRunEmptySchedulerReturnsOnCancel proves Run with no registered
// entries still respects ctx and returns without blocking forever.
func TestRunEmptySchedulerReturnsOnCancel(t *testing.T) {
	s := scheduler.New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, nil) }()

	// No readiness signal is needed: cancel is safe to call the
	// instant Run's goroutine is scheduled, since Run's first select
	// reads ctx.Done() with no other ready case for an empty job set.
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel with no entries")
	}
}

// TestJobFailedEventDataFormat pins the exact Data format Run emits.
func TestJobFailedEventDataFormat(t *testing.T) {
	s := scheduler.New()
	bus := events.New()
	received := make(chan string, 1)
	if err := bus.Subscribe(scheduler.JobFailedEvent, func(ctx context.Context, e events.Event) error {
		select {
		case received <- e.Data:
		default:
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe error = %v, want nil", err)
	}

	wantErr := errors.New("boom")
	failing := func(ctx context.Context) error { return wantErr }
	if err := s.Add("pinned-job", scheduler.Every(10*time.Millisecond), failing); err != nil {
		t.Fatalf("Add error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Run(ctx, bus) }()

	select {
	case data := <-received:
		want := fmt.Sprintf("job %s failed: %v", "pinned-job", wantErr)
		if data != want {
			t.Fatalf("JobFailedEvent.Data = %q, want %q", data, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no JobFailedEvent observed")
	}
	cancel()
}
