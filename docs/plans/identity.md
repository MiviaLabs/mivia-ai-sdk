# Plan: identity

Status: shipped. Phase 8 of the agent work. The phase contract is
docs/plans/agents/phase08_identity.md. This package depends on
envelope, which is shipped. Later phases own the agent card, the
trust policy, and the registry. A Validate hardening follows below
to reject split-brain key files.

## Goal

Own one agent key. An Identity holds the ed25519 key pair and derives
the hex signer string. The package loads the key from a file and
signs envelopes.

## Scope

Inside: the Identity type, key generation, the key-file load, the
invariant check, and the signer derivation. Sign wraps envelope.Sign.
The key file format is pinned below.

Outside: the agent card, the trust policy, the registry, and key
storage policy. File permissions stay with the caller.

The package imports envelope only. Other imports come from the
standard library: crypto/ed25519 and encoding/hex. The policy row is
"identity": ["envelope"].

### Validate hardening: reject split-brain key files

Validate currently compares i.PublicKey against i.PrivateKey.Public()
which slices key[32:64]. This does not derive from the seed. A corrupt
key file (zeroed public half, flipped seed byte) passes Load and
Validate, Sign succeeds, but receivers' VerifySignature fails because
envelope.Sign stamps Signer from the corrupt embedded half while the
signature uses the seed.

Fix: derive the true public key with
ed25519.NewKeyFromSeed(i.PrivateKey[:32]) and require both
i.PublicKey and the embedded half to match the derived key. No new
exported symbols. Validate's semantics tighten. Existing callers are
unaffected because Load already derives pub from priv and assigns it.
The builder must also update the Validate doc comment (identity.go
lines 70-72) to state the seed-derived invariant instead of "the
public key is its public half."

### Doc fixes

The builder applies these doc-truth corrections in the same change:

- README.md:55,69,71 — change "Future" to "Shipped" for agent and
  discovery; add heartbeat, discovery, and agent to the Features
  list and Layout tree.
- docs/plans/heartbeat.md:3, docs/plans/discovery.md:3 —
  change "Status: planned" to "Status: shipped."
- docs/packages/identity.md:41 — change "The public key equals
  the private key's public half." to "The public key equals the
  seed-derived key."
- AGENTS.md Layout — add discovery/ and agent/ bullets.
- docs/README.md:49 — change "tdd, and perf" to "unit, and benchmark."

Do not touch docs/plans/agent.md,
docs/plans/agents/phase13_agent_run.md,
docs/protocol-design.md, or policy/layers.json. Those have uncommitted
user edits from phase-13 work.

## API

- `type Identity struct` with the exported fields
  `PublicKey ed25519.PublicKey` and `PrivateKey ed25519.PrivateKey`.
- `New() (*Identity, error)` generates a fresh key pair.
- `Load(path string) (*Identity, error)` reads a key file.
- `(*Identity).Validate() error` checks the key invariants.
- `(*Identity).Sign(m envelope.Message) (envelope.Message, error)`
  wraps envelope.Sign.
- `(*Identity).Signer() string` returns the hex public key derived
  from the private key.
- `var ErrKeyFormat` is the sentinel for a malformed key file.
- `var ErrKeyInvalid` is the sentinel for a key that breaks an
  invariant.

The key file format is pinned. The file holds exactly 128 lowercase
hex characters. Those characters decode to the 64-byte ed25519
private key. One optional trailing newline is allowed. Every other
content is malformed. Load wraps ErrKeyFormat for malformed content.
Load derives the public key from the private key. A read failure
wraps the underlying error and is no sentinel.

Validate owns the invariants. The private key is exactly
ed25519.PrivateKeySize bytes. The public key and the embedded public
half must each equal the seed-derived key. A broken invariant wraps
ErrKeyInvalid. Load calls Validate before it returns. Sign calls
Validate before it signs. A zero-value Identity fails Validate.

Sign matches envelope.Sign in shape: value in, signed copy out. It
never logs the private key. The identity is the only object that
signs for the agent.

Signer derives the public half from the private key through
key.Public(). This is the same source envelope.Sign uses for
Message.Signer. An exported PublicKey field can diverge from the
private key; deriving keeps one source of truth. Signer returns
hex.EncodeToString of the derived key. Signer returns an empty
string when the private key is not ed25519.PrivateKeySize bytes. The
integration test pins the equality, so the two forms cannot drift
apart.

