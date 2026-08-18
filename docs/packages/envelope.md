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
