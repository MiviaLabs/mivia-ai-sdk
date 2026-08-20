# Phase 83: heartbeat and progress events

Status: plan, not scheduled.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
periodic progress events during a long `Completer` call or a long tool
batch. This phase closes that gap. It has no code, no plan review, and
no `policy/layers.json` row yet. It needs a plan review before a
builder starts it.

## Goal

Emit periodic events on `Options.Bus`, wired since the base plan but
never used, so a caller can show progress during a long `Completer`
call or a long tool batch.

## Scope

Inside:

- One new `Options` field, `HeartbeatInterval`.
- A fixed set of new `events.Name` values, defined inside `agentloop`,
  for iteration start/end, a periodic completion heartbeat, and a
  periodic per-tool-call heartbeat.
- Emitting on `l.bus` at each of those points when `HeartbeatInterval`
  is positive. A `Bus.Emit` error is swallowed, the same way `Run`
  already swallows a `hooks.Registry.Fire` error from `PointStop`:
  a heartbeat is observability, not a control-flow gate.

Outside:

- Any change to the `events` package. `events.Name` is already
  `type Name string`; `agentloop` defines its own constants of that
  type, the same way any package can. `events.Bus`, `events.Event`,
  and `events.Handler` need no change.
- Carrying structured data richer than `events.Event.Data string`
  allows. Every heartbeat event's `Data` is a short, human-readable
  string; a caller that needs structured data parses it itself, the
  same contract `events.Event` already documents.

## API

```go
// Options gains:

// HeartbeatInterval emits a heartbeat Event on Bus every interval
// while one Completer call or one tool call is in flight. Zero
// disables heartbeats. A positive HeartbeatInterval requires a
// non-nil Bus.
HeartbeatInterval time.Duration
```

```go
// The Event Names agentloop emits on Options.Bus when
// HeartbeatInterval is positive.
const (
    EventIterationStart      events.Name = "agentloop.iteration.start"
    EventCompletionHeartbeat events.Name = "agentloop.completion.heartbeat"
    EventToolCallStart       events.Name = "agentloop.tool_call.start"
    EventToolCallHeartbeat   events.Name = "agentloop.tool_call.heartbeat"
    EventToolCallEnd         events.Name = "agentloop.tool_call.end"
    EventIterationEnd        events.Name = "agentloop.iteration.end"
)
```

`Options.Validate` gains one rule: a positive `HeartbeatInterval` with
a nil `Bus` fails validation with a new sentinel,
`ErrHeartbeatRequiresBus`.

## Tests

- `Options.Validate`: a positive `HeartbeatInterval` with a nil `Bus`
  fails with `errors.Is(err, ErrHeartbeatRequiresBus)`. A zero
  `HeartbeatInterval` passes regardless of `Bus`.
- A scripted `Completer` that blocks past two heartbeat intervals, run
  with a `Bus` subscribed to `EventCompletionHeartbeat`, receives at
  least two heartbeat events before the call returns.
- The same setup with `HeartbeatInterval` zero receives no heartbeat
  event.
- `EventIterationStart` and `EventIterationEnd` fire once per
  iteration, in order, bracketing every other event that iteration
  emits.
- A `Bus.Emit` error from a failing subscriber does not fail `Run`; the
  run completes normally.
- A race sub-case: heartbeat emission from the ticking goroutine and
  the main loop's own state changes run concurrently, under
  `go test -race`, with no data race.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change: `events` is already an allowed import.
