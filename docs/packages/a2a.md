# Package reference: a2a

The a2a package maps an `envelope.Message` onto an A2A v1.0 message
part and back, and carries the A2A v1.0 client over that mapping.
`ToPart` and `FromPart` hold the mapping. `Client` sends a message to
a remote agent and reads its task status and result back, through the
`a2aproject/a2a-go` client. `Wait` turns one remote round trip into an
agent step ack. The exported surface below mirrors `api/a2a.txt`.

## Mapping types

- `Part` — one A2A v1.0 message part. A2A v1.0 has no `kind` field
  and no separate part classes: one part carries text or a data
  object. `Part` carries no message-level field. Fields: `Text`,
  `Data` (`json.RawMessage`).
- `Mapped` — the result of `ToPart` and the input to `FromPart`. It
  pairs a `Part` with the two A2A message-level fields `Part` cannot
  carry: `ContextID` (the envelope `ThreadID`) and `MessageID` (the
  envelope `ID`).

## Mapping functions

- `ToPart(m)` — sets `Part.Text` to the exact bytes `m.Encode`
  returns. `m.Encode` validates `m` before it marshals, so `ToPart`
  returns an error, not a zero `Mapped`, on a `Validate` failure.
  Signs nothing and does not modify `m`. `Data` stays empty.
- `FromPart(mapped)` — unmarshals `mapped.Part.Text` into an
  `envelope.Message`. An empty `Text` falls back to `Part.Data`, so
  an old peer's data part still decodes until v0.4.0. `Text` wins
  when both are set; both empty fails the decode. It then overwrites
  `ThreadID` with `mapped.ContextID` and `ID` with
  `mapped.MessageID`, then calls `Validate` before returning. Returns
  an error, not a partial `Message`, on a decode or a `Validate`
  failure.

## Mapping invariants

- `ToPart` never returns a non-zero `Mapped` alongside a non-nil
  error.
- `FromPart` never returns a non-zero `Message` alongside a non-nil
  error. A malformed or invalid part never crosses the a2a boundary.
- `FromPart` applies the `ContextID`/`MessageID` override before it
  calls `Validate`. The override wins over any `thread_id`/`id`
  already embedded in `Part.Text` or a fallback `Part.Data`, even
  when the override empties an otherwise-valid embedded message.

## Wire contract

- `Part.Text` carries the exact bytes `envelope.Message.Encode`
  produces. A proto string hop preserves those bytes byte-exact, so
  integer literals above 2^53 survive a round trip. `FromPart` reads
  it with `encoding/json.Unmarshal`, not `envelope.Decode`, because
  the `ContextID`/`MessageID` override must run before `Validate`.
- `ToPart` sets only `Part.Text`; `Data` stays empty. `FromPart`'s
  `Data` fallback exists for one release, until v0.4.0, and dies in
  the same change as the gRPC transport's data-part fallback.
- Conformance vectors live in `a2a/testdata/vectors/`, `valid_`
  prefixed. A vector pairs the source `envelope.Message` with its
  mapped `Part`, `ContextID`, and `MessageID`.

## Client types

- `Client` — sends envelope messages to one remote A2A agent and reads
  task status and results back. Wraps one `a2aproject/a2a-go` gRPC
  transport for one base URL. Safe for concurrent use by multiple
  goroutines.
- `TaskHandle` — identifies one remote task started by `Send`. The
  zero `TaskHandle` identifies no task; `Status` and `Result` reject
  it.
- `State` — the state of a remote task, mirrored from the a2a-go task
  state enum: `StateUnspecified`, `StateSubmitted`, `StateWorking`,
  `StateCompleted`, `StateFailed`, `StateCanceled`, `StateRejected`,
  `StateAuthRequired`, `StateInputRequired`, `StateUnknown`. Every
  a2a-go `TaskState` has one `State`. `String` returns the constant
  name, or `"unknown"` outside the declared range. `StateUnknown`
  returns that same text, so `"unknown"` names either the upstream
  indeterminate state or an out-of-range value.
