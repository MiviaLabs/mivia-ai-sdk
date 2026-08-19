# Phase 65: durable context state

Status: plan only, not scheduled. One new leaf package plus one
unification across three existing minters. It depends on no
unshipped phase. It precedes phase 66 and phase 67, which build on
its types.

## Why this phase exists

The sibling repo `mivia-agent` runs a production context system its
own docs describe as dependency-neutral by design. Its
`internal/contextstate` package defines the durable contract:
chunked content-addressed payloads, sessions, checkpoints, commit
validation, retention classes, and volume `Limits`. Its
`internal/contentref` package mints the one canonical content
reference. Together they are about four thousand lines.

This SDK ships only primitives. `memory` holds blobs under an
eviction budget. `contextbudget` checks one model call's fit.
`provider` estimates tokens. Nothing holds a session-scoped durable
conversation, and nothing checkpoints the active context.

The ref minting is also forked. `envelope.ContextRef` mints one
form. `memory` reuses it. `mivia-agent`'s `contentref` mints
another, with chunking. Two canonical minters across repos is a fork
waiting to diverge.

External practice confirms the gap is core, not app-specific. The
OpenAI Agents SDK, Google ADK, and Bedrock AgentCore all treat
session state as SDK surface. OpenAI's own tracker reports sessions
lack context-length control on long conversations; phase 66 closes
that half here.

## Goal

One SDK leaf owns the durable context contract and the single
canonical content-reference minter. Every other block consumes it.

## Scope

Inside:

- A `contextstate` leaf importing nothing in this module. It holds
  the ported contract types: `ContentRef`, `PayloadRecord` with
  chunk reassembly, `Session`, `Checkpoint`, `CommitRequest` and
  its validation, `RetentionClass`, and `Limits` with `Validate`.
- The canonical minter, ported from `contentref`: one function, one
  digest form, chunk-aware. `envelope.ContextRef` and `memory`
  delegate to it or adopt its output verbatim, so the wire form
  stays stable and the fork ends.
- An in-memory session store, matching `ledger.MemStore`'s
  precedent: callers without persistence get a working default.
- The mivia-specific payload namespace constant moves to the caller.
  The SDK type takes the namespace as a field.

Outside:

- Provider-message conversion, planning, elision, and calibration.
  Phase 66 owns those.
- The truncation spool. Phase 67 owns it.
- Any SQLite or file backend. `mivia-agent`'s `internal/storage`
  stays app-side; a later phase may add a tagged store if a caller
  needs one.
- Mivia's source-range event cap and its compiled shape bounds.
  They port only where the contract's `Validate` needs them.

## API

The surface lands in `api/contextstate.txt` via `make api-update`.
Sketch:

- `func ContentRef(namespace string, chunks ...[]byte) ContentRef`
- `type PayloadRecord`, `type Session`, `type Checkpoint`
- `func (c CommitRequest) Validate() error`
- `type Limits struct { ... }` with `Validate`
- `type MemStore` with `New`, `Put`, `Session`, `Checkpoint`

`envelope.ContextRef` keeps its signature; its body delegates.

## Tests

- Ported contract tests from `contextstate`: chunk reassembly,
  commit validation rejections, `Limits.Validate`, retention.
- Fuzz seeds for ref minting and chunk reassembly, matching the
  source repo's fuzz corpus.
- One test per delegated minter proving byte-identical refs before
  and after the unification.
- A conformance check that `memory` and `envelope` produce the
  minter's exact digest form.

## Verification

- `make verify` passes; the mutation probe covers the new leaf.
- `policy/layers.json` gains the `contextstate` row, empty.
- `docs/plans/contextstate.md` and `docs/packages/contextstate.md`
  land with the code, not this plan.
