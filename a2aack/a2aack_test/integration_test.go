package a2aack_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/a2aack"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aclient"
	"github.com/MiviaLabs/mivia-ai-sdk/a2aloopback"
	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// integrationFixture boots a live Loopback and a real client, then
// builds a one-step agent whose AckWait resolves through a2aack.Wait.
// It returns the AckWait, the agent, and the machine model.
func integrationFixture(t testing.TB) (agent.AckWait, *agent.Agent, *machine.Definition) {
	t.Helper()
	addr, stop, err := a2aloopback.Loopback()
	if err != nil {
		t.Fatalf("Loopback() error: %v", err)
	}
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Errorf("Loopback stop() error: %v", err)
		}
	})
	client, err := a2aclient.New(addr)
	if err != nil {
		t.Fatalf("a2aclient.New() error: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("client.Close() error: %v", err)
		}
	})
	ackFn, err := a2aack.Wait(client, a2aack.Options{Poll: 2 * time.Millisecond, Timeout: time.Second})
	if err != nil {
		t.Fatalf("a2aack.Wait() error: %v", err)
	}

	plan, err := flow.New([]flow.Step{
		{ID: "step-a", To: "done", Payload: "remote the step"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New() error: %v", err)
	}
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() error: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "A2A", Capabilities: []string{"ack"}}, plan)
	if err != nil {
		t.Fatalf("agent.New() error: %v", err)
	}
	return ackFn, a, m
}

// ackCounts records the events a real run emits.
type ackCounts struct {
	mu             sync.Mutex
	acked          int
	ackedConfirmed int
	threadFired    int
}

// ackCountingBus builds a bus that counts the ack and thread events
// and ignores the message-delivered and step events.
func ackCountingBus(t testing.TB) (*events.Bus, *ackCounts) {
	t.Helper()
	counts := &ackCounts{}
	bus := events.New()
	noop := func(context.Context, events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, flow.StepCompletedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%q) error: %v", name, err)
		}
	}
	if err := bus.Subscribe(agent.MessageAckedEvent, func(ctx context.Context, e events.Event) error {
		counts.mu.Lock()
		defer counts.mu.Unlock()
		counts.acked++
		if strings.Contains(e.Data, "status confirmed") {
			counts.ackedConfirmed++
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageAckedEvent) error: %v", err)
	}
	if err := bus.Subscribe(agent.ThreadVerifiedEvent, func(ctx context.Context, e events.Event) error {
		counts.mu.Lock()
		counts.threadFired++
		counts.mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(ThreadVerifiedEvent) error: %v", err)
	}
	return bus, counts
}

// TestIntegrationAgentStepThroughWait drives one real agent.Agent whose
// single gated step resolves through Wait over a live Loopback. It
// asserts the final status, one confirmed MessageAckedEvent, and one
// ThreadVerifiedEvent.
func TestIntegrationAgentStepThroughWait(t *testing.T) {
	ackFn, a, m := integrationFixture(t)
	bus, counts := ackCountingBus(t)

	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, ackFn, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}

	counts.mu.Lock()
	defer counts.mu.Unlock()
	if counts.acked != 1 || counts.ackedConfirmed != 1 {
		t.Fatalf("MessageAckedEvent observed %d acks (%d confirmed), want 1 confirmed", counts.acked, counts.ackedConfirmed)
	}
	if counts.threadFired != 1 {
		t.Fatalf("ThreadVerifiedEvent fired %d times, want 1", counts.threadFired)
	}
}
