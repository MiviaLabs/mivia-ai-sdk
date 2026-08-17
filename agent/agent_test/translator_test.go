// Package agent_test also holds the red-green unit cases for the
// envelope-to-events translator: EmitMessageDelivered,
// EmitMessageAcked, EmitThreadVerified. Each case asserted first
// against the empty translator, then went green once translator.go
// implemented the behavior.
package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
)

// recorder captures every event a handler receives, guarded by a
// mutex so the race detector stays quiet across goroutines.
type recorder struct {
	mu    sync.Mutex
	names []events.Name
	data  []string
}

func (r *recorder) handler(_ context.Context, e events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = append(r.names, e.Name)
	r.data = append(r.data, e.Data)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.names)
}

func (r *recorder) last() (events.Name, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.names)
	if n == 0 {
		return "", ""
	}
	return r.names[n-1], r.data[n-1]
}

// baseMessage returns a Message that passes Validate but carries no
// signature.
func baseMessage(id string) envelope.Message {
	return envelope.Message{
		Version:    envelope.Version,
		ID:         id,
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicInferred,
		Confidence: 0.6,
		Provenance: envelope.Provenance{Source: "model:self"},
		Payload:    "The API returns JSON.",
	}
}

// signedMessage returns a Message signed by a fresh Identity.
func signedMessage(t *testing.T, id string) envelope.Message {
	t.Helper()
	ident := newIdentity(t)
	m, err := ident.Sign(baseMessage(id))
	if err != nil {
		t.Fatalf("Sign() unexpected error: %v", err)
	}
	return m
}

// badSignatureMessage returns a signed Message tampered after
// signing, so VerifySignature must reject it.
func badSignatureMessage(t *testing.T) envelope.Message {
	t.Helper()
	m := signedMessage(t, "msg-bad-sig")
	m.Payload = "a different payload, not what was signed"
	return m
}

// validAck returns an Ack that passes Validate.
func validAck(t *testing.T) envelope.Ack {
	t.Helper()
	a, err := envelope.NewAck(baseMessage("msg-1"), "receiver-key", "I will do X")
	if err != nil {
		t.Fatalf("NewAck() unexpected error: %v", err)
	}
	return a
}

// blankMessageIDAck returns an Ack whose MessageID fails Validate.
func blankMessageIDAck() envelope.Ack {
	return envelope.Ack{
		MessageID:   "",
		From:        "receiver-key",
		Restatement: "I will do X",
		Status:      envelope.AckPending,
	}
}

// validThread returns a two-message thread that passes VerifyThread.
func validThread() []envelope.Message {
	m1 := baseMessage("t1")
	m2 := baseMessage("t2")
	m2.PrevHash = m1.Hash()
	return []envelope.Message{m1, m2}
}

// brokenThread returns a two-message thread whose PrevHash does not
// match its parent's Hash, so VerifyThread must reject it.
func brokenThread() []envelope.Message {
	m1 := baseMessage("t1")
	m2 := baseMessage("t2")
	m2.PrevHash = envelope.ContextRef("not the real previous hash")
	return []envelope.Message{m1, m2}
}

