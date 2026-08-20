# Package reference: a2aack

The `a2aack` package turns a remote A2A task round trip into the
composition layer's `AckWait`. One `Wait` call sends a gated step as
a remote task, polls the task's state, fetches its result, and
resolves the step's ack. It is an edge adapter: it exists only to
open the remote-transport edge the `a2aclient` plan reserves, so it
imports `agent` for the `AckWait` type. The exported surface below
mirrors `api/a2aack.txt`.

## Types

- `Remote` — the remote-task round trip `a2aack` polls: `Send`,
  `Status`, and `Result`. `*a2aclient.Client` implements it. An edge
  adapter depends on this minimal interface, not on the concrete
  client, so a caller can substitute any remote that speaks the three
  operations.
- `Options` — configures the poll loop. `Poll` is the interval
  between `Status` calls, required and positive. `Timeout` is the
  whole-exchange deadline, at least one `Poll`.

## Functions

- `Options.Validate` — rejects a non-positive `Poll` and a `Timeout`
  shorter than one `Poll`.
- `Wait(c Remote, opts Options)` — returns the `AckWait` that
  resolves one gated step through `c`. It validates eagerly: a nil
  `c` returns `(nil, ErrNoClient)`; invalid options return `(nil,
  opts.Validate())`; a valid input returns a non-nil `AckWait` and a
  nil error.

## The AckWait loop

The returned `AckWait` runs the whole exchange for one message:

1. Create a deadline from `Timeout` with `context.WithTimeout`.
2. `Send` the message. Any Send, Status, or Result error that is a
   context error wraps `ErrTimeout` with the last seen state; any
   other transport error propagates unwrapped.
3. Select on a `Poll` ticker and the deadline ctx on every tick, and
   read `Status` on each tick. Unresolved states — `StateSubmitted`,
   `StateWorking`, `StateUnspecified`, `StateUnknown` — record the
   last state and continue.
4. `StateCompleted` fetches `Result`, re-verifies the result's
   signature, and builds the ack. `StateFailed`, `StateCanceled`, and
   `StateRejected` return an error wrapping `ErrRemoteFailed`.
   `StateAuthRequired` and `StateInputRequired` return the same
   wrapped error. Both wait for client action `a2aack` never sends,
   so polling cannot resolve them.
5. The deadline or `ctx` returns an error wrapping `ErrTimeout` with
   the last seen state.

The ack references the sent step message, not the server reply.
`envelope.NewAck` keys `MessageID` from the sent message's id; only
`From` and the restatement come from the verified result. The result
signature is re-verified on the caller's side of the transport, so a
forged or tampered restatement never reaches the confirmed ack.

## Failure modes

- `ErrNoClient` ("a2aack: client is required") — `Wait` returns it
  when `c` is nil. Pinned by `a2aack_test/options_test.go`.
- `ErrNoPoll` ("a2aack: poll interval must be positive") —
  `Options.Validate`/`Wait` returns it when `Poll` is not positive.
  Pinned by `a2aack_test/options_test.go`.
- `ErrShortTimeout` ("a2aack: timeout must cover one poll") —
  `Options.Validate`/`Wait` returns it when `Timeout` is shorter than
  `Poll`. Pinned by `a2aack_test/options_test.go`.
- `ErrRemoteFailed` ("a2aack: remote task failed") — the returned
  `AckWait` wraps it when the remote task ends failed, canceled,
  rejected, or in a state `a2aack` cannot resolve. Pinned by
  `a2aack_test/failed_test.go`.
- `ErrTimeout` ("a2aack: remote task timed out") — the returned
  `AckWait` wraps it when the deadline or `ctx` expires before the
  task completes. Pinned by `a2aack_test/timeout_test.go`.

Retry of a failed remote task stays with `flow.Step.Retry` around
the gated step; `a2aack` never retries on its own.