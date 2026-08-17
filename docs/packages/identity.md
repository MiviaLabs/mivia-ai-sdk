# Package reference: identity

The identity package owns one agent key: an ed25519 pair, the key-file
load, the invariant check, and the hex signer string. It signs
envelopes through `envelope.Sign`; it never calls `ed25519.Sign`
directly.
See [architecture.md](../architecture.md) for the flow. The
exported surface below mirrors `api/identity.txt`.

## Types

- `Identity` — one agent key pair. Fields: `PublicKey`,
  `PrivateKey` (both ed25519).
- `ErrKeyFormat` — the sentinel for a malformed key file.
- `ErrKeyInvalid` — the sentinel for a key that breaks an invariant.

## Functions and methods

- `New()` — generates a fresh key pair.
- `Load(path)` — reads a key file and derives the public key.
- `Identity.Validate()` — the invariant check; Load and Sign call it.
- `Identity.Sign(m)` — validates, then wraps `envelope.Sign`.
- `Identity.Signer()` — the hex public key derived from the private
  key. It matches the `signer` field `envelope.Sign` writes.

## Key file format

- The file holds exactly 128 lowercase hex characters.
- The characters decode to the 64-byte ed25519 private key.
- One optional trailing newline is allowed.
- Every other content is malformed; Load wraps `ErrKeyFormat`.
- A read failure wraps the OS error and carries no sentinel.
- File permissions stay with the caller.

## Invariants

`Validate` enforces both rules below. Load and Sign call it first.

- The private key is exactly `ed25519.PrivateKeySize` bytes.
- The public key equals the seed-derived key.

A zero-value Identity fails `Validate`. `Signer` returns an empty
string for a wrong-length private key; the length guard runs before
`key.Public()`, which would panic on a short key.

## Usage

```go
id, _ := identity.New()
msg := envelope.Message{
    Version:    envelope.Version,
    ID:         "msg-1",
    ThreadID:   "task-42",
    Intent:     envelope.IntentAssert,
    Epistemic:  envelope.EpistemicInferred,
    Provenance: envelope.Provenance{Source: "model:self"},
    Payload:    "The build is green.",
}
signed, _ := id.Sign(msg)
_ = signed.VerifySignature()
memberID := id.Signer() // the room member id
```
