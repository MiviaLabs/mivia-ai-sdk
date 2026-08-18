// Package identity owns one agent key: an ed25519 pair, the key-file
// load, the invariant check, and the hex signer string. Sign wraps
// envelope.Sign. See envelope/ for the wire format and
// ../docs/plans/identity.md for the contract.
package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// Sentinel errors for identity operations; test with errors.Is.
var (
	ErrKeyFormat  = errors.New("malformed key file")
	ErrKeyInvalid = errors.New("identity key breaks an invariant")
)

// Identity holds one agent's ed25519 key pair. Build with New or Load.
// Validate enforces the invariants.
type Identity struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

// New generates a fresh key pair. The GenerateKey error path is
// trusted but untested: it fires only when the OS entropy source
// itself is broken, and no caller needs an injectable source today.
func New() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return &Identity{PublicKey: pub, PrivateKey: priv}, nil
}

// Load reads a key file: exactly 128 lowercase hex chars decoding to
// the 64-byte private key, plus one optional trailing newline. Every
// other content wraps ErrKeyFormat. A read failure wraps the OS error
// and is no sentinel. The public key derives from the private key.
func Load(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	s := strings.TrimSuffix(string(data), "\n")
	if len(s) != ed25519.PrivateKeySize*2 {
		return nil, fmt.Errorf("%w: %s", ErrKeyFormat, path)
	}
	raw, err := hex.DecodeString(s)
	// The re-encode check rejects uppercase, which DecodeString accepts.
	if err != nil || hex.EncodeToString(raw) != s {
		return nil, fmt.Errorf("%w: %s", ErrKeyFormat, path)
	}
	priv := ed25519.PrivateKey(raw)
	pub, _ := priv.Public().(ed25519.PublicKey) // length checked above
	id := &Identity{PublicKey: pub, PrivateKey: priv}
	// Defensive: PublicKey is derived from priv above, so this cannot
	// fail for any id built here. It stays in case a future edit
	// changes how id is constructed.
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return id, nil
}

// Validate checks the key invariants: the private key is exactly
// ed25519.PrivateKeySize bytes and the public key equals the
// seed-derived key. A broken invariant wraps ErrKeyInvalid. A
// zero-value Identity fails.
func (i *Identity) Validate() error {
	if len(i.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: private key length %d, want %d",
			ErrKeyInvalid, len(i.PrivateKey), ed25519.PrivateKeySize)
	}
	derived := ed25519.NewKeyFromSeed(i.PrivateKey[:32])
	wantPub := derived[32:]
	if !ed25519.PublicKey(wantPub).Equal(i.PublicKey) {
		return fmt.Errorf("%w: public key does not match the seed-derived key", ErrKeyInvalid)
	}
	if !ed25519.PublicKey(wantPub).Equal(ed25519.PublicKey(i.PrivateKey[32:])) {
		return fmt.Errorf("%w: embedded public half does not match the seed-derived key", ErrKeyInvalid)
	}
	return nil
}

// Sign validates the identity, then wraps envelope.Sign: value in,
// signed copy out. It never logs the private key.
func (i *Identity) Sign(m envelope.Message) (envelope.Message, error) {
	if err := i.Validate(); err != nil {
		return envelope.Message{}, err
	}
	return envelope.Sign(i.PrivateKey, m)
}

// Signer returns the hex public key derived from the private key, the
// same form envelope.Sign writes into Message.Signer. Deriving keeps
// one source of truth; the exported PublicKey field can diverge.
// Signer returns "" when the private key is not
// ed25519.PrivateKeySize bytes; the length guard comes first because
// key.Public() slices key[32:].
func (i *Identity) Signer() string {
	if i == nil {
		return ""
	}
	if len(i.PrivateKey) != ed25519.PrivateKeySize {
		return ""
	}
	pub, _ := i.PrivateKey.Public().(ed25519.PublicKey) // length checked above
	return hex.EncodeToString(pub)
}
