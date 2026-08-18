# Example: a2aack remote step ack

This walkthrough resolves one gated step through a remote A2A task.
`a2aack.Wait` turns an `a2aclient.Client` into an `agent.AckWait`.
The step's message goes out as a remote task; the remote agent's reply
becomes the confirmed ack's restatement.

## The shape

`a2aclient.Client` already sends a signed message, polls task state,
and fetches the result. `a2aack` closes the loop with the composition
layer. `Wait` depends on the minimal `Remote` interface, so
`*a2aclient.Client` works without change:

```go
type Remote interface {
	Send(ctx context.Context, msg envelope.Message) (a2aclient.TaskHandle, error)
	Status(ctx context.Context, h a2aclient.TaskHandle) (a2aclient.State, error)
	Result(ctx context.Context, h a2aclient.TaskHandle) (envelope.Message, error)
}
```

## One exchange

Build a client that talks to one remote agent, then build the
`AckWait`:

```go
client, err := a2aclient.New("agent.example.invalid:443")
if err != nil {
	return fmt.Errorf("dial remote agent: %w", err)
}

ackWait, err := a2aack.Wait(client, a2aack.Options{
	Poll:    100 * time.Millisecond,
	Timeout: 30 * time.Second,
})
if err != nil {
	return fmt.Errorf("build ack wait: %w", err)
}
```

`Wait` validates eagerly. A nil client returns `ErrNoClient`. Invalid
options return their error before any task is sent.

Give the `AckWait` to an `agent.Agent` run as the resolver for its
gated steps:

```go
status, _, err := ag.Run(ctx, threadID, machineDef, start, ackWait, bus, nil, "", nil)
```

For each gated step, the run:

1. signs the step message with the agent's identity and chains it, then
2. calls `ackWait`, which sends the message as a remote task,
3. polls `Status` every `Poll` until a terminal state,
4. on success, fetches `Result`, re-verifies the result's signature,
   and resolves the step's ack,
5. confirms the ack, and the step counts as done.

## What the ack carries

`MessageID` keys off the sent step message's id. `From` is the remote
agent's signer, and the restatement is the remote reply's payload.
The signature check happens on the caller's side of the transport, so
a tampered reply never reaches the confirmed ack.

## Terminal outcomes

- `StateCompleted` resolves a confirmed ack.
- `StateFailed` and `StateCanceled` return an error wrapping
  `ErrRemoteFailed`.
- The `Timeout` deadline or `ctx` cancellation returns an error
  wrapping `ErrTimeout` with the last seen state.

A failed remote task is not retried here. Retry stays with
`flow.Step.Retry` around the gated step, its designed home.