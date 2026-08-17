package envelope

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Sign returns m with Signer (hex public key) and Signature set. The signed
// bytes are the canonical JSON of m with Signature cleared, so any field
// change after signing breaks VerifySignature.
func Sign(key ed25519.PrivateKey, m Message) (Message, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Message{}, fmt.Errorf("key length %d, want %d", len(key), ed25519.PrivateKeySize)
	}
	// A valid-length PrivateKey always exposes a PublicKey, so the length
	// check covers the format; the assertion only converts the type.
	pub, _ := key.Public().(ed25519.PublicKey)
	m.Signer = hex.EncodeToString(pub)
	m.Signature = ""
	data, err := json.Marshal(m)
	if err != nil {
		return Message{}, fmt.Errorf("marshal for signing: %w", err)
	}
	m.Signature = hex.EncodeToString(ed25519.Sign(key, data))
	return m, nil
}

// VerifySignature authenticates m against its embedded Signer key. An
// unsigned or tampered message fails. Format rules are in Validate; this
// checks the cryptography. Trust policy (which signers to accept) belongs
// to the caller.
func (m Message) VerifySignature() error {
	if m.Signer == "" || m.Signature == "" {
		return errors.New("message is unsigned")
	}
	pub, err := hex.DecodeString(m.Signer)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("signer is not a valid ed25519 public key")
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature is not %d hex-encoded bytes", ed25519.SignatureSize)
	}
	signed := m
	signed.Signature = ""
	data, err := json.Marshal(signed) // same canonical form Sign used
	if err != nil {
		return fmt.Errorf("marshal for verify: %w", err)
	}
	if !ed25519.Verify(pub, data, sig) {
		return errors.New("signature does not match message content")
	}
	return nil
}
