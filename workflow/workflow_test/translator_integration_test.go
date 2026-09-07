// Package workflow_test also holds the cross-package integration cases
// for the envelope-to-events translator. These cases cross the
// envelope and events import edges the policy declares for workflow.
package workflow_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
)

// TestEmitMessageDeliveredCrossesEnvelopeAndEvents signs a real
// Message with a real envelope.Identity, then proves
// EmitMessageDelivered delivers exactly one event on a real bus.
func TestEmitMessageDeliveredCrossesEnvelopeAndEvents(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var count atomic.Int64
	if err := bus.Subscribe(workflow.MessageDeliveredEvent, func(context.Context, events.Event) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	m := signedMessage(t, "cross-msg-1")
	if err := workflow.EmitMessageDelivered(context.Background(), bus, m); err != nil {
		t.Fatalf("EmitMessageDelivered() unexpected error: %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// TestEmitMessageAckedCrossesEnvelopeAndEvents builds a real Ack
// with envelope.NewAck, then proves EmitMessageAcked delivers
// exactly one event on a real bus.
func TestEmitMessageAckedCrossesEnvelopeAndEvents(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var count atomic.Int64
	if err := bus.Subscribe(workflow.MessageAckedEvent, func(context.Context, events.Event) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	a := validAck(t)
	if err := workflow.EmitMessageAcked(context.Background(), bus, a); err != nil {
		t.Fatalf("EmitMessageAcked() unexpected error: %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// TestEmitThreadVerifiedCrossesEnvelopeAndEvents builds a real
// two-message thread, then proves EmitThreadVerified delivers
// exactly one event on a real bus.
func TestEmitThreadVerifiedCrossesEnvelopeAndEvents(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var count atomic.Int64
	if err := bus.Subscribe(workflow.ThreadVerifiedEvent, func(context.Context, events.Event) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	msgs := validThread()
	if err := workflow.EmitThreadVerified(context.Background(), bus, msgs); err != nil {
		t.Fatalf("EmitThreadVerified() unexpected error: %v", err)
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
}

// TestEmitConcurrentAllThreeKinds runs many goroutines against one
// shared bus, calling EmitMessageDelivered, EmitMessageAcked, and
// EmitThreadVerified concurrently. Atomic counters prove each call
// still delivers exactly once, with no data race. Run with
// -race to prove the translator adds no shared mutable state of its
// own; events.Bus already proves its own concurrency safety in its
// own test suite.
func TestEmitConcurrentAllThreeKinds(t *testing.T) {
	const goroutines = 30

	bus := events.New()
	var delivered, acked, verified atomic.Int64
	if err := bus.Subscribe(workflow.MessageDeliveredEvent, func(context.Context, events.Event) error {
		delivered.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	if err := bus.Subscribe(workflow.MessageAckedEvent, func(context.Context, events.Event) error {
		acked.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	if err := bus.Subscribe(workflow.ThreadVerifiedEvent, func(context.Context, events.Event) error {
		verified.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	ack := validAck(t)
	thread := validThread()
	ctx := context.Background()

	var wg sync.WaitGroup
	var errCount atomic.Int64
	start := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			<-start
			msg := signedMessage(t, messageIDFor(g))
			if err := workflow.EmitMessageDelivered(ctx, bus, msg); err != nil {
				errCount.Add(1)
			}
			if err := workflow.EmitMessageAcked(ctx, bus, ack); err != nil {
				errCount.Add(1)
			}
			if err := workflow.EmitThreadVerified(ctx, bus, thread); err != nil {
				errCount.Add(1)
			}
		}(g)
	}
	close(start)
	wg.Wait()

	if n := errCount.Load(); n != 0 {
		t.Fatalf("%d Emit calls returned an unexpected error", n)
	}
	if got := delivered.Load(); got != goroutines {
		t.Fatalf("delivered count = %d, want %d", got, goroutines)
	}
	if got := acked.Load(); got != goroutines {
		t.Fatalf("acked count = %d, want %d", got, goroutines)
	}
	if got := verified.Load(); got != goroutines {
		t.Fatalf("verified count = %d, want %d", got, goroutines)
	}
}

// messageIDFor builds a unique message ID for goroutine g, so every
// concurrently signed Message stays independently valid.
func messageIDFor(g int) string {
	return fmt.Sprintf("concurrent-msg-%d", g)
}
