# Plan: envelope

## Goal

The wire unit for AI message exchange: a natural-language
payload inside machine-checkable, authenticatable metadata.

## Scope

Inside: Message, Ack, their validation and JSON wire form, ed25519
signing, thread chain verification, conformance vectors. Outside:
membership (room), transport, discovery, sessions.

## API

Message, Ack, Intent, Epistemic, Provenance, AckStatus types; ContextRef,
Sign, VerifyThread, Decode, DecodeAck functions; Validate/Encode/Hash/
RequiresAck/VerifySignature methods. Locked in `api/envelope.txt`.
Value semantics throughout; errors over panics; validation centralized
in Validate and called by Encode/Decode.

VerifyThread rejects duplicate message IDs inside one thread; the
`id` uniqueness invariant from message.go gains enforcement. No
signature changes: Hash keeps its signature and gains an honest comment
(NaN/Inf Confidence fails json.Marshal; Validate first). VerifySignature
returns the marshal error it used to swallow.

## Tests

Table-driven validation cases, ack state flow, sign/verify round trips
and tamper detection, thread chain adversarial cases, conformance
vectors in testdata/vectors, FuzzDecode, benchmarks with pprof.

Additions:

- Duplicate-ID case in the VerifyThread rejection table.
- Metadata-tamper table: mutate To, MaxHops, Epistemic, or PrevHash
  after signing; expect a VerifySignature failure.
- New vector `invalid_sig_tampered_metadata.json`: a signed message
  with one metadata field changed in the JSON. Build it with Sign,
  then edit one field. The mutated value must still pass Validate
  (for example max_hops); a mutation that breaks validation belongs
  under invalid_decode_ instead.
- VerifySignature with NaN Confidence returns the marshal error.
- No vector for the duplicate-ID rule: vectors pin single-message
  wire contracts; this is a multi-message thread rule.

## Verification

`make verify`. Adds the conformance-vector convention: every schema or
rule change adds a `valid_`, `invalid_decode_`, or `invalid_sig_` file.
The duplicate-ID rule and the VerifySignature marshal error land with
their docs/protocol-design.md update in the same change.
