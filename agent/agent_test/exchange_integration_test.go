// Package agent_test also holds the two-agent system-integration
// test: two real identity.Identity values, a real room.Room, a real
// tools.Registry, a real memory.Store, and one a2a.ToPart/FromPart
// hop, wired around agent.Agent's Run and AckWait, with no mock at
// any trust boundary.
package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// echoToolPrefix marks the payload text an echo tool call strips
// before it returns the remainder.
const echoToolPrefix = "run:echo:"

// echoTool is Agent B's tool: it strips echoToolPrefix from its input
// string and counts every Run call, so a test can prove Run stayed at
// zero when a gate should have blocked the call before it reached the
// registry.
type echoTool struct {
	calls *int
}

// Name identifies this tool as "echo" in a tools.Registry.
func (e *echoTool) Name() string { return "echo" }

// Run strips echoToolPrefix from in.Value and returns the remainder.
// A value with no such prefix is caller error; the fixture never
// triggers that path.
func (e *echoTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*e.calls++
	s, ok := in.Value.(string)
	if !ok {
		return tools.Out{}, fmt.Errorf("echo: value is %T, want string", in.Value)
	}
	if !strings.HasPrefix(s, echoToolPrefix) {
		return tools.Out{}, fmt.Errorf("echo: value %q lacks prefix %q", s, echoToolPrefix)
	}
	return tools.Out{Value: strings.TrimPrefix(s, echoToolPrefix)}, nil
}

// exchangeFixture bundles the real state a two-agent exchange test
// needs: Agent A's identity.Identity-backed agent.Agent, the machine
// model its plan targets, Agent B's identity.Identity, the shared
// room.Room, Agent B's tools.Registry and memory.Store, the call
// counter echoTool increments, and the events.Bus the exchange
// reports through.
type exchangeFixture struct {
	a        *agent.Agent
	m        *machine.Definition
	idB      *identity.Identity
	r        *room.Room
	registry *tools.Registry
	store    *memory.Store
	bus      *events.Bus
	calls    *int
}

// newExchangeFixture builds an exchangeFixture. admitB controls
// whether Agent B's signer joins the shared room before the test
// runs the exchange.
func newExchangeFixture(t testing.TB, admitB bool) *exchangeFixture {
	t.Helper()
	idA, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	idB, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{
		Name:         "Requester",
		Description:  "requests an echo from agent B",
		Capabilities: []string{"exchange"},
	}
	plan, err := flow.New([]flow.Step{
		{ID: "request", To: "fulfilled", Payload: echoToolPrefix + "hello from agent A"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	a, err := agent.New(idA, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "fulfilled", Trigger: "go-request"})
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	r, err := room.New("exchange-room", idA.Signer())
	if err != nil {
		t.Fatalf("room.New() unexpected error: %v", err)
	}
	if admitB {
		if err := r.Admit(idB.Signer(), idA.Signer()); err != nil {
			t.Fatalf("room.Admit() unexpected error: %v", err)
		}
	}
	registry := tools.New()
	calls := new(int)
	if err := registry.Add(&echoTool{calls: calls}); err != nil {
		t.Fatalf("tools.Registry.Add() unexpected error: %v", err)
	}
	store, err := memory.New(4096)
	if err != nil {
		t.Fatalf("memory.New() unexpected error: %v", err)
	}
	return &exchangeFixture{
		a: a, m: m, idB: idB, r: r,
		registry: registry, store: store,
		bus: events.New(), calls: calls,
	}
}

// buildReceiptMessage builds the envelope.Message Agent B signs to
// confirm receipt of req inside the shared room, chained through
// InReplyTo. room.Accepts gates this message on idB's own room
// membership, since idB is the signer here: unlike req, whose signer
// is always Agent A the room founder, this message's admission check
// actually depends on whether Agent B has joined the room.
func buildReceiptMessage(idB *identity.Identity, req envelope.Message) (envelope.Message, error) {
	receipt := envelope.Message{
		Version:   envelope.Version,
		ID:        req.ID + "-receipt",
		Room:      req.Room,
		ThreadID:  req.ThreadID,
		InReplyTo: req.ID,
		Intent:    envelope.IntentAssert,
		Epistemic: envelope.EpistemicAssumed,
		Payload:   "agent B received " + req.ID,
	}
	return idB.Sign(receipt)
}

// exchangeWait builds the AckWait closure that stands in for Agent
// B: it maps msg through a2a.ToPart and back through a2a.FromPart,
// verifies the round-tripped signature, builds and signs a receipt
// message with idB, checks that receipt's room admission (gating on
// Agent B's own membership, see buildReceiptMessage), runs the echo
// tool, stores the result in fx's memory.Store, appends the
// round-tripped message to captured, and confirms an envelope.Ack. A
// non-nil error from any step returns immediately, before the next
// step runs.
func exchangeWait(t testing.TB, fx *exchangeFixture, captured *[]envelope.Message, refs *[]string) agent.AckWait {
	return func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		mapped, err := a2a.ToPart(msg)
		if err != nil {
			return envelope.Ack{}, err
		}
		roundTripped, err := a2a.FromPart(mapped)
		if err != nil {
			return envelope.Ack{}, err
		}
		if err := roundTripped.VerifySignature(); err != nil {
			t.Fatalf("VerifySignature() unexpected error after a2a round trip: %v", err)
		}
		receipt, err := buildReceiptMessage(fx.idB, roundTripped)
		if err != nil {
			return envelope.Ack{}, err
		}
		if err := fx.r.Accepts(receipt); err != nil {
			return envelope.Ack{}, err
		}
		out, err := fx.registry.Run(ctx, "echo", tools.InOut{Value: roundTripped.Payload})
		if err != nil {
			return envelope.Ack{}, err
		}
		result, ok := out.Value.(string)
		if !ok {
			t.Fatalf("echo tool result is %T, want string", out.Value)
		}
		ref, err := fx.store.Put([]byte(result))
		if err != nil {
			return envelope.Ack{}, err
		}
		*refs = append(*refs, ref)
		*captured = append(*captured, roundTripped)
		ack, err := envelope.NewAck(roundTripped, fx.idB.Signer(), result)
		if err != nil {
			return envelope.Ack{}, err
		}
		return ack.Confirm(), nil
	}
}

// TestExchangeSignedRequestConfirmedAck runs one full exchange: Agent
// A signs a request, Agent B verifies it after an a2a round trip,
// checks room admission, runs the echo tool, stores the result, and
// confirms the ack. It proves the thread verifies from the test's own
// vantage point and the shared context landed in Agent B's store.
func TestExchangeSignedRequestConfirmedAck(t *testing.T) {
	fx := newExchangeFixture(t, true)
	rec := &lifecycleRecorder{}
	rec.subscribe(t, fx.bus, agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent, agent.ThreadVerifiedEvent)

	var captured []envelope.Message
	var refs []string
	wait := exchangeWait(t, fx, &captured, &refs)

	status, _, err := fx.a.Run(context.Background(), "exchange-thread-1", fx.m, machine.InOut{}, wait, fx.bus, nil, fx.r.ID())
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if status != machine.Status("fulfilled") {
		t.Fatalf("Run() status = %q, want %q", status, "fulfilled")
	}

	if len(captured) != 1 {
		t.Fatalf("captured %d messages, want 1", len(captured))
	}
	if err := captured[0].Validate(); err != nil {
		t.Fatalf("captured message Validate() unexpected error: %v", err)
	}
	if err := envelope.VerifyThread(captured); err != nil {
		t.Fatalf("envelope.VerifyThread() unexpected error: %v", err)
	}

	if len(refs) != 1 {
		t.Fatalf("stored %d refs, want 1", len(refs))
	}
	got, err := fx.store.Get(refs[0])
	if err != nil {
		t.Fatalf("Store.Get() unexpected error: %v", err)
	}
	if string(got) != "hello from agent A" {
		t.Fatalf("Store.Get() = %q, want %q", got, "hello from agent A")
	}

	want := []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent,
		agent.ThreadVerifiedEvent,
	}
	if !equalNames(rec.snapshot(), want) {
		t.Fatalf("event sequence = %v, want %v", rec.snapshot(), want)
	}
}

