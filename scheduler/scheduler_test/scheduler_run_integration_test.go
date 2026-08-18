package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// TestRunIntegrationTwoEveryOneAtOverRealBus runs a Scheduler with two
// Every jobs and one At job against a real events.Bus, end to end, for
// a bounded wall-clock window. It asserts the At job fires exactly
// once, the two Every jobs each fire more than once, and a subscribed
// JobFailedEvent handler observes exactly the failing job's own
// failures, with no cross-job event leakage.
func TestRunIntegrationTwoEveryOneAtOverRealBus(t *testing.T) {
	s := scheduler.New()
	bus := events.New()

	var mu sync.Mutex
	counts := map[string]int{}
	failedIDs := map[string]int{}

	record := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		counts[id]++
	}

	if err := bus.Subscribe(scheduler.JobFailedEvent, func(ctx context.Context, e events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case containsID(e.Data, "flaky"):
			failedIDs["flaky"]++
		default:
			failedIDs["other"]++
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe error = %v, want nil", err)
	}

	steady := func(ctx context.Context) error {
		record("steady")
		return nil
	}
	flaky := func(ctx context.Context) error {
		record("flaky")
		return errors.New("flaky failure")
	}
	oneShot := func(ctx context.Context) error {
		record("one-shot")
		return nil
	}

	if err := s.Add("steady", scheduler.Every(10*time.Millisecond), steady); err != nil {
		t.Fatalf("Add(steady) error = %v, want nil", err)
	}
	if err := s.Add("flaky", scheduler.Every(10*time.Millisecond), flaky); err != nil {
		t.Fatalf("Add(flaky) error = %v, want nil", err)
	}
	if err := s.Add("one-shot", scheduler.At(time.Now().Add(15*time.Millisecond)), oneShot); err != nil {
		t.Fatalf("Add(one-shot) error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	err := s.Run(ctx, bus)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if counts["steady"] < 2 {
		t.Fatalf("steady fired %d times, want at least 2", counts["steady"])
	}
	if counts["flaky"] < 2 {
		t.Fatalf("flaky fired %d times, want at least 2", counts["flaky"])
	}
	if counts["one-shot"] != 1 {
		t.Fatalf("one-shot fired %d times, want exactly 1", counts["one-shot"])
	}
	if failedIDs["flaky"] != counts["flaky"] {
		t.Fatalf("JobFailedEvent observed %d times for flaky, want %d (one per failure)", failedIDs["flaky"], counts["flaky"])
	}
	if failedIDs["other"] != 0 {
		t.Fatalf("JobFailedEvent leaked %d events to a non-flaky id, want 0", failedIDs["other"])
	}
}

// containsID reports whether data names id, a minimal substring check
// against Run's pinned "job %s failed: %v" Data format.
func containsID(data, id string) bool {
	for i := 0; i+len(id) <= len(data); i++ {
		if data[i:i+len(id)] == id {
			return true
		}
	}
	return false
}
