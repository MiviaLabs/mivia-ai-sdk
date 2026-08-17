// Package agent_test also holds the run-loop integration cases: a
// real two-step sequential plan proving the ack for step one confirms
// before wait runs for step two, and an escalated second step.
package agent_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// twoStepFixture builds a two-step sequential plan (b needs a) and a
// matching two-transition machine model.
func twoStepFixture(t testing.TB) (*agent.Agent, *machine.Definition) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "a", To: "a-done", Payload: "step a payload"},
		{ID: "b", Needs: []string{"a"}, To: "b-done", Payload: "step b payload"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "a-done", Trigger: "go-a"},
		machine.Transition{From: "a-done", To: "b-done", Trigger: "go-b"},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return newRunAgent(t, plan), m
}

// orderedWait records the order it is called in and confirms every
// ack, proving one step's ack resolves strictly before the next
// step's wait call, via an ordered log, not a sleep-based race.
type orderedWait struct {
	mu    sync.Mutex
	order []string
	msgs  []envelope.Message
}

func (w *orderedWait) run(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	w.mu.Lock()
	w.order = append(w.order, msg.ID)
	w.msgs = append(w.msgs, msg)
	w.mu.Unlock()
	ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Confirm(), nil
}

// TestRunTwoStepOrderAndEvents proves step a's ack confirms strictly
// before wait runs for step b, each captured message independently
// passes Message.Validate(), the final status matches the machine's
// end state, and the bus receives the five expected events in order.
func TestRunTwoStepOrderAndEvents(t *testing.T) {
	a, m := twoStepFixture(t)
	bus := events.New()

	var (
		mu   sync.Mutex
		seen []events.Name
	)
	record := func(name events.Name) events.Handler {
		return func(ctx context.Context, e events.Event) error {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, name)
			return nil
		}
	}
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent, flow.StepCompletedEvent} {
		if err := bus.Subscribe(name, record(name)); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}

	w := &orderedWait{}
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, w.run, bus)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("b-done") {
		t.Fatalf("Run() status = %q, want %q", status, "b-done")
	}

	if want := []string{"a", "b"}; !equalStrings(w.order, want) {
		t.Fatalf("wait call order = %v, want %v", w.order, want)
	}
	if len(w.msgs) != 2 {
		t.Fatalf("wait received %d messages, want 2", len(w.msgs))
	}
	for i, msg := range w.msgs {
		if err := msg.Validate(); err != nil {
			t.Fatalf("captured message %d (%s) Validate() error: %v", i, msg.ID, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	want := []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent,
		agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent,
		agent.ThreadVerifiedEvent,
	}
	if !equalNames(seen, want) {
		t.Fatalf("event sequence = %v, want %v", seen, want)
	}
}

// TestRunTwoStepEscalatedSecondStep proves an AckWait that confirms
// step a and escalates step b returns errors.Is(err,
// agent.ErrEscalated), the bus receives no ThreadVerifiedEvent, and
// wait was called exactly twice.
func TestRunTwoStepEscalatedSecondStep(t *testing.T) {
	a, m := twoStepFixture(t)
	bus := newRunBus(t)

	calls := 0
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		calls++
		if msg.ID == "b" {
			return envelope.Ack{}, fmt.Errorf("step %s needs a human: %w", msg.ID, agent.ErrEscalated)
		}
		ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}

	threadFired := 0
	if err := bus.Subscribe(agent.ThreadVerifiedEvent, func(ctx context.Context, e events.Event) error {
		threadFired++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}

	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus)
	if !errors.Is(err, agent.ErrEscalated) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrEscalated", err)
	}
	if calls != 2 {
		t.Fatalf("wait called %d times, want 2", calls)
	}
	if threadFired != 0 {
		t.Fatalf("ThreadVerifiedEvent fired %d times, want 0", threadFired)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalNames(a, b []events.Name) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
