// Package agent_test also holds the red-green unit cases for Run:
// the nil-wait, nil-bus, and empty-threadID sentinels and their
// check order, a confirmed one-step run, a corrected one-step run,
// an escalated one-step run, a one-step run where wait returns a
// plain error, a zero-step plan, and the three no-subscriber cases
// for confirmStep's and Run's own EmitX calls.
package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// newRunBus builds a bus subscribed to every event Run's translator
// calls can emit, so a real run never fails on a missing subscriber.
func newRunBus(t *testing.T) *events.Bus {
	t.Helper()
	bus := events.New()
	noop := func(ctx context.Context, e events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent, agent.ThreadVerifiedEvent, flow.StepCompletedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
	return bus
}

// newRunAgent builds a real Agent from a fresh identity, a valid
// card, and plan.
func newRunAgent(t testing.TB, plan *flow.Definition) *agent.Agent {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Runner", Capabilities: []string{"run"}}
	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	return a
}

// oneStepFixture builds a one-step, no-panel plan and a matching
// machine model: one transition from "start" to "done".
func oneStepFixture(t testing.TB) (*agent.Agent, *machine.Definition) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "step-a", To: "done", Payload: "do the thing"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return newRunAgent(t, plan), m
}

// confirmingWait builds and confirms a real Ack for every message it
// receives.
func confirmingWait(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Confirm(), nil
}

// correctingWait builds and corrects a real Ack for every message it
// receives.
func correctingWait(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
	if err != nil {
		return envelope.Ack{}, err
	}
	return ack.Correct("try again"), nil
}

// TestRunEntryChecks proves the nil-wait, nil-bus, and empty-threadID
// sentinels and their check order: wait first, then bus, then
// threadID. Each case returns machine.Status("") and the caller's in
// unchanged.
func TestRunEntryChecks(t *testing.T) {
	a := newRunAgent(t, &flow.Definition{})
	bus := events.New()

	cases := []struct {
		name     string
		wait     agent.AckWait
		bus      *events.Bus
		threadID string
		wantErr  error
	}{
		{"nil wait", nil, bus, "thread-1", agent.ErrNoWait},
		{"nil bus", confirmingWait, nil, "thread-1", agent.ErrNoBus},
		{"empty thread id", confirmingWait, bus, "", agent.ErrNoThread},
		{"nil wait and nil bus reports ErrNoWait", nil, nil, "thread-1", agent.ErrNoWait},
		{"nil bus and empty thread id reports ErrNoBus", confirmingWait, nil, "", agent.ErrNoBus},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			in := machine.InOut{Input: "keep me"}
			status, rec, err := a.Run(context.Background(), tt.threadID, nil, in, tt.wait, tt.bus, nil, "", nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Run() error = %v, want errors.Is match for %v", err, tt.wantErr)
			}
			if status != machine.Status("") {
				t.Fatalf("Run() status = %q, want empty", status)
			}
			if rec != in {
				t.Fatalf("Run() record = %+v, want unchanged %+v", rec, in)
			}
		})
	}
}

// TestRunOneStepConfirmed proves a one-step plan whose wait confirms
// returns a nil error and the target status.
func TestRunOneStepConfirmed(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
}

// TestRunOneStepRoomStamped proves a non-empty room argument makes
// confirmStep stamp it onto Message.Room before a.id.Sign runs: wait
// receives the same signed message EmitMessageDelivered emits, so
// capturing it there pins the exact message Run built and signed.
// The captured message still passes Validate and VerifySignature,
// proving a non-empty Room signs and validates cleanly.
func TestRunOneStepRoomStamped(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	var captured envelope.Message
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		captured = msg
		return confirmingWait(ctx, msg)
	}
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "room-1", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}
	if captured.Room != "room-1" {
		t.Fatalf("captured message Room = %q, want %q", captured.Room, "room-1")
	}
	if err := captured.Validate(); err != nil {
		t.Fatalf("captured message Validate() unexpected error: %v", err)
	}
	if err := captured.VerifySignature(); err != nil {
		t.Fatalf("captured message VerifySignature() unexpected error: %v", err)
	}
}

