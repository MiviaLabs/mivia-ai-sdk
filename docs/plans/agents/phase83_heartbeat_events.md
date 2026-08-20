# Phase 83: heartbeat and progress events

Status: plan, not scheduled. Revision 1: addresses plan-reviewer REVISE
findings on function size, an unachievable test, missing adversarial
cases, timing flakiness, and Validate ordering. Revision 2: adds the
missing positive-path tool-call heartbeat test and names the shared
ticker helper.

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
- A required extraction in `agentloop/run.go`: `(l *Loop) run` is
  already at the 80-line function cap
  (`scripts/check_structure.py`). It cannot also bracket every return
  point with `EventIterationStart`/`EventIterationEnd` and start a
  ticking goroutine around the blocking `l.completer.Chat` call.
  The builder extracts a new method, `runIteration`, that holds the
  single-iteration body currently inlined in `run`'s `for` loop: the
  trim call, the budget check, the window plan, `runChat`, the audit
  call, the token-budget check, and the tool-call dispatch. `run`
  keeps only the loop control: the ctx-cancellation check, the
  `MaxIterations` check, one call to `runIteration`, and the
  iteration-done decision that call returns.
  `runIteration` fires `EventIterationStart` on entry and defers a
  closure that fires `EventIterationEnd` before returning, so every
  exit path — ctx cancellation inside the call, a trim error, a
  budget error, a `planHistory` error, an audit error, the
  token-budget-exceeded error, a tool-call error, and the two
  graceful stops (`StopNoToolCalls`, `StopHookVeto`) — emits
  `EventIterationEnd` exactly once. `run` itself drops under the
  80-line cap once this body moves out.
- A shared ticker helper, `startHeartbeat`, used by both the
  completion-heartbeat site in `runIteration` and the tool-call-
  heartbeat site in `runOneToolCall`. It starts a `time.Ticker` at
  `HeartbeatInterval`, launches the goroutine that emits the given
  event name on each tick, and returns a `stop` closure the caller
  defers to stop the ticker and join the goroutine. One shared helper
  keeps both call sites a few lines each, instead of duplicating
  ticker start/stop logic inline twice, so `runIteration` and
  `runOneToolCall` stay under the 80-line cap.

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

`Options.Validate`'s doc comment states a fixed check order:
Completer required, Tools required, MaxIterations positive, Usage
requires SessionID, Budget.Validate, MaxTotalTokens not negative,
Window.Validate plus its Summarizer/Calibrated/Trim rules. The new
`HeartbeatInterval` rule slots in last, after the Window block,
matching the precedent that each new Validate rule appends to the
end of the existing chain: a positive `HeartbeatInterval` with a nil
`Bus` fails with the new sentinel, `ErrHeartbeatRequiresBus`. The
builder updates `Validate`'s doc comment in the same change to list
this check as the final step, so the comment keeps matching the
code.

### Tool-call event semantics

`runOneToolCall` (`agentloop/toolcall.go`) returns before
`decodeAndRun`, and so before `RunScoped`, on a `PointPreTool` veto
(the `return provider.Message{}, true, nil, nil` path). This plan
brackets `runOneToolCall`, not just the `RunScoped` call inside it:
`EventToolCallStart` fires on entry, before the `PointPreTool` fire,
and `EventToolCallEnd` fires from a deferred closure covering every
return path, including the veto path and the `PointPreTool`
hook-error path. A heartbeat ticker for
`EventToolCallHeartbeat` starts only after the veto check passes,
since a vetoed call never reaches the blocking `RunScoped` work a
heartbeat exists to report progress on. This keeps
`EventToolCallStart`/`EventToolCallEnd` symmetric with
`EventIterationStart`/`EventIterationEnd` — both bracket their whole
call, not just its blocking segment — while the heartbeat itself
only ever ticks around real blocking work.

### Heartbeat test determinism

`HeartbeatInterval` is a raw `time.Duration` with no clock seam. A
real-wall-clock ticker test risks flakiness on a loaded CI runner.
This plan does not add an injectable clock: one caller and no
existing seam in `agentloop` make that abstraction speculative. The
heartbeat tests instead follow a fixed, documented convention: use a
`HeartbeatInterval` of 5 milliseconds, a scripted `Completer` or tool
that blocks for at least six intervals (30 milliseconds) before
returning, and assert on the subscriber's channel with a `select`
against a generous timeout (200 milliseconds, 40x the interval) —
never a busy poll and never a bare blocking receive. A timeout
firing before the expected event count is a hard test failure, not a
skip.

## Tests

- `Options.Validate`: a positive `HeartbeatInterval` with a nil `Bus`
  fails with `errors.Is(err, ErrHeartbeatRequiresBus)`. A zero
  `HeartbeatInterval` passes regardless of `Bus`. The failure occurs
  only after every earlier check (Completer, Tools, MaxIterations,
  Usage/SessionID, Budget, MaxTotalTokens, Window) passes, matching
  the documented append-at-the-end order.
- A scripted `Completer` that blocks past two heartbeat intervals,
  run with a `Bus` subscribed to `EventCompletionHeartbeat`, receives
  at least two heartbeat events before the call returns. Uses the
  5ms-interval/200ms-timeout convention above.
- The same setup with `HeartbeatInterval` zero receives no heartbeat
  event.
- `EventIterationStart` and `EventIterationEnd` fire once per
  iteration, in order, bracketing every other event that iteration
  emits.
- `EventIterationEnd` fires on every hard-fail exit path from
  `runIteration`, not only the happy path: ctx cancellation mid-call,
  a trim error, a budget error, a `planHistory` error, an audit
  error, the token-budget-exceeded error, and a tool-call error. Each
  cause gets its own table-driven case asserting exactly one
  `EventIterationEnd` reaches the subscriber.
- Heartbeat ticker goroutine cleanup, checked with a goroutine count
  assertion around the test (`runtime.NumGoroutine` before and after,
  with a bounded settle wait): no leak when ctx is canceled mid-call,
  and no leak when the call returns before the first tick fires.
- `PointPreTool` veto interaction: a wired `Hooks` registry that
  vetoes the call still produces `EventToolCallStart` followed
  immediately by `EventToolCallEnd`, with no
  `EventToolCallHeartbeat` in between, matching the documented
  semantics above.
- Tool-call heartbeat, positive path: a scripted tool, run through
  `RunScoped` with no `PointPreTool` veto, that blocks past two
  heartbeat intervals (30ms, the same convention as the completion-
  heartbeat test). A `Bus` subscribed to `EventToolCallStart`,
  `EventToolCallHeartbeat`, and `EventToolCallEnd` receives, in
  order: one `EventToolCallStart`, at least two
  `EventToolCallHeartbeat` events, then one `EventToolCallEnd`,
  within the 200ms timeout.
- `Bus.Emit`'s real failure mode: a heartbeat fires for an event name
  with no registered subscriber. Per `events/bus.go`, `Emit` returns
  an error only from `Event.Validate` (never true for a well-formed
  heartbeat event) or "no subscriber for name" — it never propagates
  a handler's own error, since `Emit` already swallows every handler
  error internally. This test subscribes to no heartbeat name at all,
  asserts the "no subscriber" `Emit` error is swallowed by the
  heartbeat path exactly like `PointStop`'s `hooks.Registry.Fire`
  error is already swallowed in `fireStop`, and asserts `Run`
  completes normally.
- A race sub-case: heartbeat emission from the ticking goroutine and
  the main loop's own state changes run concurrently, under
  `go test -race`, with no data race.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change: `events` is already an allowed import.
