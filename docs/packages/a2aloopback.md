# Package reference: a2aloopback

`a2aloopback` ships a real gRPC A2A test-server fixture, `Loopback`.
No production code may import it; it exists to run inside another
package's own tests, the same convention `durablefence` uses. See
[durablefence.md](durablefence.md). The exported surface below mirrors
`api/a2aloopback.txt`.

## Types

- `loopbackExecutor` (unexported) — the `a2asrv.AgentExecutor`
  `Loopback` wires into its server. It holds one ed25519 private key,
  generated fresh per `Loopback` call, used to sign every response
  envelope.

## Functions and methods

- `Loopback()` — starts a gRPC A2A server bound to `127.0.0.1:0`, a
  kernel-assigned port, so the fixture has no external network
  dependence. Returns `addr`, the server's real listen address; `stop`,
  a function that shuts the server down and blocks until it has fully
  stopped; and an `err` for a listen failure or a key-generation
  failure. `stop` is idempotent: it runs its shutdown exactly once even
  under repeated calls.
- `loopbackExecutor.Execute(ctx, reqCtx, queue)` — completes every task
  it receives. It reads the payload string from the request's first
  `a2acore.DataPart`, signs a fresh `envelope.Message` restating that
  payload, binds the response envelope's `ID` to the message ID it
  mints and its `ThreadID` to the server-assigned `ContextID`, maps the
  signed envelope to an A2A data part, and writes a final
  `TaskStateCompleted` status event carrying it. Returns an error when
  the request carries no message or no payload part, or when signing
  or the A2A part mapping fails.
- `loopbackExecutor.Cancel(ctx, reqCtx, queue)` — writes a final
  `TaskStateCanceled` status event with no message body. `Loopback`'s
  own server flow never reaches this path; it exists to satisfy
  `a2asrv.AgentExecutor`.

## Invariants

- `Loopback` binds only `127.0.0.1:0`. A test using this fixture makes
  no real external network call.
- Every response envelope's `ID` and `ThreadID` are bound to the A2A
  ids the server itself minted for that task, exactly as a real
  responding agent must, so a caller's post-hop signature and
  thread-chain check both pass.
- `stop` is safe to call more than once: a `sync.Once` guards the
  underlying `grpc.Server.Stop` call and the wait for the serving
  goroutine to exit.
- `Execute` never echoes anything from the request beyond the payload
  string it was asked to restate; it mints its own message ID and
  reads the context ID assigned by `reqCtx.TaskInfo()`.

## Cross-references

- [durablefence.md](durablefence.md) — the sibling test-only fixture
  pattern: a leaf package that exists to run inside another package's
  own tests, never from production code.
- [a2aclient.md](a2aclient.md) — `a2aclient.Client` is the fixture's
  intended caller in tests: it sends a signed message to `Loopback`'s
  address, polls status, and fetches the signed result.
- [envelope.md](envelope.md) — `Execute` builds and signs an
  `envelope.Message` for every completed task.
- [a2a.md](a2a.md) — `Execute` maps the signed envelope onto an A2A
  message part through `a2a.ToPart`.

## Usage

```go
addr, stop, err := a2aloopback.Loopback()
if err != nil {
    t.Fatalf("Loopback: %v", err)
}
defer stop()

client, err := a2aclient.New(addr)
if err != nil {
    t.Fatalf("a2aclient.New: %v", err)
}
defer client.Close()

handle, err := client.Send(ctx, signedRequest)
// handle resolves to a completed task carrying a signed reply
// envelope that restates signedRequest's payload
```
