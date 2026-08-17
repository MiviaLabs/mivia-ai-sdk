# Plan: machine phase 3 — wire form and vectors

Adds the wire form for a definition and the conformance vectors. A
definition survives transport and still fires after a reload.

## Goal

Encode and decode a definition. The encoded form pins the structure:
statuses, triggers, and transition rows. Actions and guards stay code,
bound by name through a registry.

## Scope

Inside: Encode and Decode, the JSON form, and conformance vectors.
Outside: membership, transport, and the flow graph. The machine keeps
its own package and its own vectors.

## API

- `Encode(d Definition) ([]byte, error)`
- `Decode(data []byte, reg Registry) (*Definition, error)`
- `type Registry` maps a name to an Action and to a Guard.

The encoded form stores transition rows with names for actions and
guards. Decode rebinds those names through the registry. A stored
definition still fires correctly once rebound.

## TDD

Write the round-trip test first. Encode a definition, decode it, and
fire the result. Then write the invalid-vector tests. Encode and decode
are built to satisfy them.

## Tests

Round-trip and vector cases:

- a definition round-trips and fires after a rebind.
- a missing registry name returns an error.
- an unknown status in the stored form fails decode.
- a duplicate row in the stored form fails decode.
- the conformance vectors pin the wire shape.

Conformance vectors live in machine/testdata/vectors. Prefix rules come
from AGENTS.md: valid_, invalid_decode_, and invalid_sig_.

Integration holds here: definition to wire to reload to fire.

## Verification

`make verify`. The vectors gate needs a new vector only if a real rule
changes the wire shape.
