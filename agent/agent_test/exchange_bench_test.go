package agent_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// BenchmarkExchange times a.Run alone for the two-agent exchange
// fixture, with a fresh threadID each iteration so PrevHash chaining
// never crosses iterations. b.ReportAllocs() runs.
// Target: under ten milliseconds per exchange.
// Measured: ~142 us/op, ~8.4 KB/op, 73 allocs/op. Two ed25519 signing
// operations (Agent A's step message, Agent B's receipt message) and
// the a2a JSON round trip dominate the cost.
func BenchmarkExchange(b *testing.B) {
	fx := newExchangeFixture(b, true)
	noop := func(ctx context.Context, e events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent, flow.StepCompletedEvent} {
		if err := fx.bus.Subscribe(name, noop); err != nil {
			b.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
	var captured []envelope.Message
	var refs []string
	wait := exchangeWait(b, fx, &captured, &refs)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		threadID := fmt.Sprintf("bench-thread-%d", i)
		if _, _, err := fx.a.Run(ctx, threadID, fx.m, machine.InOut{}, wait, fx.bus, nil, fx.r.ID()); err != nil {
			b.Fatal(err)
		}
	}
}
