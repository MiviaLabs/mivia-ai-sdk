package identity_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io/fs"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// TestNewGeneratesValidIdentity proves New returns a usable key pair.
func TestNewGeneratesValidIdentity(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := id.Validate(); err != nil {
		t.Fatalf("Validate after New: %v", err)
	}
	if len(id.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("public key length = %d, want %d", len(id.PublicKey), ed25519.PublicKeySize)
	}
	if len(id.PrivateKey) != ed25519.PrivateKeySize {
		t.Fatalf("private key length = %d, want %d", len(id.PrivateKey), ed25519.PrivateKeySize)
	}
	if id.Signer() != hex.EncodeToString(id.PublicKey) {
		t.Fatalf("Signer = %q, want hex of PublicKey", id.Signer())
	}
}

// TestLoadKeyFiles pins the key file format over the testdata fixtures.
// Every malformed fixture must wrap ErrKeyFormat.
func TestLoadKeyFiles(t *testing.T) {
	cases := []struct {
		name    string
		wantErr bool
	}{
		{"valid", false},
		{"valid_newline", false},
		{"empty", true},
		{"seed_form", true},
		{"uppercase", true},
		{"non_hex", true},
		{"crlf", true},
		{"interior_whitespace", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := identity.Load("testdata/" + tc.name)
			if tc.wantErr {
				if !errors.Is(err, identity.ErrKeyFormat) {
					t.Fatalf("err = %v, want ErrKeyFormat", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := id.Validate(); err != nil {
				t.Fatalf("Validate after Load: %v", err)
			}
		})
	}
}

// TestLoadDerivesPublicKey proves the public key comes from the private
// key bytes, and Signer matches it.
func TestLoadDerivesPublicKey(t *testing.T) {
	id, err := identity.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pub, ok := id.PrivateKey.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("private key exposes no ed25519 public key")
	}
	if !pub.Equal(id.PublicKey) {
		t.Fatal("PublicKey is not the private key's public half")
	}
	if id.Signer() != hex.EncodeToString(pub) {
		t.Fatalf("Signer = %q, want hex of derived public key", id.Signer())
	}
}

// TestLoadMissingFile proves a read failure wraps the OS error and
// carries no sentinel.
func TestLoadMissingFile(t *testing.T) {
	_, err := identity.Load("testdata/does_not_exist")
	if err == nil {
		t.Fatal("Load accepted a nonexistent path")
	}
	if errors.Is(err, identity.ErrKeyFormat) {
		t.Fatalf("err = %v wraps ErrKeyFormat; a read failure is no sentinel", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want unwrap to fs.ErrNotExist", err)
	}
}

// TestValidateFailures pins the Validate invariants. Each broken key
// must wrap ErrKeyInvalid.
func TestValidateFailures(t *testing.T) {
	mismatched, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	other, err := identity.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mismatched.PublicKey = other.PublicKey

	cases := map[string]identity.Identity{
		"zero value":        {},
		"short private key": {PrivateKey: make([]byte, ed25519.SeedSize)},
		"oversized private key": {
			PrivateKey: make([]byte, ed25519.PrivateKeySize+ed25519.SeedSize),
		},
		"mismatched pub half": *mismatched,
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if err := id.Validate(); !errors.Is(err, identity.ErrKeyInvalid) {
				t.Fatalf("Validate err = %v, want ErrKeyInvalid", err)
			}
		})
	}
}

// TestSignerLengthGuard proves Signer returns "" for any wrong-length
// private key; key.Public() would panic without the guard.
func TestSignerLengthGuard(t *testing.T) {
	cases := map[string]identity.Identity{
		"zero value":        {},
		"short private key": {PrivateKey: make([]byte, ed25519.SeedSize)},
		"oversized private key": {
			PrivateKey: make([]byte, ed25519.PrivateKeySize+ed25519.SeedSize),
		},
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if got := id.Signer(); got != "" {
				t.Fatalf("Signer = %q, want empty", got)
			}
		})
	}
}

// TestSignValidatesFirst proves Sign refuses a broken key before it
// signs.
func TestSignValidatesFirst(t *testing.T) {
	var id identity.Identity
	if _, err := id.Sign(envelope.Message{}); !errors.Is(err, identity.ErrKeyInvalid) {
		t.Fatalf("Sign err = %v, want ErrKeyInvalid", err)
	}
}
