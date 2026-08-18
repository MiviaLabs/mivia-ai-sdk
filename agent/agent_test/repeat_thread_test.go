package agent_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// repeatBus builds a bus subscribed to the three agent event names,
// because Emit fails a name with no subscriber and Run propagates
// that failure.
func repeatBus(t *testing.T) *events.Bus {
	t.Helper()
	bus := events.New()
	noop := func(context.Context, events.Event) error { return nil }
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%s): %v", name, err)
		}
	}
	return bus
}

// repeatAgent builds an agent over plan under a fresh identity.
func repeatAgent(t *testing.T, plan *flow.Definition) *agent.Agent {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	a, err := agent.New(id, discovery.Card{Name: "repeat", Capabilities: []string{"t"}}, plan)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return a
}

// repeatMachine builds the ping-pong rows a two-iteration loop needs:
// the child's own walk and the parent's alternating fires.
func repeatMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "mid", Trigger: "go"},
		machine.Transition{From: "mid", To: "a", Trigger: "toA"},
		machine.Transition{From: "mid", To: "b", Trigger: "toB"},
		machine.Transition{From: "start", To: "a", Trigger: "outerA"},
		machine.Transition{From: "a", To: "b", Trigger: "outerB"},
		machine.Transition{From: "b", To: "a", Trigger: "outerA2"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// loopRepeatChild returns a child whose final status alternates
// between a and b, so the parent never needs a self row.
func loopRepeatChild(t *testing.T, parity *int32) *flow.Definition {
	t.Helper()
	d, err := flow.New([]flow.Step{
		{
			ID: "branch", To: "mid", Payload: "p",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				if atomic.AddInt32(parity, 1)%2 == 1 {
					return []string{"toA"}, nil
				}
				return []string{"toB"}, nil
			},
		},
		{ID: "toA", To: "a", Needs: []string{"branch"}, Payload: "pa"},
		{ID: "toB", To: "b", Needs: []string{"branch"}, Payload: "pb"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	return d
}

// TestRunLoopedChildRepeatVerifiesThread proves a step confirmed
// twice in one thread gets a numeric suffix on its second message,
// and the whole thread still verifies.
func TestRunLoopedChildRepeatVerifiesThread(t *testing.T) {
	ctx := context.Background()
	parity := int32(0)
	twice := func(ctx context.Context) (bool, error) {
		st, _ := flow.LoopStateFrom(ctx)
		return st.Iteration == 0, nil
	}
	plan, err := flow.New([]flow.Step{
		{ID: "parent", To: "unused", Payload: "pp",
			Sub: loopRepeatChild(t, &parity), Loop: &flow.LoopPolicy{Guard: twice}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}

	var msgs []envelope.Message
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		msgs = append(msgs, msg)
		ack, err := envelope.NewAck(msg, "w", "ok")
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
	a := repeatAgent(t, plan)
	status, _, err := a.Run(ctx, "thread-loop", repeatMachine(t), machine.InOut{},
		wait, repeatBus(t), nil, "", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = status
	if err := envelope.VerifyThread(msgs); err != nil {
		t.Fatalf("VerifyThread: %v", err)
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	want := []string{"branch", "toA", "branch#2", "toB", "parent"}
	if !equalStrings(ids, want) {
		t.Fatalf("message ids = %v, want %v", ids, want)
	}
}

// TestRunSiblingSubsShareChildIDs proves two Sub children that reuse
// one step ID both confirm, with the second occurrence suffixed, and
// the thread verifies.
func TestRunSiblingSubsShareChildIDs(t *testing.T) {
	ctx := context.Background()
	childA, err := flow.New([]flow.Step{{ID: "inner", To: "a", Payload: "ia"}}, nil)
	if err != nil {
		t.Fatalf("flow.New childA: %v", err)
	}
	childB, err := flow.New([]flow.Step{{ID: "inner", To: "b", Payload: "ib"}}, nil)
	if err != nil {
		t.Fatalf("flow.New childB: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "first", To: "unused", Payload: "f", Sub: childA},
		{ID: "second", To: "unused2", Needs: []string{"first"}, Payload: "s", Sub: childB},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "a", Trigger: "t1"},
		machine.Transition{From: "start", To: "b", Trigger: "t2"},
		machine.Transition{From: "a", To: "b", Trigger: "t3"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	var msgs []envelope.Message
	wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		msgs = append(msgs, msg)
		ack, err := envelope.NewAck(msg, "w", "ok")
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
	a := repeatAgent(t, plan)
	_, _, err = a.Run(ctx, "thread-subs", m, machine.InOut{},
		wait, repeatBus(t), nil, "", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := envelope.VerifyThread(msgs); err != nil {
		t.Fatalf("VerifyThread: %v", err)
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	want := []string{"inner", "first", "inner#2", "second"}
	if !equalStrings(ids, want) {
		t.Fatalf("message ids = %v, want %v", ids, want)
	}
}
