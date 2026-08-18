# Package reference: a2aclient

The a2aclient package sends an `envelope.Message` to a remote agent
and reads its task status and result back, through the
`a2aproject/a2a-go` client. This is the first external network call
in the module. The exported surface below mirrors `api/a2aclient.txt`.

## Types

- `Client` — sends envelope messages to one remote A2A agent and reads
  task status and results back. Wraps one `a2aproject/a2a-go` gRPC
  transport for one base URL. Safe for concurrent use by multiple
  goroutines.
- `TaskHandle` — identifies one remote task started by `Send`. The
  zero `TaskHandle` identifies no task; `Status` and `Result` reject
  it.
- `State` — the state of a remote task, mirrored from the a2a-go task
  state enum: `StateUnspecified`, `StateSubmitted`, `StateWorking`,
  `StateCompleted`, `StateFailed`, `StateCanceled`. `String` returns
  the constant name, or `"unknown"` outside the declared range.

Internally, `Client` drives its task lifecycle through an unexported
`transport` interface (`Send`, `State`, `Result`, `Close`). `New`
builds the production implementation, wrapping a2a-go's gRPC
transport. This stays unexported: no caller outside this package's
own tests needs it. The package's own tests use an unexported
`newFromTransport` constructor and a same-package (`package
a2aclient`) test file to substitute a scripted transport instead of
dialing a live network endpoint, since `sdk-standards.yml` scopes the
third-party-import exception to `a2aclient/*.go` and an external test
package cannot import `a2a-go` directly. See
`docs/plans/a2aclient.md`'s Tests and Verification sections for this
test seam.

## Functions

- `New(baseURL)` — validates `baseURL` and opens the underlying
  a2a-go gRPC transport, which holds a persistent connection. Returns
  an error, not a partial `Client`, when `baseURL` is empty or the
  transport fails to open.

## Methods

- `(*Client) Close() error` — releases the resources `New` opened.
  Idempotent: a second call returns nil.
- `(*Client) Send(ctx, msg) (TaskHandle, error)` — maps `msg` to an
  A2A part through `a2a.ToPart`, then sends it as a new task. `msg`
  must already be signed. Returns an error and a zero `TaskHandle`,
  never a partial one, on a transport failure or a canceled or
  expired `ctx`.
- `(*Client) Status(ctx, h) (State, error)` — reads the current state
  of the task named by `h`. Rejects the zero `TaskHandle`. A canceled
  or expired `ctx` returns that `ctx` error, unwrapped.
- `(*Client) Result(ctx, h) (envelope.Message, error)` — fetches the
  task's output and maps it back through `a2a.FromPart`, then calls
  `VerifySignature` on the result before returning it. Returns an
  error, not a partial `Message`, when the task is not yet terminal,
  when `FromPart` fails, when the signature check fails, or when
  `ctx` is canceled or expired.

## Invariants

- `Send` never returns a non-zero `TaskHandle` alongside a non-nil
  error.
- `Status` and `Result` reject the zero `TaskHandle` with an error.
- `Result` only fetches a task's output once its `State` is terminal:
  `StateCompleted`, `StateFailed`, or `StateCanceled`.
- `Result` re-verifies the message signature after the remote hop.
  Signature verification is the invariant network transport adds: a
  message valid before the hop must still be valid after it.
- `Close` is idempotent.

## Why this shape

`a2aclient` is a new top-level package, not a addition to `a2a`.
Bolting a network client onto the stdlib-only `a2a` package would
force every caller of the pure mapping functions to carry the
third-party dependency this phase needs. `a2aclient` is the only
package in this module allowed to import `github.com/a2aproject/a2a-go`
and its `google.golang.org/grpc` dial dependency; see `AGENTS.md`'s
Rules section for the stated exception.

## Usage

```go
c, err := a2aclient.New("agent.example.com:443")
if err != nil {
    // handle error
}
defer c.Close()

h, err := c.Send(ctx, signedMessage)
if err != nil {
    // handle error
}

state, err := c.Status(ctx, h)
// poll until state is terminal

result, err := c.Result(ctx, h)
// result.VerifySignature() already checked inside Result
```
