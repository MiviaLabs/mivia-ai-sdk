# Package reference: envelope

The envelope package defines the wire unit: the message, its metadata
types, the semantic ack, and signing. It is the only package that
serializes messages to the wire. See [architecture.md](../architecture.md)
for the flow and the rationale for why the envelope is shaped this
way. The exported surface below mirrors `api/envelope.txt`.

## Types

- `Message` — the wire unit. Fields: `version`, `id`, `room`,
  `thread_id`, `to`, `in_reply_to`, `intent`, `epistemic`,
  `confidence`, `context_refs`, `prev_hash`, `provenance`, `max_hops`,
  `cost_budget`, `ack_required`, `payload`, `signer`, `signature`.
- `Intent` — the message kind. Constants: `IntentAssert`,
  `IntentQuery`, `IntentRequest`, `IntentChallenge`, `IntentRetract`,
  `IntentEscalate`.
- `Epistemic` — how the sender knows the payload. Constants:
  `EpistemicVerified`, `EpistemicInferred`, `EpistemicAssumed`,
  `EpistemicUntrustedInput`.
- `Provenance` — the payload origin: `source`, `chain`, `evidence`.
- `Ack` — the semantic acknowledgment: `message_id`, `from`,
  `restatement`, `status`, `correction`.
- `AckStatus` — the ack state. Constants: `AckPending`,
  `AckConfirmed`, `AckCorrected`.
- `Version` — the only supported schema version.

## Functions and methods

- `Sign(key, m)` — signs a message; sets `signer` and `signature`.
- `Message.VerifySignature()` — checks the signature cryptography.
- `Message.Encode()` and `Decode(data)` — the wire serialization.
- `Message.Hash()` — the canonical content address for `prev_hash`.
- `ContextRef(content)` — the canonical content address of a blob.
- `Message.Validate()` and `Ack.Validate()` — the invariant checks.
- `NewAck(msg, from, restatement)` — builds a pending ack.
- `Ack.Confirm()` and `Ack.Correct(correction)` — resolve the ack.
- `Message.RequiresAck()` — true for requests and forced acks.
- `Ack.Encode()` and `DecodeAck(data)` — the ack wire serialization.
- `VerifyThread(msgs)` — the thread hash-chain check.

## Invariants

`Validate` enforces every rule below on both types.

- `version` equals `Version`.
- `id` and `thread_id` are required.
- `id` differs from `in_reply_to`.
- `to` entries are non-empty and unique.
- `challenge` and `retract` require `in_reply_to`.
- `verified` requires `provenance.source` and at least one evidence
  ref.
- `confidence` stays inside the closed range zero to one and is not
  NaN.
- Context refs, evidence refs, and `prev_hash` are canonical `sha256:`
  addresses.
- `max_hops` and `cost_budget` are not negative; the chain length
  stays within `max_hops` when set.
- `payload` is non-empty.
- `signer` and `signature` come as a pair in canonical hex forms.
- Ack: `message_id`, `from`, and `restatement` are required.
- Ack: `correction` requires `corrected` status; `corrected` status
  requires a correction.

## Wire contract

- The JSON field names come from the struct tags.
- Zero values of omitted fields stay absent: `room`, `to`,
  `in_reply_to`, `context_refs`, `prev_hash`, `max_hops`,
  `cost_budget`, `ack_required`, `signer`, `signature`.
- `Decode` ignores unknown fields, so a newer sender can add fields.
- `Encode` validates first; an invalid message never becomes wire
  JSON.
- The vectors in `envelope/testdata/vectors/` pin the message contract.
  The prefixes are `valid_`, `invalid_decode_`, and `invalid_sig_`.
- Ack vectors live in `envelope/testdata/ack_vectors/`. The prefixes
  are `valid_ack_` and `invalid_decode_ack_`. A valid ack re-encodes to
  the same bytes.

## Failure modes

This package returns plain errors, not sentinels. A caller cannot
match them with `errors.Is`.

- `Message.Validate` fails when `version` does not equal `Version`,
  `id` or `thread_id` is blank, `id` equals `in_reply_to`, a `to`
  entry is blank or repeated, a `challenge` or `retract` intent lacks
  `in_reply_to`, `verified` epistemic lacks `provenance.source` or
  evidence, `confidence` falls outside `[0, 1]` or is NaN, a context
  ref, evidence ref, or `prev_hash` is not a canonical `sha256:`
  address, `max_hops` or `cost_budget` is negative, the provenance
  chain exceeds `max_hops`, `payload` is blank, or `signer` and
  `signature` are not both canonical hex. Pinned by
  `envelope/message_test.go` and the `invalid_decode_` vectors in
  `envelope/testdata/vectors/`.
- `Ack.Validate` fails when `message_id`, `from`, or `restatement` is
  blank, `correction` is set without `corrected` status, or
  `corrected` status carries no correction. Pinned by
  `envelope/ack_test.go` and the `invalid_decode_ack_` vectors in
  `envelope/testdata/ack_vectors/`.
- `Message.VerifySignature` fails when the message is unsigned, the
  signer is not a valid ed25519 public key, the signature is not
  128 hex-encoded bytes, or the signature does not match the
  message content. Pinned by `envelope/sign_test.go` and the
  `invalid_sig_` vectors in `envelope/testdata/vectors/`.
- `Sign` fails when the supplied key is not an ed25519 private key of
  the expected length. Pinned by `envelope/sign_test.go`.
- `VerifyThread` fails when the thread is empty, a message fails
  `Validate`, two messages share an id, a message's `thread_id`
  does not match the thread, the first message carries a
  `prev_hash`, or a later message's `prev_hash` does not match its
  parent's hash. Pinned by `envelope/thread_test.go`.

## Usage

```go
key, _ := ed25519.GenerateKey(nil)
msg := envelope.Message{
    Version:    envelope.Version,
    ID:         "msg-1",
    ThreadID:   "task-42",
    Intent:     envelope.IntentRequest,
    Epistemic:  envelope.EpistemicInferred,
    Provenance: envelope.Provenance{Source: "model:self"},
    Payload:    "Summarize the config loading path.",
}
msg, _ = envelope.Sign(key, msg)
data, _ := msg.Encode()
got, _ := envelope.Decode(data)
_ = got.VerifySignature()
```
