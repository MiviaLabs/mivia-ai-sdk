# a2aack

## Goal

`a2aack` turns a remote A2A task round trip into the composition
layer's `AckWait`. One `Wait` call sends a gated step as a remote
task, polls it, fetches its result, and resolves the step's ack.

## Scope

Inside:

- `Options` and `Options.Validate` configure the poll loop.
- `Remote` is the send, status, and result round trip one `AckWait`
  polls. `*a2aclient.Client` implements it.
- `Wait` returns the `AckWait` and validates eagerly. A nil `Remote`
  returns `ErrNoClient`. Invalid options return their error.
- The ack references the sent step message, not the server reply.
  `MessageID` keys off the step's own id; `From` and the restatement
  come from the verified result. `a2aack` re-verifies the result's
  signature after the remote hop.

Outside:

- Retry of a failed remote task stays with `flow.Step.Retry`.
- Any server side. Receiving A2A traffic is phase 52's stdlib
  endpoint and phase 53's `a2a-go` server. The `a2aclient.Loopback`
  test fixture is the one server exception.
- Any a2a-go import. `a2aack` imports none; its importers pay the
  grpc dependency only through `a2aclient`.

## API

```go
package a2aack

type Options struct {
	Poll    time.Duration
	Timeout time.Duration
}

func (o Options) Validate() error

type Remote interface {
	Send(ctx context.Context, msg envelope.Message) (a2aclient.TaskHandle, error)
	Status(ctx context.Context, h a2aclient.TaskHandle) (a2aclient.State, error)
	Result(ctx context.Context, h a2aclient.TaskHandle) (envelope.Message, error)
}

func Wait(c Remote, opts Options) (agent.AckWait, error)

var ( ErrNoClient, ErrNoPoll, ErrShortTimeout, ErrRemoteFailed, ErrTimeout )
```

The `AckWait` loop sends once, then polls `Status` every `Poll`,
bounded by `Timeout` and `ctx`. Completed fetches the result and
re-verifies its signature. Failed and canceled return a wrapped
`ErrRemoteFailed`. The deadline or `ctx` returns a wrapped
`ErrTimeout` with the last seen state. A Send, Status, or Result
error that is a context error maps to `ErrTimeout` with the last
seen state. Any other transport error propagates unwrapped.

## Tests

`a2aack/a2aack_test/` holds one external test package. A fake
`Remote` drives every loop, timing, and error outcome. The live
`a2aclient.Loopback` appears only in the happy-path and integration
tests. Cases cover the validation order, the failed and canceled
paths, the timeout path, the poll-loop invariants, the transport
errors, and one real `agent.Agent` resolving its step through `Wait`.

## Verification

`policy/layers.json` allows `a2aack` to import `a2aclient`, `agent`,
and `envelope`. `make api-update` lands `api/a2aack.txt`. `make
verify` passes and `a2aack` holds the 85 coverage floor.