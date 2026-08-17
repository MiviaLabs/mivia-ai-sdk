package events_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// buildOneBus returns a bus with one handler on one event name.
func buildOneBus() *events.Bus {
	b := events.New()
	if err := b.Subscribe("move", func(context.Context, events.Event) error { return nil }); err != nil {
		panic("buildOneBus: " + err.Error())
	}
	return b
}

// BenchmarkEmitOne benchmarks Emit on one event with one handler.
// Target: stays allocation-free in the dispatch loop.
func BenchmarkEmitOne(b *testing.B) {
	bus := buildOneBus()
	ctx := context.Background()
	ev := events.Event{Name: "move", Data: "x"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bus.Emit(ctx, ev); err != nil {
			b.Fatal(err)
		}
	}
}

// TestEmitAllocBudget guards the allocation floor for Emit.
// AllocsPerRun must stay at or below one; the single allowed
// allocation is the handler-slice copy Emit makes under the mutex.
func TestEmitAllocBudget(t *testing.T) {
	bus := buildOneBus()
	ctx := context.Background()
	ev := events.Event{Name: "move", Data: "x"}
	alloc := testing.AllocsPerRun(1000, func() {
		if err := bus.Emit(ctx, ev); err != nil {
			t.Fatal(err)
		}
	})
	const budget = 1
	if alloc > budget {
		t.Fatalf("Emit allocated %v times per call; budget is %d", alloc, budget)
	}
}
