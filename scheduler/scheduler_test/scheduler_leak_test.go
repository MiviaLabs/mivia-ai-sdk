package scheduler_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// TestRunLeavesNoGoroutineLeak records runtime.NumGoroutine before Run
// starts, runs Run with several Every jobs and a short-lived ctx, and
// asserts the goroutine count returns within a small margin of the
// starting count shortly after Run returns. It retries the check a
// few times to absorb the Go runtime's own scheduling noise.
func TestRunLeavesNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	s := scheduler.New()
	for i := 0; i < 5; i++ {
		id := "job-" + string(rune('a'+i))
		job := func(ctx context.Context) error { return nil }
		if err := s.Add(id, scheduler.Every(2*time.Millisecond), job); err != nil {
			t.Fatalf("Add(%s) error = %v, want nil", id, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx, nil)

	const margin = 5
	var after int
	for attempt := 0; attempt < 20; attempt++ {
		runtime.GC()
		after = runtime.NumGoroutine()
		if after <= before+margin {
			return
		}
		<-time.After(10 * time.Millisecond)
	}
	t.Fatalf("goroutine count after Run = %d, before = %d, want within %d", after, before, margin)
}
