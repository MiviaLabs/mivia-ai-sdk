# Phase 52: dispatch NDJSON envelope endpoint

Status: future. Plan-only; it has not gone through plan review yet.
Depends on no unshipped phase. It adds one new top-level package.

## Why this phase exists

The SDK sends but never receives. `a2aclient` and the planned
`a2aack` cover the client side. No block receives envelope bytes,
verifies them, admits them to a room, routes them to an owning
handler, and answers with a confirmed ack.

Two agents built with this SDK cannot talk over a network today.
The caller writes the whole receive loop by hand.

The user chose a stdlib-only NDJSON and HTTP endpoint over an
`a2a-go` server. The `a2a-go` server stays a separate, deferred
plan: phase 53.

## Goal

One `http.Handler` receives newline-delimited envelope JSON, runs
the full receive ladder per line, and answers with
newline-delimited ack JSON. One client helper sends messages and
collects acks.

## Scope

Inside:

- New package `dispatch` with `Options`, `Options` validation,
  `Handler` (the local interface), `New`, `Endpoint.Handler`, and
  `Send`.
- The receive ladder per line: `Decode`, `VerifySignature`,
  `agent.EmitMessageDelivered`, `Room.Accepts`, resolve, handle,
  ack.
- The reply: `NewAck` with the endpoint's id and the handler's
  restatement, `Confirm`, `Ack.Encode`, one line out, then
  `agent.EmitMessageAcked`.
- The error line: one JSON object `{"error":"..."}` for a line that
  fails decode, signature, admission, or handling. The stream stays
  open; later lines still process.
- Request-level failures answer with HTTP status codes, not stream
  lines: a non-POST method answers 405 with `ErrBadMethod`; an
  unreadable body answers 400 with `ErrBadRequest`.
- `Send`: one HTTP POST, the request body NDJSON of encoded signed
  messages, the response body parsed into `[]envelope.Ack` and error
  lines.

Outside:

- Any `a2a-go` or gRPC import. The endpoint speaks the envelope wire
  form only. This keeps the package stdlib-only.
- Any session state, streaming push, or task lifecycle. One request
  holds zero or more messages; the response answers each in order.
- Any threading of received messages into a `flow` run. The handler
  interface returns a restatement string; wiring an `agentrun`
  runner behind it stays caller code, shown in the walkthrough.
- TLS, auth beyond signatures, and rate limiting. Deployment
  concerns stay with the caller's reverse proxy.

## API

```go
package dispatch

// Handler resolves one received message into a restatement.
type Handler interface {
	Handle(ctx context.Context, m envelope.Message) (string, error)
}

type Options struct {
	ID      string // this endpoint's identity; the ack's From
	Room    *room.Room
	Resolve func(ctx context.Context, m envelope.Message) (Handler, error)
	Bus     *events.Bus // built and subscribed when nil
}

func New(opts Options) (*Endpoint, error)

type Endpoint struct{}

// Handler serves POST requests with NDJSON bodies.
func (e *Endpoint) Handler() http.Handler

// Send posts msgs and returns one entry per request line.
func Send(ctx context.Context, url string,
	msgs []envelope.Message) ([]SendResult, error)

type SendResult struct {
	Ack   envelope.Ack
	Err   error // set when the server answered an error line
}

var (
	ErrNoID       = errors.New("dispatch: endpoint id is required")
	ErrNoRoom     = errors.New("dispatch: room is required")
	ErrNoResolve  = errors.New("dispatch: resolve func is required")
	ErrBadMethod  = errors.New("dispatch: POST required")
	ErrBadRequest = errors.New("dispatch: request body read failed")
)
```

## Validation

- `New` rejects a blank `ID`, a nil `Room`, and a nil `Resolve`,
  in that order, before any wiring.
- `New` subscribes no-op handlers to the agent event names on the
  resolved bus, because the translators' `Emit` fails an
  unsubscribed name. `Send` performs no subscription; it reads
  replies only.
- Per line, the ladder runs in a fixed order and fails fast:
  decode, then verify, then admit, then resolve, then handle. Each
  failure answers one error line naming the stage.
- `Handler` implementations receive an already-verified,
  already-admitted message; the interface contract states this.

## Tests

`dispatch/dispatch_test/`, one external test package over
`httptest.Server`:

- `options_test.go`, table-driven: every `New` sentinel plus the
  accept path.
- `ladder_test.go`: one signed member message yields one confirmed
  ack line whose `From` is the endpoint id and whose restatement is
  the handler's.
- `reject_test.go`: an unsigned message, a wrong-room message, and a
  failing handler each yield one error line; the following line in
  the same request still succeeds.
- `client_test.go`: `Send` over the same server returns acks in
  request order; a server error line surfaces as that entry's `Err`.
- `integration_test.go`: two processes in one test: a `dispatch`
  endpoint answering for one agent, and an `agent.AckWait` built
  from `Send`, proving the endpoint closes the loop the client side
  opened. Asserts `MessageDeliveredEvent` and `MessageAckedEvent`
  on both buses.

## Verification

- `policy/layers.json` gains
  `"dispatch": ["agent", "envelope", "events", "room"]`.
- `make api-update` lands `api/dispatch.txt` in the same change.
- `make verify` passes; `dispatch` and the module total hold the 85
  floor.
- `go test -race ./dispatch/...` passes.
- `docs/plans/dispatch.md`, `docs/packages/dispatch.md`, and a
  `docs/examples/dispatch.md` walkthrough ship with the package.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
