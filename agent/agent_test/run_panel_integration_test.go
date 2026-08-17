// Package agent_test also holds the panel-path integration cases for
// Run: a mixed gated-and-panel plan, and a panel-only plan with zero
// gated steps.
package agent_test

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// countingWait counts wait calls per step ID and confirms every ack.
type countingWait struct {
	mu    sync.Mutex
	calls map[string]int
}

func newCountingWait() *countingWait {
	return &countingWait{calls: map[string]int{}}
}

func (w *countingWait) run(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	w.mu.Lock()
	w.calls[msg.ID]++
	w.mu.Unlock()
	ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Confirm(), nil
}

func (w *countingWait) total() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, c := range w.calls {
		n += c
	}
	return n
}

// eventCounter counts events by name.
type eventCounter struct {
	mu     sync.Mutex
	counts map[events.Name]int
}

func newEventCounter(bus *events.Bus, t *testing.T, names ...events.Name) *eventCounter {
	t.Helper()
	c := &eventCounter{counts: map[events.Name]int{}}
	for _, name := range names {
		n := name
		if err := bus.Subscribe(n, func(ctx context.Context, e events.Event) error {
			c.mu.Lock()
			c.counts[n]++
			c.mu.Unlock()
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", n, err)
		}
	}
	return c
}

func (c *eventCounter) get(name events.Name) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[name]
}

// TestRunMixedGatedAndPanel proves a plan with one sequential gated
// step and one two-member panel step calls wait exactly once, for the
// gated step only; the bus receives message events only for the
// gated step; and ThreadVerifiedEvent fires once, over the one
// message the gated step built.
func TestRunMixedGatedAndPanel(t *testing.T) {
	plan, err := flow.New([]flow.Step{
		{ID: "gate", To: "gated", Payload: "gate payload"},
		{ID: "p1", Needs: []string{"gate"}, To: "panel-done"},
		{ID: "p2", Needs: []string{"gate"}, To: "panel-done"},
	}, []flow.Panel{{"p1", "p2"}})
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "gated", Trigger: "go-gate"},
		machine.Transition{From: "gated", To: "panel-done", Trigger: "go-panel"},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	a := newRunAgent(t, plan)
	bus := events.New()
	counter := newEventCounter(bus, t, agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent, flow.StepCompletedEvent)

	w := newCountingWait()
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, w.run, bus, nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("panel-done") {
		t.Fatalf("Run() status = %q, want %q", status, "panel-done")
	}

	if got := w.total(); got != 1 {
		t.Fatalf("wait called %d times total, want 1", got)
	}
	if got := w.calls["gate"]; got != 1 {
		t.Fatalf("wait called %d times for step gate, want 1", got)
	}
	if got := w.calls["p1"]; got != 0 {
		t.Fatalf("wait called %d times for panel member p1, want 0", got)
	}
	if got := w.calls["p2"]; got != 0 {
		t.Fatalf("wait called %d times for panel member p2, want 0", got)
	}

	if got := counter.get(agent.MessageDeliveredEvent); got != 1 {
		t.Fatalf("MessageDeliveredEvent fired %d times, want 1", got)
	}
	if got := counter.get(agent.MessageAckedEvent); got != 1 {
		t.Fatalf("MessageAckedEvent fired %d times, want 1", got)
	}
	if got := counter.get(agent.ThreadVerifiedEvent); got != 1 {
		t.Fatalf("ThreadVerifiedEvent fired %d times, want 1", got)
	}
}

// TestRunPanelOnlyPlanNoGatedSteps proves a plan that is only a
// two-member panel, with no gated step, succeeds with zero wait
// calls, no message events, and no ThreadVerifiedEvent.
func TestRunPanelOnlyPlanNoGatedSteps(t *testing.T) {
	plan, err := flow.New([]flow.Step{
		{ID: "p1", To: "panel-done"},
		{ID: "p2", To: "panel-done"},
	}, []flow.Panel{{"p1", "p2"}})
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "panel-done", Trigger: "go-panel"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	a := newRunAgent(t, plan)
	bus := events.New()
	counter := newEventCounter(bus, t, agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent, flow.StepCompletedEvent)

	w := newCountingWait()
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, w.run, bus, nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("panel-done") {
		t.Fatalf("Run() status = %q, want %q", status, "panel-done")
	}

	if got := w.total(); got != 0 {
		t.Fatalf("wait called %d times, want 0", got)
	}
	if got := counter.get(agent.MessageDeliveredEvent); got != 0 {
		t.Fatalf("MessageDeliveredEvent fired %d times, want 0", got)
	}
	if got := counter.get(agent.MessageAckedEvent); got != 0 {
		t.Fatalf("MessageAckedEvent fired %d times, want 0", got)
	}
	if got := counter.get(agent.ThreadVerifiedEvent); got != 0 {
		t.Fatalf("ThreadVerifiedEvent fired %d times, want 0", got)
	}
}