- `Remote` — the client surface `Wait` needs: `Send`, `Status`,
  `Result`. `*Client` satisfies it.
- `Options` — `Wait`'s poll interval, timeout, and expected result
  signer. `Validate` enforces each rule.

Internally, `Client` drives its task lifecycle through an unexported
`transport` interface (`Send`, `State`, `Result`, `Close`). `New`
builds the production implementation, wrapping a2a-go's gRPC
transport. This stays unexported: no caller outside this package's
own tests needs it. The package's own tests use an unexported
`newFromTransport` constructor and a same-package (`package a2a`)
test file to substitute a scripted transport instead of dialing a
live network endpoint, since `policy/thirdparty.json` scopes the
third-party-import exception to `a2a` and an external test package
cannot import `a2a-go` directly. See `docs/plans/a2a.md`'s Tests and
Verification sections for this test seam.

## Client functions

- `New(baseURL)` — validates `baseURL` and opens the underlying
  a2a-go gRPC transport over a plaintext channel, which holds a
  persistent connection. It suits loopback and trusted links only.
  Returns an error, not a partial `Client`, when `baseURL` is empty
  or the transport fails to open.
- `NewWithTLS(baseURL, cfg)` — the same construction with a
  `crypto/tls` config for a remote link. A nil `cfg` returns
  `ErrInvalidOptions`; the constructor never guesses a dial mode.
  `New` dials plaintext itself and no longer delegates to this
  constructor.
- `Wait(c, opts)` — returns an ack function that sends one message
  through `c`, polls to a terminal state, and returns the remote
  agent's ack.

## Client methods

- `(*Client) Close() error` — releases the resources `New` opened.
  Idempotent: a second call returns nil.
- `(*Client) Send(ctx, msg) (TaskHandle, error)` — maps `msg` to an
  A2A part through `ToPart`, then sends it as a new task. Send
  rejects an empty `msg.Signer` with `ErrUnsigned` before it maps
  and before the transport touches the network: Send performs no
  signing of its own. Returns an error and a zero `TaskHandle`,
  never a partial one, on a transport failure or a canceled or
  expired `ctx`.
- `(*Client) Status(ctx, h) (State, error)` — reads the current state
  of the task named by `h`. Rejects the zero `TaskHandle`. A canceled
  or expired `ctx` returns that `ctx` error, unwrapped.
- `(*Client) Result(ctx, h) (envelope.Message, error)` — fetches the
  task's output and maps it back through `FromPart`, then calls
  `VerifySignature` on the result before returning it. One transport
  call returns the mapped part and the task's state together, so
  Result performs exactly one fetch per call. Returns an error, not
  a partial `Message`, when the task is not yet terminal, when
  `FromPart` fails, when the signature check fails, or when `ctx` is
  canceled or expired.

## Client invariants

- `Send` never returns a non-zero `TaskHandle` alongside a non-nil
  error.
- `Status` and `Result` reject the zero `TaskHandle` with an error.
- `Result` only fetches a task's output once its `State` is terminal:
  `StateCompleted`, `StateFailed`, `StateCanceled`, or
  `StateRejected`. That set equals a2a-go's own `TaskState.Terminal`.
- `Result` re-verifies the message signature after the remote hop.
  Signature verification is the invariant network transport adds: a
  message valid before the hop must still be valid after it.
- `Close` is idempotent.

## Client failure modes

The client half returns exported sentinels. A caller matches one with
`errors.Is`.

- `ErrInvalidOptions` (`"a2a: invalid options"`): a caller-supplied
  construction argument that fails its rule. `New` and
  `newFromTransport` return it for an empty `baseURL`;
  `newFromTransport` returns it for a nil transport; `NewWithTLS`
  returns it for a nil `cfg`; `Options.Validate` returns it for a
  non-positive poll or timeout.
