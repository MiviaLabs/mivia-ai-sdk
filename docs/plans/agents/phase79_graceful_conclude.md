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
- Interaction with `Options.Window`. `Window`'s compaction step may
  drop, reorder, or summarize away `ConcludeNotice` before the nudged
  `Completer` call sees it, the same class of risk as the `Trim` case
  below. This phase adds no test for `ConcludeMargin` combined with
  `Window`: no current caller pairs the two, and `Window` already
  excludes `Trim` per `Options.Validate`, so the combination has one
  fewer variable than the general case. A future plan adds coverage
  once a caller needs both together.

## ConcludeMargin trigger formula

`run.go`'s loop tracks a 0-based `iterations` counter, incremented
after each `Completer` call, and checked `iterations >= l.maxIterations`
at the top of the loop before the next call. Number each `Completer`
call with a 1-based index `k`, so the call that runs while
`iterations` holds `k-1` is call `k`. `k` ranges from 1 to
`MaxIterations`.

Run appends `ConcludeNotice` to history, once, immediately before the
`Completer` call at the first `k` for which:

```
MaxIterations - k < ConcludeMargin
```

Zero `ConcludeMargin` never satisfies this inequality, since `k` never
exceeds `MaxIterations`, so nudging stays disabled. A `ConcludeMargin`
greater than or equal to `MaxIterations` satisfies it at `k = 1`, so
the nudge fires on Run's first iteration.

Worked table, `MaxIterations = 5`:

| ConcludeMargin | First qualifying k | Call nudged | Notes |
| --- | --- | --- | --- |
| 0 | none | none | nudging disabled |
| 1 | 5 | the last allowed call only | `MaxIterations-5 = 0 < 1` |
| 2 | 4 | the next-to-last call | `k=4` and `k=5` both qualify; the notice appends once, at the first qualifying `k` |

## API

```go
// Options gains:

// ConcludeMargin nudges the model to produce a final answer as
// MaxIterations approaches, appending ConcludeNotice once, instead of
// hard-stopping at MaxIterations with no notice. Zero disables
// nudging. Run appends the notice before the Completer call at
// 1-based iteration k the first time MaxIterations-k < ConcludeMargin
// holds; k ranges from 1 to MaxIterations, so a positive ConcludeMargin
// greater than or equal to MaxIterations fires the nudge on Run's
// first iteration. See docs/plans/agents/phase79_graceful_conclude.md
// for the worked table.
ConcludeMargin int

// ConcludeNotice is the RoleUser content Run appends once nudging
// starts. Empty ConcludeNotice with a positive ConcludeMargin uses
// DefaultConcludeNotice. Run appends the notice at the tail of
// history, as the last message in the nudged iteration's
// Request.Messages, not spliced near the system message the way
// CompactionNotice is. A tail append puts the "final answer now"
// instruction directly before the model's next response. The append
// runs after this iteration's Trim, Budget, and Window steps,
// immediately before the Completer call.
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

```go
// ErrConcludeMargin is Validate's error when ConcludeMargin is
// negative. Test with errors.Is.
var ErrConcludeMargin = errors.New("agentloop: ConcludeMargin must not be negative")
```

`Options.Validate` gains one rule: a negative `ConcludeMargin` fails
validation with `ErrConcludeMargin`.

## Tests

- `Options.Validate`: a negative `ConcludeMargin` fails with
  `errors.Is(err, ErrConcludeMargin)`. A zero or positive
  `ConcludeMargin` passes.
- A scripted `Completer` set to run past `MaxIterations` without
  `ConcludeMargin` stops at `StopMaxIterations`, unchanged from the
  base plan.
- `MaxIterations=5`, `ConcludeMargin=2`, a scripted `Completer` that
  returns tool calls through iteration 3, and returns no tool call at
  iteration 4: the sent `Request.Messages` for the iteration-4 call
  ends with the notice, and `Run` stops at `StopConcluded` with
  `Result.Iterations == 4`. This is the next-to-last-call row of the
  worked table above.
- `MaxIterations=5`, `ConcludeMargin=1`, the same scripted `Completer`
  returning tool calls through iteration 4, and no tool call at
  iteration 5: the notice appends only before the iteration-5 call,
  not iteration 4, matching the worked table's last-call-only row.
- The request sent to `Completer` on the nudged iteration carries the
  notice message as the last element of `Request.Messages`.
- `MaxIterations=5`, `ConcludeMargin=2`, a scripted `Completer` that
  returns no tool call at iteration 1, strictly before the first
  qualifying `k=4`: `Run` stops at `StopNoToolCalls`, not
  `StopConcluded`, and the iteration-1 `Request.Messages` carries no
  notice. This pins the boundary between an early, non-nudged stop and
  a nudged one.
- `MaxIterations=1`, `ConcludeMargin=1`: `k=1` satisfies
  `MaxIterations-k < ConcludeMargin` on Run's first iteration, so the
  first `Request.Messages` sent to `Completer` ends with the notice.
  This pins the doc comment's claim that a `ConcludeMargin` greater
  than or equal to `MaxIterations` fires the nudge on Run's first
  iteration.
- A `ConcludeMargin` set, but the model still requests a tool call on
  the nudged iteration and every iteration after, still stops at
  `StopMaxIterations` once the limit is reached.
- An empty `ConcludeNotice` with a positive `ConcludeMargin` sends
  `DefaultConcludeNotice`. A caller-set `ConcludeNotice` sends that
  text instead.
- The nudge message appends exactly once across a multi-iteration run,
  even when several iterations pass while inside the margin
  (`ConcludeMargin=2` case above already covers this: `k=4` and `k=5`
  both qualify, but only one notice appends).
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

Fold this phase into `docs/plans/agentloop.md` as an addendum once
shipped, matching how every prior phase landed.
