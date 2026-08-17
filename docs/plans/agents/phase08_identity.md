# Phase 8: identity key wrap

Status: future. Builds the identity block. An identity is an ed25519
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
- `(*Identity).Sign(m envelope.Message) (envelope.Message, error)` that
  wraps `envelope.Sign`.
- `(*Identity).Signer() string` that returns the hex public key.
- `Load(path string) (*Identity, error)` that reads a key file.

`Load` reads a canonical hex file. It rejects a short or malformed
key. `Sign` never inspects or logs the private key. The identity is the
only object that signs for the agent.

## Tests

Test files live in `identity/identity_test/`:

- `phase08_tdd_test.go` — the red-green cases for `New`, `Signer`, and
  `Load`. Start with the assertions. Confirm they fail on the empty
  phase. Implement and watch them pass.
- `phase08_integration_test.go` — generate an identity, sign a message,
  and verify it with `envelope.VerifySignature`. Load the same key and
  prove the signer matches.
- `phase08_perf_test.go` — benchmark `Sign` on a small message.
  Target under fifty microseconds. State the allocation budget.

## Verification

`make verify` passes. The coverage floor for `identity` holds. The
`identity` package declares its import of `envelope` in
`policy/layers.json`. `api/identity.txt` lands via `make api-update`.
