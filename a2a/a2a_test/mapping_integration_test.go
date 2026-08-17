package a2a_test

import (
	"crypto/ed25519"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// TestSignedMessageRoundTripsThroughPart signs a message, maps it to a
// Mapped value with ToPart, maps it back with FromPart, then verifies
// the signature on the result. It proves every field, including the
// signature, survives the a2a boundary, and that ThreadID round-trips
// through ContextID and ID round-trips through MessageID intact.
func TestSignedMessageRoundTripsThroughPart(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	original := envelope.Message{
		Version:     envelope.Version,
		ID:          "msg-integration-1",
		Room:        "platform-team",
		ThreadID:    "thread-integration-1",
		To:          []string{"agent-a", "agent-b"},
		Intent:      envelope.IntentAssert,
		Epistemic:   envelope.EpistemicVerified,
		Confidence:  0.9,
		ContextRefs: []string{envelope.ContextRef("shared context")},
		Provenance: envelope.Provenance{
			Source:   "tool:grep",
			Chain:    []string{"agent-a"},
			Evidence: []string{envelope.ContextRef("evidence blob")},
		},
		MaxHops:    3,
		CostBudget: 2000,
		Payload:    "The config loader reads mivia.toml.",
	}

	signed, err := envelope.Sign(key, original)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	mapped, err := a2a.ToPart(signed)
	if err != nil {
		t.Fatalf("ToPart: %v", err)
	}
	if mapped.ContextID != signed.ThreadID {
		t.Fatalf("ContextID = %q, want %q", mapped.ContextID, signed.ThreadID)
	}
	if mapped.MessageID != signed.ID {
		t.Fatalf("MessageID = %q, want %q", mapped.MessageID, signed.ID)
	}

	got, err := a2a.FromPart(mapped)
	if err != nil {
		t.Fatalf("FromPart: %v", err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	if got.ThreadID != signed.ThreadID {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, signed.ThreadID)
	}
	if got.ID != signed.ID {
		t.Fatalf("ID = %q, want %q", got.ID, signed.ID)
	}
	if !reflect.DeepEqual(got, signed) {
		t.Fatalf("round-tripped message differs from the original:\ngot:  %+v\nwant: %+v", got, signed)
	}
}
