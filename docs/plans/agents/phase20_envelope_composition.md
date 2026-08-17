# Phase 20: envelope delivery emits via composition

Status: future. Builds on phase 17 and a composition layer. This phase
lets the shared bus carry the envelope delivery path. A delivered
message, an ack, or a thread event emits onto the bus. No envelope code
and no events code import the other. The composition layer translates.

## Goal

Let one shared bus carry message delivery too. A caller that subscribes
to the bus sees a delivered message, an ack, or a thread event as one
event. The envelope block stays a dependency-free leaf.

## Scope

Inside: a thin translator that the composition layer owns. It turns a
delivered `Message`, an `Ack`, or a `VerifyThread` result into one
`Event` and emits it onto the bus. The identity and location of the
composition layer come from the agent block; this phase depends on it.
Outside: any import from `envelope` to `events` or from `events` to
`envelope`. Both edges stay forbidden.

## API

No new exported symbol on `envelope` or on `events`. The translator is
composition-layer code. It adopts the existing path: sign, encode,
transport, decode, verify, admit, ack, thread. After the delivery step
it emits the outcome onto the bus.

## Tests

Test files live in the composition layer's test directory:

- `translator_test.go` — the red-green cases for the translator. Start
  with the assertions. Confirm they fail on the empty implementation.
  Implement and watch them pass.
- `translator_integration_test.go` — run a deliver and prove an event
  arrives once. Feed an ack and a thread verify and prove each arrives.
  Run under `go test -race`.

## Verification

`make verify` passes. The coverage floor for the composition layer
holds. `policy/layers.json` gains no edge for `envelope` or for
`events`. The translator emits only; it does not change the envelope
contract and it does not change the bus contract.
