package envelope

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestSignVerifyRoundTrip(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("signed message rejected by Validate: %v", err)
	}
	if err := m.VerifySignature(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	m.Payload = "Forged claim."
	if err := m.VerifySignature(); err == nil {
		t.Fatal("tampered payload must fail verification")
	}
}

func TestVerifyDetectsMetadataTampering(t *testing.T) {
	cases := map[string]func(*Message){
		"to":        func(m *Message) { m.To = []string{"agent-b"} },
		"max hops":  func(m *Message) { m.MaxHops = 3 },
		"epistemic": func(m *Message) { m.Epistemic = EpistemicAssumed },
		"prev hash": func(m *Message) { m.PrevHash = ContextRef("forged parent") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := Sign(testKey(t), validMessage())
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			mutate(&m)
			if err := m.VerifySignature(); err == nil {
				t.Fatal("tampered metadata must fail verification")
			}
		})
	}
}

func TestVerifySignatureReturnsMarshalError(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// NaN fails json.Marshal; the error must surface, not verify over nil.
	m.Confidence = math.NaN()
	var marshalErr *json.UnsupportedValueError
	if err := m.VerifySignature(); !errors.As(err, &marshalErr) {
		t.Fatalf("err = %v, want the json marshal error", err)
	}
}

func TestVerifyRejectsUnsigned(t *testing.T) {
	if err := validMessage().VerifySignature(); err == nil {
		t.Fatal("unsigned message must fail verification")
	}
}

func TestSignRejectsBadKey(t *testing.T) {
	if _, err := Sign(ed25519.PrivateKey("short"), validMessage()); err == nil {
		t.Fatal("bad key length must fail")
	}
}

func TestSignRejectsUnserializableMessage(t *testing.T) {
	m := validMessage()
	m.Confidence = math.NaN()
	var marshalErr *json.UnsupportedValueError
	if _, err := Sign(testKey(t), m); !errors.As(err, &marshalErr) {
		t.Fatalf("err = %v, want the json marshal error", err)
	}
}

func TestVerifyRejectsMalformedSigner(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	m.Signer = "zz" // not a hex-encoded ed25519 public key
	if err := m.VerifySignature(); err == nil {
		t.Fatal("malformed signer must fail verification")
	}
}

func TestVerifyRejectsWrongLengthHexSigner(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	m.Signer = "aabb" // valid hex, but 2 bytes, not the 32-byte public key
	if err := m.VerifySignature(); err == nil {
		t.Fatal("wrong-length signer must fail verification")
	}
}

func TestVerifyRejectsBadSignatureLength(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	m.Signature = "aabb" // far short of a 64-byte signature
	if err := m.VerifySignature(); err == nil {
		t.Fatal("short signature must fail verification")
	}
}

func TestVerifyRejectsUndecodableSignature(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	m.Signature = "zz" // not hex; fails hex.DecodeString itself
	if err := m.VerifySignature(); err == nil {
		t.Fatal("undecodable signature must fail verification")
	}
}

func TestSignatureSurvivesWire(t *testing.T) {
	m, err := Sign(testKey(t), validMessage())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("verify after wire round trip: %v", err)
	}
}
