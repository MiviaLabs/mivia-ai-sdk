package identity_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// testMessage builds a small valid message. Room stays empty here; the
// room case sets it.
func testMessage() envelope.Message {
	return envelope.Message{
		Version:    envelope.Version,
		ID:         "id-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicInferred,
		Confidence: 0.5,
		Provenance: envelope.Provenance{Source: "model:self"},
		Payload:    "The build is green.",
	}
}

// TestEnvelopeRoundTrip signs, encodes, decodes, and verifies. A tamper
// after signing must break verification.
func TestEnvelopeRoundTrip(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	signed, err := id.Sign(testMessage())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	data, err := signed.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := envelope.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}

	tampered := got
	tampered.Payload = "The build is red."
	if err := tampered.VerifySignature(); err == nil {
		t.Fatal("tampered message passed verification")
	}
}

// TestSignerEqualityAfterLoad pins the canonical signer form: Signer()
// and the signed message's Signer field derive from the same key bytes.
func TestSignerEqualityAfterLoad(t *testing.T) {
	id, err := identity.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	signed, err := id.Sign(testMessage())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Signer != id.Signer() {
		t.Fatalf("message signer = %q, Signer() = %q", signed.Signer, id.Signer())
	}
}

// TestRoomAdmission admits a member's signed message and rejects a
// signer outside the roster. The signer string is the room member id.
func TestRoomAdmission(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r, err := room.New("ops", id.Signer())
	if err != nil {
		t.Fatalf("new room: %v", err)
	}

	m := testMessage()
	m.Room = r.ID()
	signed, err := id.Sign(m)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := r.Accepts(signed); err != nil {
		t.Fatalf("member message rejected: %v", err)
	}

	outsider, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.ID = "id-2"
	forged, err := outsider.Sign(m)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := r.Accepts(forged); !errors.Is(err, room.ErrNotMember) {
		t.Fatalf("outsider err = %v, want %v", err, room.ErrNotMember)
	}
}
