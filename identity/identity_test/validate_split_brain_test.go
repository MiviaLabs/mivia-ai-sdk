package identity_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// TestValidateSplitBrainLoad proves the hardened Validate rejects
// temp key files where the embedded public half does not match the
// seed-derived public key, via the Load path.
func TestValidateSplitBrainLoad(t *testing.T) {
	raw, err := hex.DecodeString("af3efcc115314ff275626220254514aa1e3030e5058e9c6ac59a04ede5d5b4f83341399c9c63d005a66865030f21579cd104b50d099b77e15d9369888865ff33")
	if err != nil {
		t.Fatalf("decode valid key: %v", err)
	}
	seed := make([]byte, ed25519.SeedSize)
	copy(seed, raw[:ed25519.SeedSize])

	cases := []struct {
		name string
		priv []byte // full 64-byte private key to write to temp file
	}{
		{
			name: "zeroed public half",
			priv: func() []byte {
				k := make([]byte, ed25519.PrivateKeySize)
				copy(k[:ed25519.SeedSize], seed)
				return k
			}(),
		},
		{
			name: "flipped seed byte 0",
			priv: func() []byte {
				k := make([]byte, ed25519.PrivateKeySize)
				copy(k, raw)
				k[0] ^= 0xff
				return k
			}(),
		},
		{
			name: "mismatched public half",
			priv: func() []byte {
				k := make([]byte, ed25519.PrivateKeySize)
				copy(k[:ed25519.SeedSize], seed)
				_, otherPub, _ := ed25519.GenerateKey(nil)
				copy(k[ed25519.SeedSize:], otherPub)
				return k
			}(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "key")
			if err := os.WriteFile(path, []byte(hex.EncodeToString(tc.priv)), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, err := identity.Load(path)
			if !errors.Is(err, identity.ErrKeyInvalid) {
				t.Fatalf("Load err = %v, want ErrKeyInvalid", err)
			}
		})
	}
}

// TestValidateSplitBrainDirect proves the hardened Validate rejects
// an Identity constructed directly (not via Load) with a valid
// PrivateKey whose embedded public half differs from the
// seed-derived key.
func TestValidateSplitBrainDirect(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	derived := ed25519.NewKeyFromSeed(priv[:ed25519.SeedSize])
	_, otherPub, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey other: %v", err)
	}
	broken := make([]byte, ed25519.PrivateKeySize)
	copy(broken[:ed25519.SeedSize], priv[:ed25519.SeedSize])
	copy(broken[ed25519.SeedSize:], otherPub)
	id := &identity.Identity{
		PublicKey:  ed25519.PublicKey(derived[32:]),
		PrivateKey: ed25519.PrivateKey(broken),
	}
	if err := id.Validate(); !errors.Is(err, identity.ErrKeyInvalid) {
		t.Fatalf("Validate err = %v, want ErrKeyInvalid", err)
	}
}
