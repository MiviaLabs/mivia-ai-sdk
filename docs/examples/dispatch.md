# Example: dispatch NDJSON endpoint

This walkthrough runs one message through a live `dispatch.Endpoint`
and reads back its confirmed ack. It closes the loop `docs/examples/
a2aack.md` opens on the client side: instead of a remote A2A task,
this walkthrough speaks the envelope wire form directly over HTTP.

## The shape

`dispatch.New` wires an `Options` value into an `Endpoint`. `ID` names
the endpoint in every ack it sends; `Room` gates admission; `Resolve`
looks up the `Handler` that owns each admitted message:

```go
type Handler interface {
	Handle(ctx context.Context, m envelope.Message) (string, error)
}
```

## Build the endpoint

A room founder identity doubles as the room member the example's
sender signs as:

```go
_, key, err := ed25519.GenerateKey(nil)
if err != nil {
	return fmt.Errorf("generate sender key: %w", err)
}
senderSigner := hex.EncodeToString(key.Public().(ed25519.PublicKey))

r, err := room.New("support-room", senderSigner)
if err != nil {
	return fmt.Errorf("build room: %w", err)
}

endpoint, err := dispatch.New(dispatch.Options{
	ID:   "support-agent",
	Room: r,
	Resolve: func(ctx context.Context, m envelope.Message) (dispatch.Handler, error) {
		return restater{}, nil
	},
})
if err != nil {
	return fmt.Errorf("build endpoint: %w", err)
}
```

Size `Options.ReplayLease` above `Handler.Handle`'s expected p99
latency. `taskrun.Run` claims the replay key once, then calls `Handle`
synchronously with no lease renewal. A lease shorter than `Handle`'s
real latency re-runs `Handle` on an ordinary slow call, not only on a
crash. An `agentrun`-backed `Handler` can exceed the 30-second
`DefaultReplayLease` during normal operation, so size `ReplayLease`
for that handler explicitly; do not rely on the default.

`restater` is any type implementing `Handler`, for example one that
echoes the payload back with a prefix:

```go
type restater struct{}

func (restater) Handle(ctx context.Context, m envelope.Message) (string, error) {
	return "received: " + m.Payload, nil
}
```

Serve the endpoint over a live listener:

```go
srv := httptest.NewServer(endpoint.Handler())
defer srv.Close()
```

## Send one message

Sign a message with the sender's key, then send it through `Send`:

```go
msg, err := envelope.Sign(key, envelope.Message{
	Version:    envelope.Version,
	ID:         "msg-1",
	Room:       "support-room",
	ThreadID:   "thread-1",
	Intent:     envelope.IntentAssert,
	Epistemic:  envelope.EpistemicAssumed,
	Confidence: 0.9,
	Payload:    "the build is failing",
})
if err != nil {
	return fmt.Errorf("sign message: %w", err)
}

results, err := dispatch.Send(ctx, srv.URL, []envelope.Message{msg})
if err != nil {
	return fmt.Errorf("send: %w", err)
}
```

## What the program shows

`results` holds one `SendResult` for the one message sent. Its `Ack`
carries `From: "support-agent"` and
`Restatement: "received: the build is failing"`, with
`Status: envelope.AckConfirmed`. `results[0].Err` is nil: the message
passed every ladder stage — decode, signature, room admission,
resolve, and handle — before the endpoint built and confirmed the
ack.

## Replay protection

`Options.Ledger` defaults to a bounded in-memory ledger when left
nil, so `endpoint` above already rejects a replayed message. Tune the
default ledger's memory footprint without opting out of the
convenience by setting `ReplayCapacity`:

```go
endpoint, err := dispatch.New(dispatch.Options{
	ID:             "support-agent",
	Room:           r,
	Resolve:        resolve,
	ReplayCapacity: 10000,
})
```

Supply a caller-built `*ledger.Ledger` to own the store, its
capacity, and its eviction policy directly:

```go
led, err := ledger.New(ledger.NewMemStore(), nil)
if err != nil {
	return fmt.Errorf("build ledger: %w", err)
}

endpoint, err := dispatch.New(dispatch.Options{
	ID:      "support-agent",
	Room:    r,
	Resolve: resolve,
	Ledger:  led,
})
```

## Wiring into agent.Run

A caller resolving a gated `flow.Step`'s ack over this transport wraps
`Send` into an `agent.AckWait`:

```go
ackWait := agent.AckWait(func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
	results, err := dispatch.Send(ctx, srv.URL, []envelope.Message{msg})
	if err != nil {
		return envelope.Ack{}, err
	}
	return results[0].Ack, results[0].Err
})
```

`a.Run` then drives the same signed-message, confirmed-ack loop this
walkthrough runs by hand, one step at a time.
