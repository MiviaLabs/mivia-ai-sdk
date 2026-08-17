# Phase 9: a2a message mapping

Status: future. Builds the a2a block. The a2a block plans live in
`docs/plans/a2a.md`. This phase maps an envelope message to an A2A
part and back. See `docs/plans/agents/PHASES.md`.

## Goal

Map an `envelope.Message` into an A2A message part. Map it back
unchanged. The round trip proves the envelope survives the wire.
This phase carries no network.

## Scope

Inside: the mapping between the envelope and the A2A part data, and
the round-trip tests. Outside: the a2a-go client, the network calls,
and the Agent Card. Those belong to phases 10 and 11.

## API

The mapping follows A2A v1.0. A custom object rides in the part `data`
value or the message `metadata`. There is no longer a separate part
kind. See `docs/research-agents.md` for the v1.0 notes.

- `ToPart(m envelope.Message) (a2a.Part, error)`
- `FromPart(p a2a.Part) (envelope.Message, error)`

The signature stays unbroken. The mapping signs nothing itself. It
does not modify the envelope. `contextId` on the task maps to the
envelope `thread_id`. The `messageId` maps to the envelope `id`.

## Tests

Test files live in `a2a/a2a_test/`:

- `phase09_tdd_test.go` — the red-green cases for `ToPart` and
  `FromPart`. Start with the assertions. Confirm they fail on the
  empty phase. Implement and watch them pass.
- `phase09_integration_test.go` — build a signed message, map it to a
  part, map it back, and verify the signature and every field. Prove
  `thread_id` becomes `contextId` and returns intact.
- `phase09_perf_test.go` — benchmark the round trip on a full message.
  Target under fifty microseconds. State the allocation budget.

Conformance vectors for the mapped form land in
`a2a/testdata/vectors/`. Prefix the valid form `valid_`.

## Verification

`make verify` passes. The coverage floor for `a2a` holds. The a2a
package declares its import of `envelope` in `policy/layers.json`.
The mapping section of `docs/protocol-design.md` updates in the same
change.
