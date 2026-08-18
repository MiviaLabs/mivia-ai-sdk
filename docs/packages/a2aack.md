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
2. `Send` the message. A Send that returns a context error maps to a
   wrapped `ErrTimeout`; any other Send error propagates unwrapped.
3. Select on a `Poll` ticker and the deadline ctx on every tick, and
   read `Status` on each tick. Non-terminal states — `StateSubmitted`,
   `StateWorking`, `StateUnspecified` — record the last state and
   continue.
4. `StateCompleted` fetches `Result`, re-verifies the result's
   signature, and builds the ack. `StateFailed` and `StateCanceled`
   return an error wrapping `ErrRemoteFailed`.
5. The deadline or `ctx` returns an error wrapping `ErrTimeout` with
   the last seen state.

The ack references the sent step message, not the server reply.
`envelope.NewAck` keys `MessageID` from the sent message's id; only
`From` and the restatement come from the verified result. The result
signature is re-verified on the caller's side of the transport, so a
forged or tampered restatement never reaches the confirmed ack.

## Sentinels

- `ErrNoClient` — `Wait` got a nil client.
- `ErrNoPoll` — the poll interval is not positive.
- `ErrShortTimeout` — the timeout does not cover one poll.
- `ErrRemoteFailed` — the remote task ended failed or canceled.
- `ErrTimeout` — the exchange outran its deadline or `ctx`.

Retry of a failed remote task stays with `flow.Step.Retry` around
the gated step; `a2aack` never retries on its own.