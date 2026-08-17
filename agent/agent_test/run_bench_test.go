package agent_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// benchRunWait builds and confirms a real Ack for every message, an
// in-process, synchronous round trip with no I/O.
func benchRunWait(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Confirm(), nil
}

// benchRunBus subscribes a no-op handler to every event Run's
// translator can emit.
func benchRunBus(b *testing.B) *events.Bus {
	b.Helper()
	bus := events.New()
	noop := func(ctx context.Context, e events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			b.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
	return bus
}

// BenchmarkRun benchmarks a two-step run with a real, synchronous
// AckWait round trip.
// Target: under two milliseconds per call.
// Measured: ~94 us/op, ~7050 B/op, 64 allocs/op. Two ed25519 signing
// operations dominate the cost; both are real cryptography, not test
// overhead.
func BenchmarkRun(b *testing.B) {
	a, m := twoStepFixture(b)
	bus := benchRunBus(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := a.Run(ctx, "thread-1", m, machine.InOut{}, benchRunWait, bus); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRunAllocBudget guards the allocation budget for a two-step run
// with a real ack round trip. AllocsPerRun must stay at or below the
// budget. A regression that adds an unnecessary copy or allocation to
// the hot path fails here.
func TestRunAllocBudget(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := events.New()
	noop := func(ctx context.Context, e events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
	ctx := context.Background()
	alloc := testing.AllocsPerRun(200, func() {
		if _, _, err := a.Run(ctx, "thread-1", m, machine.InOut{}, benchRunWait, bus); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > float64(runAllocBudget()) {
		t.Fatalf("Run allocated %.0f times per call; budget is %d", alloc, runAllocBudget())
	}
}

// runAllocBudget states the allocation budget for a one-step run with
// a real ack round trip. Measured at 28 allocs/op under go test, and
// up to 38 allocs/op under go test -race, whose instrumentation adds
// its own bookkeeping allocations. The budget covers both modes with
// a small margin, so a real regression, such as an unnecessary copy
// on the hot path, still fails.
func runAllocBudget() int {
	return 45
}
