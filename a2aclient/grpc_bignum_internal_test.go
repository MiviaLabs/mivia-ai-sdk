package a2aclient

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb/pbconv"
)

// fakeProtoHop runs the request's message through the same pbconv
// conversion pair grpcTransport.Send uses on the wire, and returns
// the response-side DataPart payload. It reproduces the structpb hop
// that rounds integers, with no live network.
func fakeProtoHop(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	msg := &a2acore.Message{
		ID:    a2acore.NewMessageID(),
		Role:  a2acore.MessageRoleUser,
		Parts: a2acore.ContentParts{a2acore.DataPart{Data: data}},
	}
	req, err := pbconv.ToProtoSendMessageRequest(&a2acore.MessageSendParams{Message: msg})
	if err != nil {
		t.Fatalf("ToProtoSendMessageRequest: %v", err)
	}
	back, err := pbconv.FromProtoSendMessageRequest(req)
	if err != nil {
		t.Fatalf("FromProtoSendMessageRequest: %v", err)
	}
	for _, p := range back.Message.Parts {
		if dp, ok := p.(a2acore.DataPart); ok {
			return dp.Data
		}
	}
	t.Fatal("proto hop dropped the data part")
	return nil
}

// signedRawMessage signs an envelope with the given max_hops and
// encodes it, exactly as a2a.ToPart does for a real Send.
func signedRawMessage(t *testing.T, maxHops int) json.RawMessage {
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
		MaxHops:    maxHops,
		Payload:    "hop fidelity",
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

// TestProtoHopKeepsLargeIntegersByteExact pins the transport's
// integer-fidelity contract: an envelope integer above 2^53 must
// survive the proto struct hop unchanged, or the remote's signature
// check fails on a legitimately signed message. Against a transport
// that lets structpb round the value, the value decodes differently
// and verification fails.
func TestProtoHopKeepsLargeIntegersByteExact(t *testing.T) {
	raw := signedRawMessage(t, 9007199254740993)
	data, err := dataFromRaw(raw)
	if err != nil {
		t.Fatalf("dataFromRaw: %v", err)
	}
	got := fakeProtoHop(t, data)
	back, err := dataFromParts(a2acore.ContentParts{a2acore.DataPart{Data: got}})
	if err != nil {
		t.Fatalf("dataFromParts: %v", err)
	}
	msg, err := a2a.FromPart(a2a.Mapped{Part: a2a.Part{Data: back}, ContextID: "thread-1", MessageID: "msg-1"})
	if err != nil {
		t.Fatalf("FromPart: %v", err)
	}
	if err := msg.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature: %v: the hop rounded a signed integer", err)
	}
	if msg.MaxHops != 9007199254740993 {
		t.Fatalf("MaxHops = %d, want 9007199254740993", msg.MaxHops)
	}
}

// TestProtoHopLeavesSmallIntegersAsNumbers pins the minimal-encoding
// rule: a number float64 holds exactly crosses the hop as a number,
// not as a marker string.
func TestProtoHopLeavesSmallIntegersAsNumbers(t *testing.T) {
	raw := signedRawMessage(t, 3)
	data, err := dataFromRaw(raw)
	if err != nil {
		t.Fatalf("dataFromRaw: %v", err)
	}
	if _, ok := data["max_hops"].(json.Number); !ok {
		t.Fatalf("max_hops = %T, want json.Number before the hop", data["max_hops"])
	}
	got := fakeProtoHop(t, data)
	back, err := dataFromParts(a2acore.ContentParts{a2acore.DataPart{Data: got}})
	if err != nil {
		t.Fatalf("dataFromParts: %v", err)
	}
	if !bytes.Contains(back, []byte(`"max_hops":3`)) {
		t.Fatalf("round trip = %s, want a plain numeric max_hops of 3", back)
	}
	if strings.Contains(string(back), numPrefix) {
		t.Fatalf("round trip = %s, want no marker prefix on an exactly representable number", back)
	}
}
