# Phase 51: a2aack remote step ack

Status: future. Plan-only; it has not gone through plan review yet.
Depends on no unshipped phase. It adds one new top-level package.

## Why this phase exists

`a2aclient` sends a signed step message as a remote task, polls its
state, and fetches its result. Nothing turns that exchange into an
`agent.AckWait`. Every remote caller writes the same send, poll,
result, verify, and ack loop by hand.

The `a2aclient` plan already reserves this edge: "A later phase adds
that edge when the composition layer wires in a real transport"
(`docs/plans/a2aclient.md:189`). This phase is that later phase.

## Goal

One constructor turns an `a2aclient.Client` into an `agent.AckWait`.
A gated step becomes one remote task; the remote reply becomes the
confirmed ack's restatement.

## Scope

Inside:

- New package `a2aack` with `Options`, `Options.Validate`, `Wait`,
  and sentinels.
- The poll loop: `Send`, then `Status` every `Poll` until a terminal
  state, bounded by `Timeout` and `ctx`.
- The ack construction: `Result` returns a verified
  `envelope.Message`; `NewAck` takes its `Signer` as `From` and its
  `Payload` as the restatement; `Confirm` closes the round trip.

Outside:

- Any retry of a failed remote task. `StateFailed` and `StateCanceled`
  return errors; retry policy stays with `flow.Step.Retry` around the
  gated step, which is its designed home.
- Any server side. Receiving A2A traffic is phase 52's stdlib
  endpoint and phase 53's deferred `a2a-go` server.
- Any a2a-go import inside `a2aack` itself. The Semgrep
  stdlib-only exception covers `a2aclient/*.go` only, and test files
  are scanned. The loopback fixture therefore ships as exported
  `a2aclient` surface, inside the exempted directory.

## API

```go
package a2aack

type Options struct {
	Poll    time.Duration // between Status calls; required, positive
	Timeout time.Duration // whole-exchange deadline; at least Poll
}

func (o Options) Validate() error

// Wait returns the AckWait resolving one step through c.
func Wait(c *a2aclient.Client, opts Options) agent.AckWait

var (
	ErrNoClient     = errors.New("a2aack: client is required")
	ErrNoPoll       = errors.New("a2aack: poll interval must be positive")
	ErrShortTimeout = errors.New("a2aack: timeout must cover one poll")
	ErrRemoteFailed = errors.New("a2aack: remote task failed")
	ErrTimeout      = errors.New("a2aack: remote task timed out")
)

// a2aclient package, same change:
//   func Loopback() (addr string, stop func() error, err error)
//
// A reference loopback server over a2a-go's in-memory service,
// exported for cross-package tests. It compiles inside a2aclient,
// the one directory the third-party exception covers.
```

The returned `AckWait` resolves one message:

1. `Send` the message; an error returns unwrapped.
2. Poll `Status` every `Poll` ticks. `StateWorking`,
   `StateSubmitted`, and `StateUnspecified` continue the loop; the
   zero value appears before the first remote status lands.
3. `StateCompleted` fetches `Result`. `Result` re-verifies the
   signature on the caller's side of the transport.
4. The ack's `From` is the result message's `Signer`; the
   restatement is its `Payload`. `Confirm` returns.
5. `StateFailed` and `StateCanceled` return an error wrapping
   `ErrRemoteFailed` with the state's `String`.
6. `ctx` cancellation or the `Timeout` deadline returns an error
   wrapping `ErrTimeout` with the last seen state.

## Validation

- `Wait` rejects a nil client with `ErrNoClient`.
- `Options.Validate` enforces a positive `Poll` and a `Timeout` at
  least one `Poll`. The run calls it once before the first `Send`.
- The poll loop selects on `ctx.Done` every tick, so cancellation
   never waits out a full interval.

## Tests

`a2aack/a2aack_test/`, one external test package. The fixture boots
`a2aclient.Loopback` and points a real `a2aclient.Client` at it; no
test file imports a2a-go, so the Semgrep stdlib-only rule holds
outside `a2aclient`.

- `options_test.go`, table-driven: nil client, zero poll, short
  timeout, and the accept path.
- `wait_test.go`: a completed task yields a confirmed ack whose
  `From` and restatement match the server's reply.
- `failed_test.go`: a failing task yields `ErrRemoteFailed`.
- `timeout_test.go`: a never-terminal task yields `ErrTimeout`
  within one interval of the deadline.
- `integration_test.go`: one real `agent.Agent` whose step resolves
  through `Wait`, asserting `MessageAckedEvent` and the final
  status.

## Verification

- `policy/layers.json` gains `"a2aack": ["a2aclient", "agent", "envelope"]`.
- `make api-update` lands `api/a2aack.txt` and the `a2aclient`
  `Loopback` line in `api/a2aclient.txt`, in the same change.
- `make verify` passes; `a2aack` and the module total hold the 85
  floor.
- The a2a-go import stays confined to `a2aclient`'s files. `a2aack`
  itself imports none; its importers pay the grpc dependency only
  through `a2aclient`, as before.
- `docs/plans/a2aack.md`, `docs/packages/a2aack.md`, and a
  `docs/examples/a2aack.md` walkthrough ship with the package.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