// TestEmitMessageDeliveredSuccess proves a valid, signed Message
// emits MessageDeliveredEvent to a subscribed handler.
func TestEmitMessageDeliveredSuccess(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.MessageDeliveredEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	m := signedMessage(t, "msg-ok")

	if err := agent.EmitMessageDelivered(context.Background(), bus, m); err != nil {
		t.Fatalf("EmitMessageDelivered() unexpected error: %v", err)
	}
	if got := r.count(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	name, data := r.last()
	if name != agent.MessageDeliveredEvent {
		t.Fatalf("event name = %q, want %q", name, agent.MessageDeliveredEvent)
	}
	if !strings.Contains(data, m.ID) {
		t.Fatalf("event data = %q, want it to contain %q", data, m.ID)
	}
}

// TestEmitMessageDeliveredBadSignature proves a tampered Message
// returns the VerifySignature error and runs no handler.
func TestEmitMessageDeliveredBadSignature(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.MessageDeliveredEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	m := badSignatureMessage(t)
	wantErr := m.VerifySignature()
	if wantErr == nil {
		t.Fatal("badSignatureMessage() verified cleanly, want a verification error")
	}

	err := agent.EmitMessageDelivered(context.Background(), bus, m)
	if err == nil {
		t.Fatal("EmitMessageDelivered() returned a nil error, want the verification error")
	}
	if err.Error() != wantErr.Error() {
		t.Fatalf("EmitMessageDelivered() error = %q, want %q", err.Error(), wantErr.Error())
	}
	if got := r.count(); got != 0 {
		t.Fatalf("handler ran %d times, want 0", got)
	}
}

// TestEmitMessageAckedSuccess proves a valid Ack emits
// MessageAckedEvent to a subscribed handler.
func TestEmitMessageAckedSuccess(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.MessageAckedEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	a := validAck(t)

	if err := agent.EmitMessageAcked(context.Background(), bus, a); err != nil {
		t.Fatalf("EmitMessageAcked() unexpected error: %v", err)
	}
	if got := r.count(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	name, data := r.last()
	if name != agent.MessageAckedEvent {
		t.Fatalf("event name = %q, want %q", name, agent.MessageAckedEvent)
	}
	if !strings.Contains(data, a.MessageID) || !strings.Contains(data, string(a.Status)) {
		t.Fatalf("event data = %q, want it to contain %q and %q", data, a.MessageID, a.Status)
	}
}

// TestEmitMessageAckedBlankMessageID proves an Ack with a blank
// MessageID returns the Validate error and runs no handler.
func TestEmitMessageAckedBlankMessageID(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.MessageAckedEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	a := blankMessageIDAck()
	wantErr := a.Validate()
	if wantErr == nil {
		t.Fatal("blankMessageIDAck() validated cleanly, want a validation error")
	}

	err := agent.EmitMessageAcked(context.Background(), bus, a)
	if err == nil {
		t.Fatal("EmitMessageAcked() returned a nil error, want the validation error")
	}
	if err.Error() != wantErr.Error() {
		t.Fatalf("EmitMessageAcked() error = %q, want %q", err.Error(), wantErr.Error())
	}
	if got := r.count(); got != 0 {
		t.Fatalf("handler ran %d times, want 0", got)
	}
}

// TestEmitThreadVerifiedSuccess proves a verifying thread emits
// ThreadVerifiedEvent to a subscribed handler.
func TestEmitThreadVerifiedSuccess(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.ThreadVerifiedEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	msgs := validThread()

	if err := agent.EmitThreadVerified(context.Background(), bus, msgs); err != nil {
		t.Fatalf("EmitThreadVerified() unexpected error: %v", err)
	}
	if got := r.count(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	name, data := r.last()
	if name != agent.ThreadVerifiedEvent {
		t.Fatalf("event name = %q, want %q", name, agent.ThreadVerifiedEvent)
	}
	if data != "thread of 2 messages verified" {
		t.Fatalf("event data = %q, want %q", data, "thread of 2 messages verified")
	}
}

// TestEmitThreadVerifiedBrokenChain proves a thread with a broken
// hash chain returns the VerifyThread error and runs no handler.
func TestEmitThreadVerifiedBrokenChain(t *testing.T) {
	bus := events.New()
	r := &recorder{}
	if err := bus.Subscribe(agent.ThreadVerifiedEvent, r.handler); err != nil {
		t.Fatalf("Subscribe() unexpected error: %v", err)
	}
	msgs := brokenThread()
	wantErr := envelope.VerifyThread(msgs)
	if wantErr == nil {
		t.Fatal("brokenThread() verified cleanly, want a verification error")
	}

	err := agent.EmitThreadVerified(context.Background(), bus, msgs)
	if err == nil {
		t.Fatal("EmitThreadVerified() returned a nil error, want the verification error")
	}
	if err.Error() != wantErr.Error() {
		t.Fatalf("EmitThreadVerified() error = %q, want %q", err.Error(), wantErr.Error())
	}
	if got := r.count(); got != 0 {
		t.Fatalf("handler ran %d times, want 0", got)
	}
}

// TestEmitNilBusReturnsErrNoBus proves every EmitX function returns
// ErrNoBus when its bus argument is nil, and none panics.
func TestEmitNilBusReturnsErrNoBus(t *testing.T) {
	cases := []struct {
		name string
		run  func() error
	}{
		{"EmitMessageDelivered", func() error {
			return agent.EmitMessageDelivered(context.Background(), nil, signedMessage(t, "msg-nil-bus"))
		}},
		{"EmitMessageAcked", func() error {
			return agent.EmitMessageAcked(context.Background(), nil, validAck(t))
		}},
		{"EmitThreadVerified", func() error {
			return agent.EmitThreadVerified(context.Background(), nil, validThread())
		}},
		{"EmitMessageDeliveredInvalidEnvelope", func() error {
			return agent.EmitMessageDelivered(context.Background(), nil, badSignatureMessage(t))
		}},
		{"EmitMessageAckedInvalidEnvelope", func() error {
			return agent.EmitMessageAcked(context.Background(), nil, blankMessageIDAck())
		}},
		{"EmitThreadVerifiedInvalidEnvelope", func() error {
			return agent.EmitThreadVerified(context.Background(), nil, brokenThread())
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, agent.ErrNoBus) {
				t.Fatalf("%s() error = %v, want errors.Is match for ErrNoBus", tt.name, err)
			}
		})
	}
}

// TestEmitNoSubscriberReturnsBusError proves each EmitX function
// surfaces events.Bus.Emit's own "no subscriber" error, unwrapped,
// when nothing subscribed to its event name.
func TestEmitNoSubscriberReturnsBusError(t *testing.T) {
	cases := []struct {
		name string
		run  func(bus *events.Bus) error
	}{
		{"EmitMessageDelivered", func(bus *events.Bus) error {
			return agent.EmitMessageDelivered(context.Background(), bus, signedMessage(t, "msg-no-sub"))
		}},
		{"EmitMessageAcked", func(bus *events.Bus) error {
			return agent.EmitMessageAcked(context.Background(), bus, validAck(t))
		}},
		{"EmitThreadVerified", func(bus *events.Bus) error {
			return agent.EmitThreadVerified(context.Background(), bus, validThread())
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bus := events.New()
			err := tt.run(bus)
			if err == nil {
				t.Fatal("expected the bus's no-subscriber error, got nil")
			}
			if !strings.Contains(err.Error(), "no subscriber") {
				t.Fatalf("%s() error = %q, want it to contain %q", tt.name, err.Error(), "no subscriber")
			}
		})
	}
}
