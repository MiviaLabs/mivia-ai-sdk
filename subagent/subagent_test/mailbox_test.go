package subagent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
)

// TestMailboxDeliverTakeFull drives the mailbox contract: validate on
// deliver, drain on take, and the full sentinel.
func TestMailboxDeliverTakeFull(t *testing.T) {
	box, err := subagent.NewMailbox(1)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	if err := box.Deliver(badMessage()); err == nil {
		t.Fatal("Deliver accepted an invalid message")
	}
	if err := box.Deliver(signedMessage(t, "first")); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if err := box.Deliver(signedMessage(t, "second")); !errors.Is(err, subagent.ErrMailboxFull) {
		t.Fatalf("Deliver full = %v, want ErrMailboxFull", err)
	}
	msgs := box.Take()
	if len(msgs) != 1 || msgs[0].Payload != "first" {
		t.Fatalf("Take = %d msgs, want the first", len(msgs))
	}
	if got := box.Take(); got != nil {
		t.Fatalf("Take after drain = %d msgs, want none", len(got))
	}
}

// TestSendToolSignsAndDelivers proves a send lands a signed message
// in the bound mailbox and reports its id.
func TestSendToolSignsAndDelivers(t *testing.T) {
	ctx := context.Background()
	box, _ := subagent.NewMailbox(4)
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	out, err := subagent.SendTool("to-worker", box, id).Run(ctx, inString("hello sub"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := box.Take()
	if len(msgs) != 1 {
		t.Fatalf("mailbox = %d msgs, want 1", len(msgs))
	}
	if msgs[0].Payload != "hello sub" || msgs[0].ID != out.Value {
		t.Fatalf("message = %s/%s, want hello sub and %v", msgs[0].Payload, msgs[0].ID, out.Value)
	}
	if msgs[0].Signature == "" {
		t.Fatal("message carries no signature")
	}
}

// TestInboxToolDrainsPayloads proves the inbox returns pending
// payloads as JSON and the empty marker when nothing waits.
func TestInboxToolDrainsPayloads(t *testing.T) {
	ctx := context.Background()
	box, _ := subagent.NewMailbox(4)
	out, err := subagent.InboxTool("inbox", box).Run(ctx, inString(""))
	if err != nil || out.Value != "empty" {
		t.Fatalf("empty inbox = %v,%v, want the marker", out.Value, err)
	}
	if err := box.Deliver(signedMessage(t, "one")); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if err := box.Deliver(signedMessage(t, "two")); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	out, err = subagent.InboxTool("inbox", box).Run(ctx, inString(""))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "one,two" {
		t.Fatalf("payloads = %v, want one,two", out.Value)
	}
	if after := box.Take(); after != nil {
		t.Fatalf("inbox drained twice = %d msgs", len(after))
	}
}

// TestNewMailboxRejectsBadCapacity proves a non-positive capacity
// fails at construction, for both zero and negative values.
func TestNewMailboxRejectsBadCapacity(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := subagent.NewMailbox(tt.capacity)
			if err == nil {
				t.Fatalf("NewMailbox(%d) error = nil, want error", tt.capacity)
			}
			if !errors.Is(err, subagent.ErrInvalidCapacity) {
				t.Fatalf("NewMailbox(%d) error = %v, want errors.Is ErrInvalidCapacity", tt.capacity, err)
			}
			if !strings.Contains(err.Error(), "capacity") {
				t.Fatalf("err = %v, want the capacity fault", err)
			}
		})
	}
}

// TestDeliverRejectsUnsignedMessage proves a message that passes
// Validate with no signer and no signature never enters the mailbox.
func TestDeliverRejectsUnsignedMessage(t *testing.T) {
	box, err := subagent.NewMailbox(4)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	msg := unsignedMessage("plain")
	if vErr := msg.Validate(); vErr != nil {
		t.Fatalf("Validate = %v, want nil for an unsigned message", vErr)
	}
	err = box.Deliver(msg)
	if !errors.Is(err, subagent.ErrUnverified) {
		t.Fatalf("Deliver unsigned = %v, want ErrUnverified", err)
	}
	if got := box.Take(); len(got) != 0 {
		t.Fatalf("Take = %d msgs, want none", len(got))
	}
}

// TestDeliverRejectsTamperedSignature proves a payload change after
// signing fails verification, even though the hex format stays valid.
func TestDeliverRejectsTamperedSignature(t *testing.T) {
	box, err := subagent.NewMailbox(4)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	msg := signedMessage(t, "original")
	msg.Payload = "tampered"
	if vErr := msg.Validate(); vErr != nil {
		t.Fatalf("Validate = %v, want nil for a tampered message", vErr)
	}
	err = box.Deliver(msg)
	if !errors.Is(err, subagent.ErrUnverified) {
		t.Fatalf("Deliver tampered = %v, want ErrUnverified", err)
	}
	if got := box.Take(); len(got) != 0 {
		t.Fatalf("Take = %d msgs, want none", len(got))
	}
}

// TestDeliverKeepsSignedMessage proves the new check does not reject a
// correctly signed message.
func TestDeliverKeepsSignedMessage(t *testing.T) {
	box, err := subagent.NewMailbox(4)
	if err != nil {
		t.Fatalf("NewMailbox: %v", err)
	}
	if dErr := box.Deliver(signedMessage(t, "good")); dErr != nil {
		t.Fatalf("Deliver signed = %v, want nil", dErr)
	}
	got := box.Take()
	if len(got) != 1 || got[0].Payload != "good" {
		t.Fatalf("Take = %d msgs, want the signed message", len(got))
	}
}

// signedMessage builds one valid signed message carrying payload.
func signedMessage(t *testing.T, payload string) envelope.Message {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	signed, err := id.Sign(envelope.Message{
		Version:   envelope.Version,
		ID:        "m-" + payload,
		ThreadID:  "mb",
		Intent:    envelope.IntentRequest,
		Epistemic: envelope.EpistemicAssumed,
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return signed
}

// badMessage builds a message that fails Validate.
func badMessage() envelope.Message {
	return envelope.Message{ID: "bad", ThreadID: "mb"}
}

// unsignedMessage builds a message that passes Validate but carries no
// signer and no signature.
func unsignedMessage(payload string) envelope.Message {
	return envelope.Message{
		Version:   envelope.Version,
		ID:        "u-" + payload,
		ThreadID:  "mb",
		Intent:    envelope.IntentRequest,
		Epistemic: envelope.EpistemicAssumed,
		Payload:   payload,
	}
}
