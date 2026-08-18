package dispatch_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/dispatch"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// deliveredAckedCounts tallies MessageDeliveredEvent and
// MessageAckedEvent on one bus.
type deliveredAckedCounts struct {
	mu        sync.Mutex
	delivered int
	acked     int
}

// countingBus builds a bus that tallies the two dispatch-relevant
// events and ignores everything else it must subscribe to keep Emit
// from erroring on an unrelated name.
func countingBus(t testing.TB, extra ...events.Name) (*events.Bus, *deliveredAckedCounts) {
	t.Helper()
	counts := &deliveredAckedCounts{}
	bus := events.New()
	if err := bus.Subscribe(agent.MessageDeliveredEvent, func(context.Context, events.Event) error {
		counts.mu.Lock()
		counts.delivered++
		counts.mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageDeliveredEvent) error: %v", err)
	}
	if err := bus.Subscribe(agent.MessageAckedEvent, func(context.Context, events.Event) error {
		counts.mu.Lock()
		counts.acked++
		counts.mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("Subscribe(MessageAckedEvent) error: %v", err)
	}
	noop := func(context.Context, events.Event) error { return nil }
	for _, name := range extra {
		if err := bus.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%q) error: %v", name, err)
		}
	}
	return bus, counts
}

// TestIntegrationSendClosesTheLoop drives a real agent.Agent run whose
// one gated step resolves through an AckWait built from dispatch.Send,
// against a live dispatch.Endpoint. It proves the endpoint answers the
// client's opened loop and that both sides' buses see
// MessageDeliveredEvent and MessageAckedEvent.
func TestIntegrationSendClosesTheLoop(t *testing.T) {
	senderID, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() error: %v", err)
	}
	roomID := "integration-room"
	r, err := room.New(roomID, senderID.Signer())
	if err != nil {
		t.Fatalf("room.New() error: %v", err)
	}

	serverBus, serverCounts := countingBus(t)
	endpoint, err := dispatch.New(dispatch.Options{
		ID:   "responder",
		Room: r,
		Resolve: func(context.Context, envelope.Message) (dispatch.Handler, error) {
			return integrationHandler{}, nil
		},
		Bus: serverBus,
	})
	if err != nil {
		t.Fatalf("dispatch.New() error: %v", err)
	}
	srv := httptest.NewServer(endpoint.Handler())
	defer srv.Close()

	ackWait := agent.AckWait(func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
		results, err := dispatch.Send(ctx, srv.URL, []envelope.Message{msg})
		if err != nil {
			return envelope.Ack{}, err
		}
		if len(results) != 1 {
			t.Fatalf("Send() results len = %d, want 1", len(results))
		}
		return results[0].Ack, results[0].Err
	})

	plan, err := flow.New([]flow.Step{
		{ID: "step-a", To: "done", Payload: "dispatch the step"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() error: %v", err)
	}
	m, err := machine.New("start", machine.Transition{From: "start", To: "done", Trigger: "go"})
	if err != nil {
		t.Fatalf("machine.New() error: %v", err)
	}
	a, err := agent.New(senderID, discovery.Card{Name: "Dispatch", Capabilities: []string{"ack"}}, plan)
	if err != nil {
		t.Fatalf("agent.New() error: %v", err)
	}

	clientBus, clientCounts := countingBus(t, flow.StepCompletedEvent, agent.ThreadVerifiedEvent)

	status, _, err := a.Run(context.Background(), "thread-1", m, machine.InOut{}, ackWait, clientBus, nil, roomID, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if status != machine.Status("done") {
		t.Fatalf("Run() status = %q, want %q", status, "done")
	}

	clientCounts.mu.Lock()
	defer clientCounts.mu.Unlock()
	if clientCounts.delivered != 1 {
		t.Fatalf("client MessageDeliveredEvent count = %d, want 1", clientCounts.delivered)
	}
	if clientCounts.acked != 1 {
		t.Fatalf("client MessageAckedEvent count = %d, want 1", clientCounts.acked)
	}

	serverCounts.mu.Lock()
	defer serverCounts.mu.Unlock()
	if serverCounts.delivered != 1 {
		t.Fatalf("server MessageDeliveredEvent count = %d, want 1", serverCounts.delivered)
	}
	if serverCounts.acked != 1 {
		t.Fatalf("server MessageAckedEvent count = %d, want 1", serverCounts.acked)
	}
}

// integrationHandler restates the payload for the integration test.
type integrationHandler struct{}

func (integrationHandler) Handle(_ context.Context, m envelope.Message) (string, error) {
	return "responder saw: " + strings.TrimSpace(m.Payload), nil
}