// TestRunOneStepRoomEmpty proves an empty room argument reproduces
// today's exact behavior: the captured message's Room stays the empty
// string. This pins the no-op path so a future change cannot silently
// start stamping a default room name.
func TestRunOneStepRoomEmpty(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	var captured envelope.Message
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		captured = msg
		return confirmingWait(ctx, msg)
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if captured.Room != "" {
		t.Fatalf("captured message Room = %q, want empty", captured.Room)
	}
}

// TestRunOneStepCorrected proves a one-step plan whose wait returns a
// corrected ack returns a non-nil error.
func TestRunOneStepCorrected(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, correctingWait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for a corrected ack")
	}
}

// TestRunOneStepEscalated proves a one-step plan whose wait wraps
// ErrEscalated returns an error errors.Is matches against
// ErrEscalated.
func TestRunOneStepEscalated(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	escalate := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		return envelope.Ack{}, fmt.Errorf("step needs a human: %w", agent.ErrEscalated)
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, escalate, bus, nil, "", nil)
	if !errors.Is(err, agent.ErrEscalated) {
		t.Fatalf("Run() error = %v, want errors.Is match for ErrEscalated", err)
	}
}

// TestRunOneStepPlainWaitError proves a one-step plan whose wait
// returns a plain error, wrapping nothing, returns a non-nil error
// that errors.Is still matches against the exact sentinel wait
// returned. This proves the ack-error short-circuit is unconditional:
// EmitMessageAcked never runs, so no zero-value-Ack Validate error
// masks the real cause.
func TestRunOneStepPlainWaitError(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	wantErr := errors.New("wait: connection refused")
	plainErr := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		return envelope.Ack{}, wantErr
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, plainErr, bus, nil, "", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want errors.Is match for %v", err, wantErr)
	}
}

// TestRunOneStepWaitErrorWithValidAck proves the wait-error
// short-circuit holds even when wait returns a validly-shaped Ack
// (one that would pass Ack.Validate) alongside a non-nil error: Run
// still returns the wait error unchanged, and EmitMessageAcked never
// runs, so MessageAckedEvent fires zero times.
func TestRunOneStepWaitErrorWithValidAck(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := events.New()
	acked := 0
	if err := bus.Subscribe(agent.MessageAckedEvent, func(ctx context.Context, e events.Event) error {
		acked++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageAckedEvent) unexpected error: %v", err)
	}
	if err := bus.Subscribe(agent.MessageDeliveredEvent, func(ctx context.Context, e events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe(MessageDeliveredEvent) unexpected error: %v", err)
	}
	wantErr := errors.New("wait: connection refused")
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		ack, err := envelope.NewAck(msg, "receiver", "restating "+msg.ID)
		if err != nil {
			t.Fatalf("envelope.NewAck() unexpected error: %v", err)
		}
		return ack.Confirm(), wantErr
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want errors.Is match for %v", err, wantErr)
	}
	if acked != 0 {
		t.Fatalf("MessageAckedEvent fired %d times, want 0: EmitMessageAcked must not run on a wait error", acked)
	}
}