The exported surface above lands in api/identity.txt through
make api-update. The expected lock content is:

```text
package identity
  func (i *Identity) Sign(m envelope.Message) (envelope.Message, error)
  func (i *Identity) Signer() (string)
  func (i *Identity) Validate() (error)
  func Load(path string) (*Identity, error)
  func New() (*Identity, error)
  type Identity struct {
  PublicKey ed25519.PublicKey
  PrivateKey ed25519.PrivateKey
}
  var ErrKeyFormat
  var ErrKeyInvalid
```

## Tests

Test files live in identity/identity_test/. The test package imports
envelope and room. The deps gate exempts test code, so the room edge
needs no policy row.

- `new_test.go` — the red-green cases for New, Load, Validate,
  and Signer. Assertions come first. The builder confirms they fail
  on the empty package, then implements them to green. The Load cases
  form a table over the testdata fixtures. Each malformed fixture
  asserts errors.Is against ErrKeyFormat. A nonexistent path returns
  an error that fails errors.Is against ErrKeyFormat and unwraps to
  fs.ErrNotExist. A zero-value Identity fails Validate with errors.Is
  against ErrKeyInvalid. A zero-value Identity returns an empty
  Signer. A New identity with PublicKey overwritten by
  another key fails Validate the same way.
- `sign_integration_test.go` — the cross-package path, three cases.
  The test exercises the real path and never mocks the trust
  boundary.
- `sign_bench_test.go` — a benchmark of Sign on a small message.
  AllocsPerRun states the allocation budget. The builder records the
  measured baseline in this file.

The integration cases:

- Envelope round trip. New, sign a message, Encode, Decode, then
  VerifySignature passes. Tamper one field after signing and
  VerifySignature fails.
- Signer equality. Load the valid key file, sign a message, and
  prove Signer() equals the message Signer field. Both forms derive
  from the same private key bytes. This case pins the canonical
  signer form shared with envelope.Sign.
- Room admission. Sign with the identity and admit the message with
  room.Accepts. The identity's signer string is the room member id.
  A message from a signer outside the roster is rejected.

Key files for Load live in identity/identity_test/testdata/. The
fixture set is exact:

- `valid` — 128 lowercase hex chars, no newline. Load accepts it.
- `valid_newline` — 128 chars plus one trailing newline. Load
  accepts it.
- `empty` — zero bytes. Load rejects it.
- `seed_form` — 64 chars, the 32-byte seed form. Load rejects it.
- `uppercase` — 128 uppercase hex chars. Load rejects it although
  hex.DecodeString would accept it.
- `non_hex` — characters outside the hex set. Load rejects it.
- `crlf` — 128 chars plus a carriage return and a newline. Load
  rejects it.
- `interior_whitespace` — whitespace inside the hex. Load rejects
  it.

Every rejection asserts errors.Is against ErrKeyFormat.

- `validate_split_brain_test.go` — table-driven test with three
  invalid temp key files and one direct-construct case. Case (a):
  valid-length 64 bytes with bytes 32:64 zeroed. Case (b):
  valid-length key with seed byte 0 flipped. Case (c): bytes 0:32
  are a valid seed, bytes 32:64 are a different key's public half
  generated by ed25519.GenerateKey. Assert Load returns errors.Is
  against ErrKeyInvalid for each. Case (d): construct an Identity
  directly (not through Load) with a valid PrivateKey where the
  embedded public half differs from the seed-derived key. Assert
  Validate returns errors.Is against ErrKeyInvalid. This tests the
  hardened invariant independent of Load's derivation. Cases (a)
  through (c) test the Load path. The test proves the hardened
  Validate rejects keys where the embedded public half does not
  match the seed-derived public key.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for identity and for the total.
- The identity row in policy/layers.json lists envelope only. The
  row lands with this plan, before the code.
- `api/identity.txt` lands through make api-update in the same change
  as the code. The lock matches the surface in the API section.
- docs/architecture.md gains the identity entry in the package map.
- docs/packages/identity.md documents the exported API, the key file
  format, and the invariants.
- The phase adds no conformance vectors. The key file format is a
  local format, not a wire schema. The envelope vectors in
  envelope/testdata/vectors/ already pin the signature form.
- For the Validate hardening: `python3 scripts/check_plan.py` must
  pass. `go test -race ./identity/...` must pass. `make verify-fast`
  must pass.
