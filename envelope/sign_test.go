package envelope

import (
	"crypto/ed25519"
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
