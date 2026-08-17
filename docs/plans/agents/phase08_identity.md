# Phase 8: identity key wrap

Status: done. Builds the identity block. An identity is an ed25519
key plus its hex signer string. This phase wraps the key so callers
hold one object. See `docs/plans/agents/PHASES.md`.

## Goal

Own an agent key. One object holds the private key and the signer.
The object validates the key form and derives the signer. It loads a
key from a file.

## Scope

Inside: the `Identity` type, the load from a key file, and the signer
derivation. Outside: the agent card, the trust policy, and the
registry. Those belong to later phases.

## API

- `type Identity struct` holding an ed25519 `PublicKey` and a
  `PrivateKey`.
- `New() (*Identity, error)` that generates a fresh key pair.
- `Load(path string) (*Identity, error)` that reads a key file.
- `(*Identity).Validate() error` that checks the key form.
- `(*Identity).Sign(m envelope.Message) (envelope.Message, error)` that
  wraps `envelope.Sign`.
- `(*Identity).Signer() string` that returns the hex public key.

The key file format is pinned. The file holds exactly 128 lowercase
hex characters. Those characters decode to the 64-byte ed25519
private key. One optional trailing newline is allowed. Every other
content is malformed. `Load` derives the public key from the private
key. `Load` returns sentinel errors so callers check the rejection
cause with `errors.Is`.

`Validate` owns the invariants. The private key is exactly
`ed25519.PrivateKeySize` bytes. The public key equals the private
key's public half. `Load` calls `Validate` before it returns. `Sign`
calls `Validate` before it signs.

`Sign` matches `envelope.Sign` in shape: value in, signed copy out.
It never logs the private key. The identity is the only object that
signs for the agent.

`Signer` returns `hex.EncodeToString` of the public key. This is the
same form `envelope.Sign` writes into `Message.Signer`. The
integration test pins the equality, so the two forms cannot drift
apart.

## Tests

Test files live in `identity/identity_test/`:

- `new_test.go` — the red-green cases for `New`, `Load`,
  `Validate`, and `Signer`. Start with the assertions. Confirm they
  fail on the empty phase. Implement and watch them pass.
- `sign_integration_test.go` — the cross-package path, three cases.
  Each case is listed below.
- `sign_bench_test.go` — benchmark `Sign` on a small message.
  `AllocsPerRun` states the allocation budget. The builder records
  the measured baseline in this file.

The integration test exercises the real path across boundaries. It
never mocks the trust boundary. The test package may import the other
packages; the deps gate exempts test code.

- Envelope round trip: `New`, sign a message, `Encode`, `Decode`,
  then `VerifySignature` passes. Tamper one field after signing and
  `VerifySignature` fails.
- Signer equality: `Load` the same key file and prove `Signer()`
  equals the signed message's `Signer` field. This case pins the
  canonical signer form shared with `envelope.Sign`.
- Room admission: sign with the identity and admit the message with
  `room.Accepts`. The identity's signer string is the room member id.
  A message from a signer outside the roster is rejected.

Key files for `Load` live in `identity/identity_test/testdata/`: one
valid key, one short, one uppercase, one non-hex, one with extra
whitespace.

## Verification

`make verify` passes. The coverage floor for `identity` holds. The
change lands these artifacts with the code:

- `docs/plans/identity.md` from `docs/plans/TEMPLATE.md`. The plan
  gate requires it once `identity/` holds Go files.
- The `identity` row in `policy/layers.json`: `envelope` only.
- `api/identity.txt` via `make api-update`.
- The `identity` entry in the package map of `docs/architecture.md`.
- `docs/packages/identity.md` with the exported API reference.