// TestRunOneStepSignFailure proves a one-step plan whose Agent holds a
// broken identity (a zero-value *identity.Identity, non-nil but
// invalid) returns a non-nil error from the Sign call inside Run's
// Confirm closure, before wait ever runs.
func TestRunOneStepSignFailure(t *testing.T) {
	plan, err := flow.New([]flow.Step{
		{ID: "step-a", To: "done", Payload: "do the thing"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Broken", Capabilities: []string{"run"}}
	a, err := agent.New(&identity.Identity{}, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	bus := newRunBus(t)
	calls := 0
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		calls++
		return envelope.Ack{}, nil
	}
	_, _, err = a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for a broken identity")
	}
	if calls != 0 {
		t.Fatalf("wait called %d times, want 0: Sign fails before wait runs", calls)
	}
}

// TestRunOneStepWaitReturnsInvalidAckNoError proves a wait that
// returns a nil error alongside an Ack that fails Ack.Validate (a
// blank MessageID) surfaces the Validate error through
// EmitMessageAcked, and Run returns a non-nil error.
func TestRunOneStepWaitReturnsInvalidAckNoError(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := newRunBus(t)
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		return envelope.Ack{}, nil
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for an invalid ack")
	}
}

// TestRunConfirmStepMessageDeliveredNoSubscriber proves confirmStep
// surfaces EmitMessageDelivered's own bus error, unwrapped, when
// nothing subscribes to MessageDeliveredEvent. wait must never run:
// the delivered-event failure short-circuits confirmStep before the
// ack round trip starts.
func TestRunConfirmStepMessageDeliveredNoSubscriber(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := events.New()
	calls := 0
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		calls++
		return envelope.Ack{}, nil
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for a bus with no MessageDeliveredEvent subscriber")
	}
	if !strings.Contains(err.Error(), "no subscriber") {
		t.Fatalf("Run() error = %q, want it to contain %q", err.Error(), "no subscriber")
	}
	if calls != 0 {
		t.Fatalf("wait called %d times, want 0: EmitMessageDelivered must fail before wait runs", calls)
	}
}

// TestRunConfirmStepMessageAckedNoSubscriber proves confirmStep
// surfaces EmitMessageAcked's own bus error, unwrapped, when a
// validly-shaped, confirmed Ack has no subscriber for
// MessageAckedEvent. This differs from
// TestRunOneStepWaitReturnsInvalidAckNoError: that case fails inside
// EmitMessageAcked's own Ack.Validate call, before bus.Emit ever
// runs, so it leaves this bus.Emit failure path unexercised even
// though both cases return a non-nil error from the same line.
func TestRunConfirmStepMessageAckedNoSubscriber(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := events.New()
	if err := bus.Subscribe(agent.MessageDeliveredEvent, func(ctx context.Context, e events.Event) error { return nil }); err != nil {
		t.Fatalf("Subscribe(MessageDeliveredEvent) unexpected error: %v", err)
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for a bus with no MessageAckedEvent subscriber")
	}
	if !strings.Contains(err.Error(), "no subscriber") {
		t.Fatalf("Run() error = %q, want it to contain %q", err.Error(), "no subscriber")
	}
}

// TestRunThreadVerifiedNoSubscriber proves Run surfaces
// EmitThreadVerified's own bus error, unwrapped, when a one-step
// run's message verifies as a thread but nothing subscribes to
// ThreadVerifiedEvent. The step itself still completes: only the
// closing EmitThreadVerified call fails.
func TestRunThreadVerifiedNoSubscriber(t *testing.T) {
	a, m := oneStepFixture(t)
	bus := events.New()
	noop := func(ctx context.Context, e events.Event) error { return nil }
	for _, name := range []events.Name{agent.MessageDeliveredEvent, agent.MessageAckedEvent} {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%q) unexpected error: %v", name, err)
		}
	}
	_, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, confirmingWait, bus, nil, "", nil)
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for a bus with no ThreadVerifiedEvent subscriber")
	}
	if !strings.Contains(err.Error(), "no subscriber") {
		t.Fatalf("Run() error = %q, want it to contain %q", err.Error(), "no subscriber")
	}
}

// TestRunZeroStepPlan proves a zero-step plan returns a nil error and
// the initial status, and calls wait zero times.
func TestRunZeroStepPlan(t *testing.T) {
	a := newRunAgent(t, &flow.Definition{})
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	bus := events.New()
	calls := 0
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		calls++
		return envelope.Ack{}, nil
	}
	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, wait, bus, nil, "", nil)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("start") {
		t.Fatalf("Run() status = %q, want %q", status, "start")
	}
	if calls != 0 {
		t.Fatalf("wait called %d times, want 0", calls)
	}
}
