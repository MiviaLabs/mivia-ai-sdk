# Phase 67: truncation spool

Status: plan only, not scheduled. One new leaf package plus one
`tools.Tool` wrapper. It depends on phase 65's canonical refs. It
builds independently of phase 66.

## Why this phase exists

`mivia-agent`'s `internal/remainder` owns the truncated-result
spool, about fourteen hundred lines. When output must shrink, the
spool stores the full bytes under a principal-scoped grant and hands
the caller a bounded view plus a reference. Its grants are
caller-scoped: one principal cannot read another's spooled content.

This SDK truncates nothing. Tools return whatever they return. The
parity scenarios route on short strings only because nothing longer
exists. Any real tool output — a log tail, a file read, a test run —
needs bounded results with the full content one reference away.

The spool is also the planner's counterpart. Phase 66 elides context
and emits references; this phase is where those references resolve
for tool output.

## Goal

One leaf stores oversized content and returns a bounded view with a
reference, and one wrapper gives any tool that behavior.

## Scope

Inside:

- A `spool` leaf importing `memory` only, or phase 65's ref store
  once it lands. `policy/layers.json` gains that one row.
- `Spool(principal, data)`: store the bytes, return the bounded view
  and the `ContentRef`-shaped reference.
- `Load(principal, ref)`: return the bytes, refusing another
  principal's grant.
- `SpoolTool(maxBytes, inner)`: a `tools.Tool` wrapper that spools
  an inner tool's oversized string result and returns the truncated
  view naming the reference.
- Grant expiry by byte age, matching the source repo's caller-scoped
  grant store shape.

Outside:

- Any persistence backend. The in-memory store is the default; a
  tagged store is a later phase if a caller needs one.
- Context elision decisions. Phase 66 owns those.
- Mivia's operator-facing spool views and its principal mapping.

## API

- `func NewSpool(store ContentStore) *Spool`
- `func (s *Spool) Spool(ctx, principal string, data []byte) (view string, ref string)`
- `func (s *Spool) Load(ctx, principal, ref string) ([]byte, error)`
- `func SpoolTool(name string, maxBytes int, inner tools.Tool) tools.Tool`
- Sentinels: `ErrUnknownRef`, `ErrWrongPrincipal`.

## Tests

- Spool and load round trip; oversized input truncates the view and
  keeps the full bytes.
- A wrong principal's load fails with `ErrWrongPrincipal`.
- The wrapper: a small result passes through untouched; a large one
  truncates and names its reference.
- Grant expiry drops old content and fails its loads.

## Verification

- `make verify` passes; `policy/layers.json` gains the `spool` row.
- One e2e case wires `SpoolTool` around a large-output tool inside
  an `agentrun` step.
- `docs/plans/spool.md` lands with the code.
