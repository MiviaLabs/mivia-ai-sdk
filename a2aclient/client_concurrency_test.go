package a2aclient

import (
	"context"
	"sync"
	"testing"
)

// TestConcurrentSendStatusResult runs Send, Status, and Result from
// multiple goroutines against one Client, proving the "safe for
// concurrent use" doc comment on Client. Run with `go test -race`.
func TestConcurrentSendStatusResult(t *testing.T) {
	msg := signedMessage(t)
	tr := &stubTransport{
		taskID: "task-concurrent",
		states: []State{
			StateSubmitted,
			StateWorking,
			StateCompleted,
		},
		result: mappedResult(t, msg),
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		t.Fatalf("newFromTransport: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			h, err := c.Send(ctx, msg)
			if err != nil {
				errs <- err
				return
			}
			var state State
			for j := 0; j < 64; j++ {
				state, err = c.Status(ctx, h)
				if err != nil {
					errs <- err
					return
				}
				if state == StateCompleted {
					break
				}
			}
			if state != StateCompleted {
				errs <- err
				return
			}
			if _, err := c.Result(ctx, h); err != nil {
				errs <- err
				return
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("goroutine returned unexpected error: %v", err)
		}
	}
}