- `ErrNoTaskID` (`"a2a: transport returned an empty task id"`):
  `Send` returns it when the transport answers an empty task id.
- `ErrZeroTaskHandle` (`"a2a: zero TaskHandle"`): `Status` and
  `Result` return it against the zero `TaskHandle`.
- `ErrNotTerminal` (`"a2a: task is not terminal"`): `Result` wraps
  it, with the task's current state in the message, when the task has
  not yet reached a terminal state.
- `ErrUnsigned` (`"a2a: message must be signed"`): `Send` returns it
  when `msg.Signer` is empty.
- `ErrSignatureCheckFailed` (`"a2a: signature check failed"`):
  `Result` wraps it, alongside the underlying `VerifySignature`
  error, when the mapped message's signature fails after the remote
  hop.
- `ErrNoTask` (`"a2a: send did not return a task"`, in `grpc.go`):
  the internal gRPC transport's `Send` returns it when the remote
  response is not a `*Task`.
- `ErrNoResultMessage` (`"a2a: task carries no result message"`, in
  `grpc.go`): the internal gRPC transport's `Result` returns it when
  the task carries no status message and no history entry.
- `ErrNoTextPart` (`"a2a: result message carries no text part"`, in
  `grpc.go`): the internal gRPC transport's `Result` returns it when
  the result message carries no `TextPart` and no `DataPart`; a
  `DataPart`-only result takes the one-release fallback until v0.4.0
  instead.
- `ErrRemoteFailed` (`"a2a: remote task failed"`): `Wait` returns it
  when the remote task settles in a failed or canceled state.
- `ErrTimeout` (`"a2a: remote task timed out"`): `Wait` returns it
  when the task does not settle inside `Options.Timeout`.
- `ErrSignerMismatch` (`"a2a: result signer is not the expected
  remote"`): `Wait` returns it when the result signer differs from
  `Options.ExpectSigner`.

Every client failure mode above is pinned by a test in
`a2a/client_test.go`, `a2a/wait_test.go`, or
`a2a/grpc_internal_test.go`.

## Client usage

```go
c, err := a2a.New("agent.example.com:443")
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

## Why this shape

`Part` mirrors the A2A v1.0 wire shape: one struct, no `kind` field,
no separate part classes. `ContextID` and `MessageID`
sit on `Mapped`, not on `Part`, because in the real A2A wire form
they belong to the wrapping A2A `Message`, not to an individual
`Part`.

The mapping and the client live in one package because they are one
protocol. A caller that maps a message also sends it. `a2a` is the
only package in this module allowed to import
`github.com/a2aproject/a2a-go` and its `google.golang.org/grpc` dial
dependency; see `policy/thirdparty.json` for the stated exception.

## Mapping failure modes

The mapping half returns plain errors, not sentinels. A caller cannot
match them with `errors.Is`.

- `ToPart` fails when `m.Validate()` fails, for example on an unset
  `Version` or an invalid `Intent`. Pinned by `a2a/mapping_test.go`.
- `FromPart` fails when `mapped.Part.Text`, or a fallback
  `mapped.Part.Data`, does not unmarshal into an `envelope.Message`.
  Pinned by `a2a/mapping_test.go`.
- `FromPart` fails when the mapped message, after the `ContextID`/
  `MessageID` override, fails `Validate`. Pinned by
  `a2a/mapping_test.go`.

## Mapping usage

```go
m := envelope.Message{
    Version:    envelope.Version,
    ID:         "msg-1",
    ThreadID:   "thread-1",
    Intent:     envelope.IntentAssert,
    Epistemic:  envelope.EpistemicInferred,
    Confidence: 0.5,
    Provenance: envelope.Provenance{Source: "model:self"},
    Payload:    "The build is green.",
}
mapped, _ := a2a.ToPart(m)
_ = mapped.ContextID // "thread-1"
_ = mapped.MessageID // "msg-1"

got, _ := a2a.FromPart(mapped)
_ = got.Payload // "The build is green."
```
