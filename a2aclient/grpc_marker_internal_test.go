package a2aclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	a2acore "github.com/a2aproject/a2a-go/a2a"
)

// signedRawPayload signs an envelope carrying the given payload and
// encodes it, exactly as a2a.ToPart does for a real Send.
func signedRawPayload(t *testing.T, payload string) []byte {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signed, err := envelope.Sign(key, envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		MaxHops:    3,
		Payload:    payload,
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	mapped, err := a2a.ToPart(signed)
	if err != nil {
		t.Fatalf("ToPart: %v", err)
	}
	return mapped.Part.Data
}

// TestProtoHopKeepsPrefixedPayloadString pins the restore symmetry:
// restoreNumbers may rewrite only the marker strings encodeInexactNumbers
// mints, which come from lossy json.Number values. A payload string the
// sender wrote with the marker prefix is user content. Rewriting it to a
// number breaks the decode of a correctly signed result.
func TestProtoHopKeepsPrefixedPayloadString(t *testing.T) {
	payload := numPrefix + "123"
	raw := signedRawPayload(t, payload)
	data, err := dataFromRaw(raw)
	if err != nil {
		t.Fatalf("dataFromRaw: %v", err)
	}
	if s, ok := data["payload"].(string); !ok || s != payload {
		t.Fatalf("payload after encode = %T %v, want the string %q", data["payload"], data["payload"], payload)
	}
	got := fakeProtoHop(t, data)
	back, err := dataFromParts(a2acore.ContentParts{a2acore.DataPart{Data: got}})
	if err != nil {
		t.Fatalf("dataFromParts: %v", err)
	}
	msg, err := a2a.FromPart(a2a.Mapped{
		Part:      a2a.Part{Data: back},
		ContextID: "thread-1",
		MessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("FromPart: %v: the payload string lost its string form", err)
	}
	if msg.Payload != payload {
		t.Fatalf("Payload = %q, want %q", msg.Payload, payload)
	}
	if err := msg.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature: %v: the round trip altered signed bytes", err)
	}
}
