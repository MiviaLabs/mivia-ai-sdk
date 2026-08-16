# Plan: a2a

Status: future. No code yet. This plan exists to fix the boundary
before any builder starts.

## Goal

Delegate tasks to remote agents through the A2A protocol. Keep the
envelope's semantics intact across the wire.

## Scope

Inside: mapping between envelope.Message and A2A task messages.
The envelope travels inside an A2A Part, in the data or metadata value.
A2A v1.0 has one Part type; the old DataPart is gone. Discovery reads
Agent Cards. Task states map to thread events. Outside: an A2A server,
streaming, push. We are a client of A2A, not a server, in v1.

## API

Proposed shape, subject to plan review:

- `Send(ctx, card AgentCard, msg envelope.Message) (TaskHandle, error)`
- `Status(ctx, h TaskHandle) (TaskState, error)`
- `Result(ctx, h TaskHandle) (envelope.Message, error)`

A new row in policy/layers.json: a2a imports envelope only. A2A client
libraries are the one allowed exception to the stdlib-only rule, if
plan review accepts one. Use the official Go SDK a2aproject/a2a-go; it
needs Go 1.25 or newer.

## Tests

Contract tests against a recorded A2A server transcript. Round-trip
tests proving the envelope survives the DataPart embedding unchanged.
Signature verification after every remote hop.

## Verification

`make verify`. Conformance vectors for the embedded form. The A2A
mapping section of docs/protocol-design.md updates in the same change.
