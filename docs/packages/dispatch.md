# Package reference: dispatch

The `dispatch` package receives newline-delimited `envelope.Message`
JSON over HTTP, runs the full receive ladder per line, and answers
with newline-delimited `envelope.Ack` JSON. One client helper, `Send`,
posts messages and collects the replies. It is stdlib-only: it speaks
the envelope wire form over `net/http`, with no `a2a-go` or gRPC
import. The exported surface below mirrors `api/dispatch.txt`.

## Types

- `Handler` — resolves one received message into a restatement.
  Implementations receive an already-verified, already-admitted
  message.
- `Options` — configures `New`. `ID` becomes each built ack's `From`.
  `Room` gates admission. `Resolve` looks up the `Handler` that owns
  an admitted message. `Bus` receives `MessageDeliveredEvent` and
  `MessageAckedEvent`; built and subscribed when nil.
- `Endpoint` — the built receiver. Build one with `New`.
- `SendResult` — one reply line's outcome, in request order: `Ack`
  holds the decoded ack; `Err` is set when the server answered an
  error line or when the reply line fails to decode.

## Functions

- `Options.Validate` — checks `ID`, `Room`, and `Resolve`, in that
  order, and returns the first sentinel that fails.
- `New(opts Options)` — validates `opts`, builds a `Bus` when
  `opts.Bus` is nil, subscribes a no-op handler for
  `MessageDeliveredEvent` and `MessageAckedEvent` on the resolved bus,
  and returns the wired `Endpoint`.
- `(*Endpoint).Handler()` — returns an `http.Handler` that serves POST
  requests with NDJSON bodies.
- `Send(ctx, url, msgs)` — posts `msgs` as one NDJSON request and
  returns one `SendResult` per reply line, in the order the server
  answered.

## The receive ladder

`Endpoint.Handler` answers a non-POST method with 405 and
`ErrBadMethod`. It answers an unreadable body with 400 and
`ErrBadRequest`. A readable body splits into lines on `\n`; each
non-blank line runs the ladder in fixed order, and each stage is
fail-fast:

1. `envelope.Decode` — malformed JSON or an invalid message fails
   here.
2. `Message.VerifySignature` — an unsigned or tampered message fails
   here.
3. `agent.EmitMessageDelivered`, best-effort: its error return is
   ignored, so it never fails the line. This is why
   `MessageDeliveredEvent` means "signature verified," not
   "room-admitted": it fires here, before the next stage.
4. `Room.Accepts` — a message naming the wrong room, or a signer or
   recipient outside the roster, fails here.
5. `Options.Resolve` — a lookup failure for the message fails here.
6. `Handler.Handle` — a handler error, or `envelope.NewAck` rejecting
   a blank restatement, fails here.
7. `Ack.Confirm`, `agent.EmitMessageAcked` (best-effort, error
   ignored), then `Ack.Encode` build the reply line.

A failing stage answers one JSON object, `{"error":"..."}`, naming the
stage, and the stream stays open: the next line in the same request
still runs the full ladder.

## Failure modes

- `ErrNoID` ("dispatch: endpoint id is required") —
  `Options.Validate`/`New` returns it when `ID` is blank. Pinned by
  `dispatch_test/options_test.go`.
- `ErrNoRoom` ("dispatch: room is required") —
  `Options.Validate`/`New` returns it when `Room` is nil. Pinned by
  `dispatch_test/options_test.go`.
- `ErrNoResolve` ("dispatch: resolve func is required") —
  `Options.Validate`/`New` returns it when `Resolve` is nil. Pinned
  by `dispatch_test/options_test.go`.
- `ErrBadMethod` ("dispatch: POST required") —
  `Endpoint.Handler`'s `http.Handler` writes it as an HTTP 405 body
  when the request method is not POST. The handler never returns this
  value to a Go caller, so `dispatch_test/badrequest_test.go` checks
  the status code, not `errors.Is`. This is a weak pin. `Send`
  recognizes a 405 response and wraps `ErrBadMethod`, so a client-side
  caller matches it with `errors.Is`; pinned by
  `TestSendMatchesBadMethodSentinel` in
  `dispatch/dispatch_test/client_test.go`.
- `ErrBadRequest` ("dispatch: request body read failed") —
  `Endpoint.Handler`'s `http.Handler` writes it as an HTTP 400 body
  when the request body fails to read. Same weak-pin note as
  `ErrBadMethod`: `dispatch_test/badrequest_test.go` checks the
  status code only. `Send` recognizes a 400 response and wraps
  `ErrBadRequest`, so a client-side caller matches it with
  `errors.Is`; pinned by `TestSendMatchesBadRequestSentinel` in
  `dispatch/dispatch_test/client_test.go`.

## Scope

`dispatch` carries no session state, no streaming push, and no task
lifecycle: one request holds zero or more messages, and the response
answers each in order. It threads no received message into a `flow`
run; a caller wires a handler that calls into `agentrun` or any other
runner on its own. TLS, auth beyond signatures, and rate limiting stay
with the caller's reverse proxy.
