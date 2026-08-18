package a2aclient

import (
	"context"
	"testing"
)

// BenchmarkSendStatusResult runs a full send-status-result cycle
// against the recorded transcript. Target: under ten milliseconds per
// cycle on the reference machine; the measured baseline is close to
// 0.3ms. The allocation sources are the a2a.ToPart/FromPart calls and
// the per-call TaskHandle and State values; the measured baseline on
// the reference machine is 23 allocations per cycle. See
// BenchmarkAllocsSendStatusResult for the asserted budget.
func BenchmarkSendStatusResult(b *testing.B) {
	msg := signedMessage(b)
	tr := &stubTransport{
		taskID: "task-bench",
		states: []State{StateCompleted},
		result: mappedResult(b, msg),
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		b.Fatalf("newFromTransport: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}()

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h, err := c.Send(ctx, msg)
		if err != nil {
			b.Fatalf("Send: %v", err)
		}
		if _, err := c.Status(ctx, h); err != nil {
			b.Fatalf("Status: %v", err)
		}
		if _, err := c.Result(ctx, h); err != nil {
			b.Fatalf("Result: %v", err)
		}
	}
}

// BenchmarkAllocsSendStatusResult asserts the allocation budget for a
// full send-status-result cycle, using AllocsPerRun so a regression is
// a test failure, not only a comment. The measured baseline is 23
// allocs/op; the budget of 35 leaves a small margin while still
// catching a real regression of a few allocations.
func BenchmarkAllocsSendStatusResult(b *testing.B) {
	msg := signedMessage(b)
	tr := &stubTransport{
		taskID: "task-bench-allocs",
		states: []State{StateCompleted},
		result: mappedResult(b, msg),
	}
	c, err := newFromTransport(testBaseURL, tr)
	if err != nil {
		b.Fatalf("newFromTransport: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
	}()

	ctx := context.Background()
	const budget = 35
	allocs := testing.AllocsPerRun(1000, func() {
		h, err := c.Send(ctx, msg)
		if err != nil {
			b.Fatalf("Send: %v", err)
		}
		if _, err := c.Status(ctx, h); err != nil {
			b.Fatalf("Status: %v", err)
		}
		if _, err := c.Result(ctx, h); err != nil {
			b.Fatalf("Result: %v", err)
		}
	})
	if allocs > budget {
		b.Fatalf("allocs per cycle = %.1f, want <= %d", allocs, budget)
	}
}
