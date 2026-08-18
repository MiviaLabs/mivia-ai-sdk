# Phase 53: a2a-go server bridge

Status: future. Plan-only; it has not gone through plan review yet.
Not scheduled. Two gates must open before any build starts.

## Why this plan exists

Phase 52 ships a stdlib NDJSON and HTTP endpoint for the envelope
wire form. A caller interoperating with the wider A2A ecosystem
needs the same receive ladder behind the A2A v1.0 protocol instead.
That requires `github.com/a2aproject/a2a-go`'s server API, which
today only `a2aclient` may import.

This plan records the intent and its gates. It commits nothing and
blocks on an explicit user decision.

## Gates

- Phase 52 must prove the receive ladder in real use. Its handler
  seam, error mapping, and admission order are the shape this
  bridge reuses. Building both at once risks two speculative
  designs instead of one proven one.
- The user must authorize widening the `a2a-go` exception. AGENTS.md
  scopes that import to `a2aclient`. A server package needs its own
  named exception row in AGENTS.md and its own plan review.

## Goal

One package, `a2aserver`, hosts an A2A v1.0 server whose inbound
parts map through `a2a.FromPart` onto the same ladder phase 52
runs: verify, admit, resolve, handle, ack.

## Scope

Inside, once both gates open:

- Inbound part to `envelope.Message` through the shipped `a2a`
  mapping.
- Reuse of the phase 52 handler seam and ladder, imported or
  lifted, not copied.
- Task lifecycle mapping: one A2A task per request, terminal states
  mapped from the ladder's outcome.

Outside:

- Any change to `a2a`'s mapping rules.
- Any second wire form. NDJSON stays phase 52's concern.

## API

Sketch only; the contract is fixed at plan review, after phase 52's
seams are proven:

```go
package a2aserver

func New(opts Options) (*Server, error) // serves a2a-go transport

type Options struct {
	ID      string
	Room    *room.Room
	Resolve func(ctx context.Context, m envelope.Message) (Handler, error)
	Bus     *events.Bus
}
```

## Tests

A loopback round trip against `a2aclient.Client`: send, terminal
state, verified result, confirmed ack. The fixture reuses the
exported `a2aclient.Loopback` reference server phase 51 ships.

## Verification

- AGENTS.md's dependency rule gains the named exception, in the same
  change, recorded as a user decision.
- `policy/layers.json` gains the row with the widened import.
- `make verify` passes; the 85 floor holds.
- Until both gates open, this file records intent only. No code, no
  lock, no layer row.
