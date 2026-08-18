package scheduler_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

// TestConcurrentAddRemoveDuringRun runs many goroutines calling Add,
// Remove, and a running Run concurrently on one Scheduler, under
// go test -race. Asserts no data race and no panic.
func TestConcurrentAddRemoveDuringRun(t *testing.T) {
	s := scheduler.New()
	noop := func(ctx context.Context) error { return nil }

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- s.Run(ctx, nil) }()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(3 * n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("job-%03d", i)
			_ = s.Add(id, scheduler.Every(5*time.Millisecond), noop)
		}()
	}
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("job-%03d", i)
			runtime.Gosched()
			s.Remove(id)
		}()
	}
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("at-%03d", i)
			_ = s.Add(id, scheduler.At(time.Now().Add(time.Duration(i)*time.Millisecond)), noop)
		}()
	}
	wg.Wait()

	select {
	case err := <-runDone:
		if err == nil {
			t.Fatal("Run error = nil, want context.DeadlineExceeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its own timeout")
	}
}
