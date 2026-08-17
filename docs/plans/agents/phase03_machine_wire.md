# Phase 3: machine wire form

Status: future. Builds on phase 2. This phase adds the wire form of a
definition. The definition stays serializable. Conformance vectors pin
the form. See `docs/plans/agents/PHASES.md` for the contract.

## Goal

Serialize a machine definition to JSON and back. The round trip
preserves the transition table. Invalid shapes fail on decode. The
vectors lock the wire contract.

## Scope

Inside: `Encode` and `Decode` for a `Definition`, plus a canonical
form for guards and actions. Outside: the `machine` package's public
invariants. Those are in phase 1. The flow package waits for phase 4.

## API

- `(*Definition).Encode() ([]byte, error)`
- `Decode(data []byte, reg Registry) (Definition, error)`
- `type Registry struct { Actions map[string]Action; Guards map[string]Guard }`
- `NewRegistry() Registry` to build an empty registry.

A function does not serialize. The form stores a name for each guard
and action. Guard names and action names are separate namespaces in
the wire form. Decode rebinds each name through the matching Registry
map. A missing name returns an error. Unknown fields are ignored on
decode.

## Tests

Test files live in `machine/machine_test/`:

- `phase03_tdd_test.go` — the red-green cases for `Encode` and
  `Decode`. Start with the assertions. Confirm they fail on the empty
  phase. Implement and watch them pass.
- `phase03_integration_test.go` — round-trip a definition through the
  wire form. Prove the table is identical before and after. Push a bad
  shape and confirm decode fails.
- `phase03_perf_test.go` — benchmark the round trip on a ten-row
  table. Target under two microseconds.

Conformance vectors land in `machine/testdata/vectors/`. Prefix the
valid form `valid_` and the bad form `invalid_decode_`.

## Verification

`make verify` passes. The coverage floor for `machine` holds. The
wire-follow semantics live in `docs/protocol-design.md` if they change.
`api/machine.txt` gains `Encode` and `Decode`.
