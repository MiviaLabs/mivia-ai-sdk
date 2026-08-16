# Plan: envelope

## Goal

The wire unit for model-to-model communication: a natural-language
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

## Tests

Table-driven validation cases, ack state flow, sign/verify round trips
and tamper detection, thread chain adversarial cases, conformance
vectors in testdata/vectors, FuzzDecode, benchmarks with pprof.

## Verification

`make verify`. Adds the conformance-vector convention: every schema or
rule change adds a `valid_`, `invalid_decode_`, or `invalid_sig_` file.
