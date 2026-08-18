# dispatch

## Goal

`dispatch` receives newline-delimited envelope JSON over HTTP, runs
the full receive ladder per line, and answers with newline-delimited
ack JSON. One client helper sends messages and collects the acks.

## Scope

Inside:

- New package `dispatch` with `Options`, `Options` validation,
  `Handler` (the local interface), `New`, `Endpoint.Handler`, and
  `Send`.
- The receive ladder per line, in fixed order and fail-fast:
  `Decode`, `VerifySignature`, `Room.Accepts`, resolve, handle,
  build the ack (`NewAck`, `Confirm`, `Encode`).
- `agent.EmitMessageDelivered` after signature verification and
  `agent.EmitMessageAcked` after a confirmed ack. `New` always
  subscribes no-op handlers for both event names, so neither call
  can fail from a missing subscriber. Both calls are best-effort
  diagnostics, not ladder stages: the endpoint ignores their error
  return and never fails a line because of them.
- The error line: one JSON object `{"error":"..."}` for a line that
  fails decode, verify, admission, resolve, or handle (including
  `NewAck` construction). The stream stays open; later lines still
  process.
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
	Ack envelope.Ack
	Err error // set when the server answered an error line
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

- `New` rejects a blank `ID`, a nil `Room`, and a nil `Resolve`, in
  that order, before any wiring.
- `New` subscribes no-op handlers to the agent event names on the
  resolved bus, so `EmitMessageDelivered` and `EmitMessageAcked`
  never see an unsubscribed-name error. `Send` performs no
  subscription; it reads replies only.
- Per line, the reachable ladder runs in a fixed order and fails
  fast: decode, then verify, then admit, then resolve, then handle
  (including `NewAck` construction). Each reachable failure answers
  one error line naming the stage. `EmitMessageDelivered` and
  `EmitMessageAcked` sit outside this fail-fast ladder; their error
  return is structurally unreachable given the always-subscribed
  bus, so the endpoint ignores it.
- `Handler` implementations receive an already-verified,
  already-admitted message; the interface contract states this.
- `doc.go` states that `MessageDeliveredEvent` means "signature
  verified," not "room-admitted": the endpoint emits it after
  `VerifySignature` and before `Room.Accepts`, so a delivered event
  can still precede an admission rejection.

## Tests

`dispatch/dispatch_test/`, one external test package. Every case
uses `httptest.NewServer` unless noted otherwise:

- `options_test.go`, table-driven: every `New` sentinel plus the
  accept path.
- `ladder_test.go`: one signed member message yields one confirmed
  ack line whose `From` is the endpoint id and whose restatement is
  the handler's.
- `reject_test.go`: table-driven, one error line per case, followed
  by a good line in the same request that still succeeds:
  - an unsigned message (verify-stage failure);
  - a wrong-room message (admission-stage failure);
  - a failing `Handler.Handle` (handle-stage failure);
  - a `Handler.Handle` that returns `("", nil)`, which
    `envelope.NewAck` rejects with "restatement is required"
    (handle/NewAck-construction failure, reachable and distinct from
    the always-succeeding emit calls).
- `badrequest_test.go`: a POST with a body reader that returns an
  error answers 400. This case calls `Endpoint.Handler().ServeHTTP`
  directly against an `httptest.NewRequest` built with a hand-rolled
  broken `io.ReadCloser` body, not `httptest.NewServer`: a live
  server-and-client round trip cannot manufacture a server-side
  body-read error, since a broken client body only ever surfaces as
  a client-side transport error.
- `client_test.go`: `Send` over the same server returns acks in
  request order; a server error line surfaces as that entry's `Err`.
- `integration_test.go`: two processes in one test: a `dispatch`
  endpoint answering for one agent, and an `agent.AckWait` built
  from `Send`, proving the endpoint closes the loop the client side
  opened. Asserts `MessageDeliveredEvent` and `MessageAckedEvent`
  fire on both buses; does not assert either event blocks or fails
  the ladder.

## Verification

- `policy/layers.json` carries
  `"dispatch": ["agent", "envelope", "events", "room"]`.
- `make api-update` lands `api/dispatch.txt` in the same change.
- `make verify` passes; `dispatch` and the module total hold the 85
  floor.
- `go test -race ./dispatch/...` passes.
- `docs/plans/dispatch.md`, `docs/packages/dispatch.md`, and a
  `docs/examples/dispatch.md` walkthrough ship with the package.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
