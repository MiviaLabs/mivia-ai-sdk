package flow_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestEmitStepCompletedOnLinearGraph subscribes to StepCompletedEvent,
// runs a three-step linear graph with a non-nil bus, and proves every
// step emits exactly one event in topological order.
func TestEmitStepCompletedOnLinearGraph(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var mu sync.Mutex
	var received []string
	if err := bus.Subscribe(flow.StepCompletedEvent, func(ctx context.Context, e events.Event) error {
		mu.Lock()
		received = append(received, e.Data)
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	const (
		s1 = machine.Status("s1")
		s2 = machine.Status("s2")
		s3 = machine.Status("s3")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(s1)},
		{ID: "b", Needs: []string{"a"}, To: string(s2)},
		{ID: "c", Needs: []string{"b"}, To: string(s3)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: s1, Trigger: triggerGo},
		machine.Transition{From: s1, To: s2, Trigger: triggerGo},
		machine.Transition{From: s2, To: s3, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, bus)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != s3 {
		t.Fatalf("status = %q, want %q", status, s3)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"step a completed", "step b completed", "step c completed"}
	if len(received) != len(want) {
		t.Fatalf("got %d events, want %d: %v", len(received), len(want), received)
	}
	for i := range want {
		if received[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q", i, received[i], want[i])
		}
	}
}

// TestEmitNilBusRunsWithoutPanic proves Run does not panic when bus
// is nil. Every existing test passes nil; this case is explicit.
func TestEmitNilBusRunsWithoutPanic(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
}

// TestEmitOnPanelWave proves every member of a multi-member panel
// emits a StepCompletedEvent after the wave completes.
func TestEmitOnPanelWave(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var count atomic.Int64
	if err := bus.Subscribe(flow.StepCompletedEvent, func(ctx context.Context, e events.Event) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	const panelTo = machine.Status("panel-done")
	d, err := flow.New([]flow.Step{
		{ID: "m1", To: string(panelTo)},
		{ID: "m2", To: string(panelTo)},
		{ID: "m3", To: string(panelTo)},
	}, []flow.Panel{{"m1", "m2", "m3"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, bus)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := count.Load(); got != 3 {
		t.Fatalf("got %d events, want 3", got)
	}
}

// TestEmitOnChainedStep proves a chained step emits after the child
// workflow completes and the parent confirms. The child workflow runs
// with a nil bus, so child steps do not emit.
func TestEmitOnChainedStep(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var mu sync.Mutex
	var received []string
	if err := bus.Subscribe(flow.StepCompletedEvent, func(ctx context.Context, e events.Event) error {
		mu.Lock()
		received = append(received, e.Data)
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	const childTo = machine.Status("child-done")
	child, err := flow.New([]flow.Step{
		{ID: "child", To: string(childTo)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New (child): %v", err)
	}
	parent, err := flow.New([]flow.Step{
		{ID: "parent", To: string(childTo), Sub: child},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New (parent): %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: childTo, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	report, err := flow.Run(context.Background(), parent, m, machine.InOut{}, noopConfirm, bus)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != childTo {
		t.Fatalf("status = %q, want %q", status, childTo)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("got %d events, want 1 (parent only): %v", len(received), received)
	}
	if received[0] != "step parent completed" {
		t.Fatalf("event = %q, want %q", received[0], "step parent completed")
	}
}

// TestEmitOnFailedGuard proves no event emits when a guard rejects
// the transition. The runner returns an error; the handler never runs.
func TestEmitOnFailedGuard(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var count atomic.Int64
	if err := bus.Subscribe(flow.StepCompletedEvent, func(ctx context.Context, e events.Event) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	const target = machine.Status("nope")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(target)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: target, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) { return false, nil },
		},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, bus)
	if err == nil {
		t.Fatal("expected guard rejection error, got nil")
	}
	if got := count.Load(); got != 0 {
		t.Fatalf("got %d events, want 0 (guard rejected)", got)
	}
}

// TestEmitNoneOnConfirmFailure proves no event emits when confirm
// rejects the ack. Run wraps the confirm error and names the step.
func TestEmitNoneOnConfirmFailure(t *testing.T) {
	t.Parallel()
	bus := events.New()
	var count atomic.Int64
	if err := bus.Subscribe(flow.StepCompletedEvent, func(ctx context.Context, e events.Event) error {
		count.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	confirmErr := errors.New("ack denied")
	confirm := func(ctx context.Context, step flow.Step) error { return confirmErr }
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, confirm, bus)
	if err == nil {
		t.Fatal("expected confirm failure error, got nil")
	}
	if !errors.Is(err, confirmErr) {
		t.Fatalf("error %q should wrap the confirm error %q", err, confirmErr)
	}
	if !strings.Contains(err.Error(), `step "a": ack not confirmed`) {
		t.Fatalf("error %q should contain the ack-not-confirmed message", err.Error())
	}
	if got := count.Load(); got != 0 {
		t.Fatalf("got %d events, want 0 (confirm failed)", got)
	}
}
