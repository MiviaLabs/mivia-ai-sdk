# Phase 79: graceful work-limit conclude

Status: plan, not scheduled.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
a nudge toward a usable final answer as the iteration limit
approaches. This phase closes that gap. It has no code, no plan
review, and no `policy/layers.json` row yet. It needs a plan review
before a builder starts it.

## Goal

Nudge the model to produce a usable final answer as `MaxIterations`
approaches, instead of hard-stopping at `StopMaxIterations` with
whatever partial, mid-task state the transcript happens to hold.

## Scope

Inside:

- Two new `Options` fields, `ConcludeMargin` and `ConcludeNotice`.
- One new `StopReason`, `StopConcluded`, for the case the nudge
  worked: the model returned no tool call on the nudged iteration.
- One new exported constant, `DefaultConcludeNotice`.

Outside:

- Nudging as `MaxTotalTokens` approaches. Estimating "one more turn's
  headroom" against a token cap is a harder estimation problem than an
  iteration count, and needs its own review once phase 79's
  iteration-only nudge ships and its shape is proven.
- Stripping `Request.Tools` on the nudged iteration to force a
  text-only reply. This phase only appends a text nudge; a model that
  still requests a tool call on the nudged iteration is not blocked,
  and `Run` still ends with `StopMaxIterations`, unchanged, if the
  limit is hit.
- Retrying the nudge more than once. `ConcludeMargin` fires the notice
  exactly once, on the first iteration it applies to.

## API

```go
// Options gains:

// ConcludeMargin nudges the model to produce a final answer within
// this many iterations of MaxIterations, appending ConcludeNotice
// once, instead of hard-stopping at MaxIterations with no notice.
// Zero disables nudging. A positive ConcludeMargin greater than or
// equal to MaxIterations fires the nudge on Run's first iteration.
ConcludeMargin int

// ConcludeNotice is the RoleUser content Run appends once nudging
// starts. Empty ConcludeNotice with a positive ConcludeMargin uses
// DefaultConcludeNotice.
ConcludeNotice string
```

```go
// DefaultConcludeNotice is Options.ConcludeNotice's fallback text.
const DefaultConcludeNotice = "You are close to the iteration limit. Provide your best final answer now."

// StopConcluded is Run's stop reason when the model returns no tool
// call on an iteration ConcludeMargin nudged. Graceful, same
// Result-shape rule as StopNoToolCalls.
const StopConcluded StopReason = "concluded"
```

`Options.Validate` gains one rule: a negative `ConcludeMargin` fails
validation with a new sentinel, `ErrConcludeMargin`.

## Tests

- `Options.Validate`: a negative `ConcludeMargin` fails with
  `errors.Is(err, ErrConcludeMargin)`. A zero or positive
  `ConcludeMargin` passes.
- A scripted `Completer` set to run past `MaxIterations` without
  `ConcludeMargin` stops at `StopMaxIterations`, unchanged from the
  base plan.
- The same `Completer`, with `ConcludeMargin` set to trigger on the
  next-to-last allowed iteration, and scripted to return no tool call
  on that nudged iteration, stops at `StopConcluded`, and the sent
  request for that iteration carries the notice message.
- A `ConcludeMargin` set, but the model still requests a tool call on
  the nudged iteration and every iteration after, still stops at
  `StopMaxIterations` once the limit is reached.
- An empty `ConcludeNotice` with a positive `ConcludeMargin` sends
  `DefaultConcludeNotice`. A caller-set `ConcludeNotice` sends that
  text instead.
- The nudge message appends exactly once across a multi-iteration run,
  even when several iterations pass while inside the margin.
- A `Trim` hook that drops every `RoleUser` message, run with
  `ConcludeMargin` set: the notice appends to history, then the next
  iteration's `Trim` call drops it before the following `Completer`
  call sees it. `Run` still reaches `StopMaxIterations`, since the
  model was never actually nudged. This is a documented, accepted
  limit, not a guarantee this phase makes: `Options.Trim` already runs
  on the full history before every `Completer` call and may drop any
  message, per the base plan's `Trim` contract; `ConcludeNotice` gets
  no special protection from that contract.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change.
