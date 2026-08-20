# Phase 84: partial-recovery streaming

Status: plan, not scheduled, blocked on a `provider` package decision.
Depends on phase 78.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
keeping partial streamed text when a completion is interrupted, instead
of discarding it. This phase closes that gap. It has no code, no plan
review, and no `policy/layers.json` row yet. It needs a plan review
before a builder starts it, and it depends on phase 78 (`Steer`).

## Goal

When a completion is interrupted mid-stream, by a phase 78 steer
request or by `ctx` cancellation, keep whatever partial text the model
already produced, instead of discarding it. Today `agentloop` never
streams: `runChat` calls `Completer.Chat`, never `ChatStream`, so no
partial text exists to preserve.

## Scope, and the shared-package flag

This phase cannot land as an `agentloop`-only change. `provider`
already ships `RunTurn` and its unexported helper `drainStream`, which
consume `Completer.ChatStream` and merge `Chunk` values into one
`Response`. `drainStream`'s cancellation branch discards every
accumulated `Chunk` on `ctx.Done()`: `return Response{}, ctx.Err()`,
with no partial content returned. Reusing `RunTurn` today would
inherit that discard behavior, defeating this phase's goal.

Two designs need a plan-review decision before a builder starts this
phase:

- Option A: extend `provider.RunTurn`'s cancellation path to return
  the partial `Response` accumulated so far, alongside the `ctx`
  error, instead of a zero `Response`. This changes `RunTurn`'s
  already-locked, documented contract in `api/provider.txt` and needs
  its own plan review against `docs/plans/provider.md`, weighed
  against every other `RunTurn` caller's expectations.
- Option B: `agentloop` drains `ChatStream` itself, without calling
  `RunTurn`, duplicating `drainStream`'s merge logic
  (`mergeToolCallDelta`, `buildResponse`) inside `agentloop`. Disfavored:
  this forks logic `provider` already owns, against the Building
  blocks rule against a copied type or a forked algorithm. It is named
  here for completeness, not as an equal candidate.
- Option C: export `provider`'s existing pure merge and accumulation
  primitives, `buildResponse` and `mergeToolCallDelta` in
  `provider/runturn.go`, unexported today only because no caller
  outside `provider` has needed them. `RunTurn`'s behavior and its
  locked, documented cancellation contract stay unchanged. `agentloop`
  drains `ChatStream` itself and calls the exported merge helper
  directly, so it reuses `provider`'s algorithm instead of forking it.

Option A and Option C are both live candidates; Option B is not, under
the Building blocks rule. The choice between A and C, and the final
decision, is left to whoever reviews phase 84 when it is built.

Inside, once the shared-package question is resolved:

- One new `Options` field, `Stream`, opting an iteration into the
  streaming path.
- On an interrupted stream, whether by phase 78's `Steer` or by `ctx`
  cancellation, `Result.Final` carries the partial text produced so
  far, instead of the zero value.

Outside:

- Any change to `provider.Chunk`, `provider.Completer`, or
  `mergeToolCallDelta`'s merge policy for tool-call fragments. This
  phase's concern is preserving partial assistant text, not changing
  how a tool-call fragment merges.
- Resuming a stream after an interruption. Interruption ends the
  iteration; `Run` does not attempt to resume mid-stream on the next
  iteration.

## Amendment to the Result-shape rule

The base plan's Result-shape rule, in `docs/plans/agentloop.md`,
states `Final` and `Stop` "stay the zero value" on every hard-fail
error return, including a `ctx`-cancellation mid-`Completer`-call.
This phase amends that rule for the one case it targets: a `ctx`
cancellation that lands mid-stream, with `Options.Stream` set, now
carries a non-zero `Final.Content` holding the partial text produced
before cancellation. `Stop` still stays the zero value, since a hard
`ctx` cancellation is still a hard failure, not a graceful stop; only
`Final.Content` changes. Every other hard-fail cause in the base
plan's closed list is unaffected.

## API

Depends on the Option A/C decision above; the exact shape of the
streaming plumbing is not specified until that decision is made. The
following is expected, not final:

```go
// Options gains:

// Stream opts one Loop into streaming Completer calls through
// ChatStream instead of Chat, so an interrupted completion (Steer or
// ctx cancellation) can preserve partial text. False, the zero value,
// keeps today's non-streaming Chat path.
Stream bool
```

## Tests

- A scripted streaming `Completer` that emits several `Delta` chunks,
  then blocks, with `ctx` canceled mid-stream: `Result.Final.Content`
  holds the concatenation of every `Delta` chunk emitted before
  cancellation. `Result.Stop` stays `StopReason("")`, the zero value.
  The returned error satisfies `errors.Is(err, context.Canceled)`.
  `Result.History`, `Result.Iterations`, and `Result.Usage` stay at
  their pre-call values, unchanged by the partial `Final.Content`.
- The same setup with a phase 78 `Steer.Trigger` call instead of a
  `ctx` cancellation: `Stop == StopSteered`, and `Result.Final.Content`
  holds the same partial concatenation.
- `Options.Stream` false runs unchanged from every existing base-plan
  case: no `ChatStream` call happens.
- A stream that completes normally, `Options.Stream` true, produces
  the same `Result` shape a non-streaming `Chat` call would produce
  for an equivalent scripted response.

## Verification

Blocked until the Option A/C decision lands its own plan review. Once
resolved: `make verify` passes, including the deps gate against
whatever `policy/layers.json` change, if any, that decision needs, and
the API gate against the regenerated `api/agentloop.txt` and, under
Option A, `api/provider.txt`. `go test -race ./agentloop/...` and,
under Option A, `go test -race ./provider/...` pass.
