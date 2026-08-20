# Phase 78: steering and interruption

Status: plan, not scheduled.

## Why this plan exists

A gap analysis compared `agentloop` against `internal/agent.Loop`, a
production caller in a separate, external repository (`mivia-agent`).
It found a capability that repo's caller needs and `agentloop` lacks:
a graceful way to stop the current iteration without a hard `ctx`
cancellation. This phase closes that gap. It has no code, no plan
review, and no `policy/layers.json` row yet. It needs a plan review
before a builder starts it.

## Goal

Let a caller stop the current iteration's in-flight `Completer` call
without hard-canceling the whole `Run`. A hard `ctx` cancellation
today always ends in `Run`'s hard-fail path: `Final` and `Stop` stay
the zero value. A steer request is graceful instead: it stops at the
current iteration boundary with a named `Stop` reason, and it leaves
every already-appended history entry, including a completed tool
call's `RoleTool` message, untouched.

## Scope

Inside:

- One new type, `Steer`, a caller-held handle that requests a
  soft-cancel of the `Completer` call in flight for one `Run` call.
- One new method, `RunSteerable`, alongside the existing `Run`. `Run`
  keeps its current signature and behavior; `RunSteerable` adds the
  `steer` parameter. `Run(ctx, msgs)` stays equivalent to
  `RunSteerable(ctx, msgs, nil)`.
- One new `StopReason`, `StopSteered`.
- Scoping the soft-cancel to the `Completer.Chat` (or, after phase 84,
  `ChatStream`) call for the current iteration only. A steer request
  never touches `ctx` itself, and never affects a tool call already
  dispatched.

Outside:

- Interrupting a running tool call. Today's tool calls run
  sequentially, one at a time, strictly after the iteration's
  `Completer` call returns; no tool call is ever "in flight" at the
  moment a steer request can fire. A future concurrent-tool-call
  design would need its own review of what "interrupt a tool call"
  means.
- Any change to `provider.Completer`'s interface. A `Completer`
  already receives a `context.Context` first argument, by Go
  convention; `RunSteerable` relies only on that convention, the same
  way `Run` already relies on it for hard cancellation.
- Any change to `Options`. `Steer` is a per-`Run`-call value, not a
  per-`Loop` value: a `Loop` already supports concurrent `Run` calls,
  and one `Options.Steer` field would let one caller's steer request
  stop another caller's unrelated `Run` call sharing the same `Loop`.

## API

```go
// Steer is a caller-held handle that requests a soft-cancel of one
// RunSteerable call's in-flight Completer call. Trigger is safe to
// call from another goroutine. A Steer triggered before RunSteerable
// starts, or after it already returned, is a no-op.
type Steer struct {
    // unexported: an internal cancel func and a sync.Once guard.
}

// NewSteer returns a ready Steer, unbound to any Run call until
// passed to RunSteerable.
func NewSteer() *Steer

// Trigger requests the soft-cancel this Steer is bound to. Calling
// Trigger more than once, or before RunSteerable binds this Steer,
// has no additional effect.
func (s *Steer) Trigger()

// RunSteerable is Run with one addition: a non-nil steer lets the
// caller request a soft-cancel of the current iteration's in-flight
// Completer call from another goroutine. ctx cancellation still ends
// the run as a hard failure, unchanged from Run. A triggered steer
// ends the run gracefully instead, at the next iteration boundary,
// with Stop == StopSteered and Final holding the last message
// appended before the steer fired. Run(ctx, msgs) is equivalent to
// RunSteerable(ctx, msgs, nil).
func (l *Loop) RunSteerable(ctx context.Context, msgs []provider.Message, steer *Steer) (Result, error)
```

New `StopReason`:

```go
// StopSteered is Run's stop reason when a Steer.Trigger call requests
// a soft-cancel of the in-flight Completer call. Graceful: nil error,
// same Result-shape rule as every other graceful stop.
const StopSteered StopReason = "steered"
```

`Steer`, `NewSteer`, `(*Steer).Trigger`, `RunSteerable`, and
`StopSteered` land in `api/agentloop.txt` via `make api-update`, in
the same change as the code.

## Tests

- A `Steer.Trigger` call from a second goroutine, mid-`Completer.Chat`
  in a scripted `Completer` that blocks until its own ctx is canceled,
  stops `RunSteerable` with `Stop == StopSteered` and a nil error.
- The same setup with a prior iteration already completed asserts
  `Result.History`, `Iterations`, and `Usage` carry that prior
  iteration's state, matching the base plan's Result-shape rule for a
  graceful stop.
- A `Steer` never triggered runs `RunSteerable` to its normal stop,
  identical to `Run`.
- `ctx` canceled directly, with a `Steer` present but never triggered,
  still hard-fails exactly like `Run`, proving `RunSteerable` does not
  change hard-cancellation behavior.
- `Trigger` called twice, and `Trigger` called before `RunSteerable`
  starts, are both no-ops that do not panic.
- A race sub-case: `N` goroutines call `Trigger` concurrently, under
  `go test -race`.
- A `Steer.Trigger` call fired mid-tool-batch, not mid-`Completer`, has
  no effect until the next iteration boundary: a scripted multi-call
  tool batch, with `Trigger` called while the second of three calls is
  executing, still runs the third call before `RunSteerable` stops
  with `Stop == StopSteered` on the following iteration.

## Verification

`make verify` passes, including the API gate against the regenerated
`api/agentloop.txt`. `go test -race ./agentloop/...` passes. No
`policy/layers.json` change: this phase adds no new import.
