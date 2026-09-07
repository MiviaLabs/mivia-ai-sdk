// Package workflow_test also holds the full-lifecycle integration test:
// a real identity, a real discovery.Card, a real flow.Definition with
// one one-member panel step and one sequential step, workflow.New
// binding them, and workflow.Run executing the plan end to end.
package workflow_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
)

// lifecycleFixture builds a real Agent whose plan has one root step
// named in a one-member panel and one sequential step that needs the
// root, and a machine model whose transitions cover both steps' To
// values.
func lifecycleFixture(t *testing.T) (*workflow.Agent, *machine.Definition) {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Lifecycle Agent", Description: "runs the full loop", Capabilities: []string{"run"}}
	plan, err := flow.New([]flow.Step{
		{ID: "root", To: "root-done", Payload: "root payload"},
		{ID: "next", Needs: []string{"root"}, To: "final", Payload: "next payload"},
	}, []flow.Panel{{"root"}})
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	a, err := workflow.New(id, card, plan)
	if err != nil {
		t.Fatalf("workflow.New() unexpected error: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "root-done", Trigger: "go-root"},
		machine.Transition{From: "root-done", To: "final", Trigger: "go-next"},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return a, m
}

// lifecycleRecorder captures the ordered sequence of event names a
// shared bus delivers, guarded by a mutex for concurrent handlers.
type lifecycleRecorder struct {
	mu   sync.Mutex
	seen []events.Name
}

func (r *lifecycleRecorder) subscribe(t *testing.T, bus *events.Bus, names ...events.Name) {
	t.Helper()
	for _, name := range names {
		if err := bus.Subscribe(name, r.handler); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
}

func (r *lifecycleRecorder) handler(ctx context.Context, e events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, e.Name)
	return nil
}

func (r *lifecycleRecorder) snapshot() []events.Name {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Name(nil), r.seen...)
}

// TestLifecycleFullRunSucceeds proves Run returns a nil error and the
// machine's final Status, and that the event sequence on the bus is
// exactly MessageDeliveredEvent, MessageAckedEvent, StepCompletedEvent
// for the root step, the same three-event sequence for the next step,
// then one closing ThreadVerifiedEvent.
func TestLifecycleFullRunSucceeds(t *testing.T) {
	a, m := lifecycleFixture(t)
	bus := events.New()
	rec := &lifecycleRecorder{}
	rec.subscribe(t, bus, workflow.MessageDeliveredEvent, workflow.MessageAckedEvent, flow.StepCompletedEvent, workflow.ThreadVerifiedEvent)

	status, _, err := a.Run(context.Background(), "thread-lifecycle", m, machine.InOut{}, confirmingWait, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("final") {
		t.Fatalf("Run() status = %q, want %q", status, "final")
	}

	want := []events.Name{
		workflow.MessageDeliveredEvent, workflow.MessageAckedEvent, flow.StepCompletedEvent,
		workflow.MessageDeliveredEvent, workflow.MessageAckedEvent, flow.StepCompletedEvent,
		workflow.ThreadVerifiedEvent,
	}
	if !equalNames(rec.snapshot(), want) {
		t.Fatalf("event sequence = %v, want %v", rec.snapshot(), want)
	}
}

// TestLifecycleForcedAckFailureHaltsWithoutErasingPriorEvents proves a
// second run, with wait returning a corrected ack for the sequential
// step, halts before that step's StepCompletedEvent fires, returns a
// non-nil error, and never emits ThreadVerifiedEvent. The root step's
// earlier MessageDeliveredEvent, MessageAckedEvent, and
// StepCompletedEvent still appear on the bus: the forced failure
// stops the walk, it does not erase what already happened. Per the
// plan's documented Fire-before-Confirm caveat, this test does not
// assert rollback of the machine state; flow.Run does not roll back,
// and that is pre-existing, out-of-scope behavior.
func TestLifecycleForcedAckFailureHaltsWithoutErasingPriorEvents(t *testing.T) {
	a, m := lifecycleFixture(t)
	bus := events.New()
	rec := &lifecycleRecorder{}
	rec.subscribe(t, bus, workflow.MessageDeliveredEvent, workflow.MessageAckedEvent, flow.StepCompletedEvent, workflow.ThreadVerifiedEvent)

	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		if msg.ID == "next" {
			return correctingWait(ctx, msg)
		}
		return confirmingWait(ctx, msg)
	}

	_, _, err := a.Run(context.Background(), "thread-lifecycle-fail", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for a corrected ack")
	}
	if errors.Is(err, workflow.ErrEscalated) {
		t.Fatalf("Run() error unexpectedly wraps ErrEscalated: %v", err)
	}

	seen := rec.snapshot()
	want := []events.Name{
		workflow.MessageDeliveredEvent, workflow.MessageAckedEvent, flow.StepCompletedEvent,
		workflow.MessageDeliveredEvent, workflow.MessageAckedEvent,
	}
	if !equalNames(seen, want) {
		t.Fatalf("event sequence = %v, want %v", seen, want)
	}
}