// TestExchangeRejectsUnadmittedReceiver proves the trust boundary is
// real: with Agent B's signer never admitted, Room.Accepts blocks the
// exchange before the echo tool runs. The tool call counter stays at
// zero, proving Accepts gates the tool call, not just the ack.
func TestExchangeRejectsUnadmittedReceiver(t *testing.T) {
	fx := newExchangeFixture(t, false)
	rec := &lifecycleRecorder{}
	rec.subscribe(t, fx.bus, agent.MessageDeliveredEvent, agent.MessageAckedEvent, flow.StepCompletedEvent, agent.ThreadVerifiedEvent)

	var captured []envelope.Message
	var refs []string
	wait := exchangeWait(t, fx, &captured, &refs)

	status, _, err := fx.a.Run(context.Background(), "exchange-thread-2", fx.m, machine.InOut{}, wait, fx.bus, nil, fx.r.ID())
	if err == nil {
		t.Fatal("Run() returned a nil error, want a non-nil error for an unadmitted receiver")
	}
	if !errors.Is(err, room.ErrNotMember) {
		t.Fatalf("Run() error = %v, want errors.Is match for room.ErrNotMember", err)
	}
	// flow.Run fires the machine transition before it calls Confirm, so
	// a Confirm failure leaves the machine already moved. This mirrors
	// TestLifecycleForcedAckFailureHaltsWithoutErasingPriorEvents's
	// documented Fire-before-Confirm caveat: the walk halts, but it
	// does not roll back the status Fire already committed.
	if status != machine.Status("fulfilled") {
		t.Fatalf("Run() status = %q, want %q", status, "fulfilled")
	}

	for _, name := range rec.snapshot() {
		if name == agent.ThreadVerifiedEvent {
			t.Fatal("ThreadVerifiedEvent fired, want it never to fire on a rejected exchange")
		}
	}
	if *fx.calls != 0 {
		t.Fatalf("echo tool Run called %d times, want 0: Accepts must gate the tool call", *fx.calls)
	}
	if len(captured) != 0 {
		t.Fatalf("captured %d messages, want 0", len(captured))
	}
}
