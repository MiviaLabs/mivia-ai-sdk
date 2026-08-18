package dispatch

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// echoHandler restates the message payload it receives.
type echoHandler struct{}

func (echoHandler) Handle(_ context.Context, m envelope.Message) (string, error) {
	return "echo:" + m.Payload, nil
}

// TestProcessLine_EmitIsBestEffort proves EmitMessageDelivered and
// EmitMessageAcked never fail-fast the ladder. It builds an Endpoint
// around a bus with no subscribers for either event name, bypassing
// New so bus.Emit returns an unsubscribed-name error at both emit
// points, then asserts processLine still answers a confirmed ack.
func TestProcessLine_EmitIsBestEffort(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	probe, err := envelope.Sign(priv, envelope.Message{
		Version:    envelope.Version,
		ID:         "probe",
		ThreadID:   "probe",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "probe",
	})
	if err != nil {
		t.Fatalf("sign probe: %v", err)
	}
	_ = pub

	r, err := room.New("room-1", probe.Signer)
	if err != nil {
		t.Fatalf("room.New() error: %v", err)
	}

	m, err := envelope.Sign(priv, envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		Room:       "room-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "hello",
	})
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}
	line, err := m.Encode()
	if err != nil {
		t.Fatalf("encode message: %v", err)
	}

	e := &Endpoint{
		id:   "endpoint-1",
		room: r,
		resolve: func(context.Context, envelope.Message) (Handler, error) {
			return echoHandler{}, nil
		},
		bus: events.New(),
	}

	out := e.processLine(context.Background(), line)

	ack, err := envelope.DecodeAck(out)
	if err != nil {
		t.Fatalf("processLine did not answer a confirmed ack despite the bus having no emit subscribers: decode error %v, line %q", err, out)
	}
	if ack.From != e.id {
		t.Errorf("ack.From = %q, want %q", ack.From, e.id)
	}
	if ack.Status != envelope.AckConfirmed {
		t.Errorf("ack.Status = %q, want confirmed", ack.Status)
	}
}
