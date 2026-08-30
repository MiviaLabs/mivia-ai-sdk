# Proposal: a stop-decision hook on agentloop

Status: proposal, not implemented. No API is locked and no code is
written. This file exists so the design is not lost while the evidence
that motivates it is still being gathered. See "Sequencing" for the
condition that must hold before this is built.

Package: `agentloop`. The shipped contract stays in
`docs/plans/agentloop.md`; this file becomes an addendum there if and
when it lands.

## Problem

The loop decides on its own when to stop. A caller can inject messages
BEFORE an iteration (`Steer.SetInjector`) and can ask the loop to wind
down as a bound approaches (`ConcludeMargin` / `ConcludeNotice`). It
cannot look at the response the model just produced, decide the run
should keep going, and supply the messages that continue it.

`runToolStage` (`agentloop/run.go`) holds the whole picture at the
moment it gives up: the assistant message, an empty tool-call list, the
finish reason, the iteration count. It converts that into
`StopNoToolCalls`, `StopEmptyResponse`, or `StopConcluded` and returns.
The caller receives a finished `Result` and has no way back in.

A host that wants to continue such a turn has exactly one option today:
run the whole loop again with a longer message list. `mivia-agent` does
this twice - `retryOnEmptyResponse` for a turn with no text and no tool
calls, and `continueUnactedTurn` for a turn that announced work and
called nothing. Both build a fresh `agentloop.Loop`.

A fresh loop resets state this package owns:

- `maxIterations` restarts, so one host turn can execute
  `MaxIterations x (1 + retries)` iterations. The host cannot bound
  this, because the counter is not its own.
- `maxTotalTokens` and the internal `runningTokens` restart, so
  `ErrTokenBudgetExceeded` stops guarding the turn it was configured
  for. This is the more serious of the two: the budget silently
  becomes per-attempt instead of per-turn.
- `Trim` re-runs over the whole history, so a long turn pays
  preparation again for messages it already prepared.

None of these are host bugs. They are the cost of re-entering a loop
from outside, and they disappear if the continuation happens inside it.

## Why the two existing seams do not cover it

`Steer.SetInjector` is pull-based: the loop drains it at the top of an
iteration and at a steered-stop downgrade, which is before the response
exists. Its own doc comment states that it is meant to be installed
before `RunSteerable` and that installing it mid-run races the loop.
A caller cannot decide "continue" from evidence it has not seen yet.

`ConcludeMargin` / `ConcludeNotice` / `StopConcluded` is a stop-sooner
mechanism. It fires on proximity to a bound, not on what the model did,
and its notice pushes the model toward a final answer. It is the
opposite operation.

## Proposal

Add one optional hook to `Options`:

```go
// ContinueOnStop is consulted when the loop is about to stop
// gracefully. Returning a non-empty slice appends those messages to
// the run history and continues the loop; returning nil or an empty
// slice stops the run as it would have. Nil hook means no change.
ContinueOnStop func(ctx context.Context, d StopDecision) []provider.Message
```

```go
// StopDecision is the evidence the loop had when it decided to stop.
type StopDecision struct {
    Stop       StopReason
    Message    provider.Message // the assistant turn that ended the run
    ToolCalls  []provider.ToolCall
    Iterations int
    History    []provider.Message
}
```

Call sites: the three graceful stops in `runToolStage`
(`StopNoToolCalls`, `StopEmptyResponse`, `StopConcluded`).

Deliberately NOT hookable:

- `StopSteered` - the caller asked the run to stop. A hook that
  overrides a steer defeats the steer.
- `StopHookVeto` - a veto that a continuation can override is not a
  veto. This is a policy decision, not a control-flow one.
- `StopMaxIterations` and `StopRepeatedToolFailures` - both are bounds
  doing their job. A caller that wants more iterations raises the
  bound.
- Every `hardFail` path. An error is not a stop.

## Semantics

A continuation is an ordinary iteration. It counts against
`MaxIterations`, its tokens count against `maxTotalTokens`, `Trim`
applies to the grown history, and `ctx` cancellation ends it. That is
the entire point: the loop keeps owning its own bounds, so a caller
cannot escape them by continuing.

The hook runs on the loop goroutine, like `surfaceFn` and the
injector. It must not block indefinitely and must not call back into
the loop.

A hook that returns messages unconditionally produces an infinite run
bounded only by `MaxIterations`. That is the correct failure mode: the
loop's own bound catches it, and the caller sees `StopMaxIterations`.
No second bound is added for this, because a second bound would be a
worse version of the one that already exists.

Messages returned by the hook are appended verbatim. The loop does not
label, wrap, or validate them beyond what it already does for
injected messages. A host that appends a `RoleUser` notice is
responsible for making it distinguishable from a real user turn.

## Scope

Inside: the `Options` field, the `StopDecision` type, the call sites in
`runToolStage`, and the tests below.

Outside: any judgement about WHEN to continue. The loop offers the
decision point; the policy is the caller's. In particular, a text
heuristic that reads a model's prose ("did it announce work it did not
do?") stays in the host. This package must not carry natural-language
patterns.

Also outside: changing `Steer` or `Conclude`. They keep their current
meanings.

## API

New exported surface for `api/agentloop.txt`:

- `Options.ContinueOnStop` field.
- `StopDecision` struct with `Stop`, `Message`, `ToolCalls`,
  `Iterations`, `History` fields.

No existing symbol changes. A caller that does not set the field gets
byte-identical behaviour, so this is additive and needs no major
version.

## Tests

The names below are the commitment; they do not exist yet, so this
file lives under `docs/plans/agents/` rather than in the gated
`docs/plans/agentloop.md` until they do.

- A hook returning messages on `StopNoToolCalls` continues the run and
  the continued iteration reaches the completer.
- A hook returning nil stops the run exactly as today, with the same
  `Result`.
- A nil hook produces a byte-identical request sequence to the current
  loop.
- The continuation counts against `MaxIterations`: a hook that always
  continues terminates with `StopMaxIterations`, not a hang.
- The continuation's tokens count against `maxTotalTokens`: a hook that
  always continues terminates with `ErrTokenBudgetExceeded` when the
  ceiling is set.
- `StopSteered`, `StopHookVeto`, `StopMaxIterations`, and
  `StopRepeatedToolFailures` never consult the hook.
- No `hardFail` path consults the hook.
- The hook sees the assistant message and the empty tool-call list that
  produced the stop.
- `Trim` applies to the grown history on the continued iteration.
- A hook that panics fails the run closed rather than half-continuing,
  matching `safeSurface`.

## Verification

- `go test -race -count=1 ./agentloop/...`
- `make api-update` then `make verify` - the API lock must record the
  two new symbols.
- `python3 scripts/check_plan.py`
- `python3 scripts/check_prose.py`

## Sequencing

Do not build this yet.

The motivating host feature (`mivia-agent`'s
`max_unacted_continuations`) is opt-in and off by default, and its
trigger is not yet confirmed against real traffic: the reported
behaviour was observed before a separate fix removed a fabricated
chain-of-thought block from that provider's requests, and it has not
been reproduced since. Locking public API on `Options` to serve a
mechanism that may not be needed is the wrong order.

The condition to build: a captured provider wire dump showing a turn
that ends with real assistant text, zero tool calls, and a tool
surface that was advertised - against a current `mivia-agent` build.

If that evidence arrives, this hook also lets `retryOnEmptyResponse`
collapse into the same seam, so it pays for itself twice. If it does
not arrive within a release cycle, delete the host feature and this
proposal together.
