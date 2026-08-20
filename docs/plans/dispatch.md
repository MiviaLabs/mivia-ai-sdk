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
  unreadable body answers 400 with `ErrBadRequest`. A body past
  `Options.MaxBodyBytes` answers 400 the same way.
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

Warning for `Options.ReplayLease` (see the replay-protection gap fix
below): size `ReplayLease` above `Handler.Handle`'s expected p99
latency, not as a crash-detection timeout. A handler slower than the
lease re-runs its side effect on any replay, crash or not. This
matters most for a slow, `agentrun`-backed `Handler`; `docs/examples/
dispatch.md` must carry the same warning next to its `Handler`
walkthrough.

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
	ErrBadMaxBody = errors.New("dispatch: max body bytes must not be negative")
)

// DefaultMaxBodyBytes caps one NDJSON request body when
// Options.MaxBodyBytes is zero.
const DefaultMaxBodyBytes int64 = 1 << 20
```

## Validation

- `New` rejects a negative `MaxBodyBytes`. A zero value resolves to
  `DefaultMaxBodyBytes`, so the endpoint is never unbounded. The
  handler wraps the request body in `http.MaxBytesReader` before
  `io.ReadAll`. Without the cap one request can commit unbounded
  memory before any line runs the ladder.
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

### Gap fix: Send matches ErrBadMethod and ErrBadRequest

Status: planned, not yet built. `ErrBadMethod` and `ErrBadRequest` are
already exported sentinels, but `Endpoint.Handler`'s `http.Handler`
only ever writes them through `http.Error(w, ErrX.Error(), code)`. An
`http.Handler` cannot return a Go `error`, so no Go caller reaches
these sentinels through that path. `docs/packages/dispatch.md`'s
Failure modes section already names both as weak pins for this
reason.

`Send`, the client-side function in `dispatch/client.go`, is the cheap
fix: it already reads the whole response body but never checks
`resp.StatusCode`, so a 405 or 400 response's plain-text body is fed
into `parseReply`, which cannot parse it as NDJSON and returns a
useless decode error, dropping the original message entirely.

`dispatch/endpoint.go:33-40` enforces a fixed 1:1 mapping, in the same
package that owns both sentinels: status 405 only ever means
`ErrBadMethod`, and status 400 only ever means `ErrBadRequest`. `Send`
can key off `resp.StatusCode` alone; it does not need to read or
compare the response body at all. A status-code switch is simpler
than body matching and stays correct even if a reverse proxy or other
middleware between `Send` and the endpoint rewrites or truncates the
plain-text body: the status code is the one signal a proxy is least
likely to change silently.

The build, in `dispatch/client.go`:

- After the existing `io.ReadAll(resp.Body)` call and before calling
  `parseReply`, check `resp.StatusCode`. When it is not `http.StatusOK`,
  return early with a matching error instead of calling `parseReply`.
- Add a new unexported helper, `requestFailure(status int) error`,
  that switches on the status code and returns a wrap of the matching
  sentinel; any other non-200 status returns a plain error carrying
  the status code.

```go
// requestFailure maps a non-200 Endpoint response status to the
// matching sentinel. Endpoint.Handler's serveHTTP only ever answers
// 405 for ErrBadMethod and 400 for ErrBadRequest (dispatch/endpoint.go),
// so the status code alone identifies the failure; no body read is
// required.
func requestFailure(status int) error {
    switch status {
    case http.StatusMethodNotAllowed:
        return fmt.Errorf("dispatch: request failed with status %d: %w", status, ErrBadMethod)
    case http.StatusBadRequest:
        return fmt.Errorf("dispatch: request failed with status %d: %w", status, ErrBadRequest)
    default:
        return fmt.Errorf("dispatch: request failed with status %d", status)
    }
}
```

`Send` gains one new call, no new import:

```go
reply, err := io.ReadAll(resp.Body)
if err != nil {
    return nil, err
}
if resp.StatusCode != http.StatusOK {
    return nil, requestFailure(resp.StatusCode)
}
return parseReply(reply), nil
```

This is safe against the success path: `Endpoint.Handler`'s
per-line-error path still answers HTTP 200 with a JSON error line in
the body (`serveHTTP` never calls `WriteHeader` for that case, so Go's
default is 200). Only the two request-level failures, and any other
non-2xx status a caller's own middleware might add in front of the
endpoint, take this new branch.

No exported symbol changes: `requestFailure` stays unexported, and
`Send`'s signature is unchanged. `make api-update` produces no diff
for `api/dispatch.txt` in this gap fix. No `policy/layers.json` edit.

Test, in `dispatch/dispatch_test/client_test.go`:

- `TestSendMatchesBadMethodSentinel` — build an `httptest.NewServer`
  with a raw `http.HandlerFunc` that calls
  `http.Error(w, dispatch.ErrBadMethod.Error(),
  http.StatusMethodNotAllowed)`, mirroring `Endpoint.Handler`'s own
  bad-method write. Call `dispatch.Send` against it and assert
  `errors.Is(err, dispatch.ErrBadMethod)`.
- `TestSendMatchesBadRequestSentinel` — same shape, with
  `http.Error(w, dispatch.ErrBadRequest.Error(), http.StatusBadRequest)`.
  Assert `errors.Is(err, dispatch.ErrBadRequest)`.
- Update `docs/packages/dispatch.md`'s Failure modes entries for
  `ErrBadMethod` and `ErrBadRequest`: each now also carries a
  `Send`-side pin, in addition to the existing status-code pin. See
  the docs deliverable list below.

### Gap fix: replay protection for the receive ladder

Status: planned, not yet built. `Endpoint.processLine`
(`dispatch/ladder.go`) has no idempotency check. A validly signed
message replayed by the network, a client retry, or a captured
request runs `resolve` and `Handler.Handle` again on every replay,
re-running the handler's side effect. `envelope.Message.ID` is
"unique within `ThreadID`" only (`envelope/message.go:62`), so no
per-message field alone identifies a submission across two separate
HTTP requests, and nothing in `dispatch` tracks processed IDs today.

This gap fix wraps the resolve-and-handle stage in the ledger's
admit-claim-complete ceremony, through the `taskrun` package
(`docs/plans/taskrun.md`), instead of calling `ledger.Ledger` methods
directly. `taskrun.Run` already returns a named sentinel
(`ErrTaskDone`, `ErrTaskFailed`, `ErrTaskBlocked`) for a key already
terminal in the ledger, without running its work func — this is
exactly the replay check this gap needs, built and tested already.

#### Ladder position

The new stage sits between `Room.Accepts` and `resolve`:

```
Decode -> VerifySignature -> Room.Accepts -> Replay -> resolve -> handle -> NewAck -> Confirm -> Encode
```

Two reasons fix it there, not earlier and not later:

- After `Room.Accepts`: a message a room would reject never consumes
  a ledger admission slot. Checking replay before room admission lets
  an attacker fill the ledger with wrong-room submissions that never
  reach a handler anyway.
- Before `resolve`/`handle`: the side effect this gap protects lives
  in `resolve` and `Handler.Handle`. The replay check must run before
  either call, or a duplicate still triggers the side effect once
  more before being caught.

`EmitMessageDelivered`'s position does not move. It still sits between
`VerifySignature` and `Room.Accepts`, outside the fail-fast ladder, the
same place it holds today (`dispatch/ladder.go:23-26`). This gap fix
inserts only the new `Replay` stage; it does not touch the emit call's
position.

`NewAck` construction moves inside the replay-guarded work func too,
matching the existing plan language that already groups it with
"handle" ("handle (including `NewAck` construction)", `docs/plans/
dispatch.md` Scope section above). A `NewAck` failure is
deterministic given the same restatement, so completing the ledger
record `StatusFailed` on that outcome is correct: a retry of the same
bad message returns `ErrTaskFailed`, not a second identical failure.

#### Key construction

The replay key must combine `ThreadID` and `ID`, since `ID` alone is
only unique within one `ThreadID` (`envelope/message.go:62`,
`Message.Validate`, `envelope/message.go:107-181`, enforces no
narrower rule). A plain `ThreadID + ":" + ID` concatenation is
ambiguous: `ThreadID="a:b", ID="c"` and `ThreadID="a", ID="b:c"` both
concatenate to `"a:b:c"`. `dispatch/ladder.go` gains an unexported
helper using a length-prefixed encoding, which is injective without
hashing:

```go
// replayKey builds the ledger.IdempotencyKey for m. The len(ThreadID)
// prefix, terminated by the first ':', disambiguates the ThreadID/ID
// boundary: a reader parses the leading digits up to ':', takes
// exactly that many bytes as ThreadID, and the remainder as ID. ID is
// unique only within ThreadID (envelope/message.go), so the key must
// carry both.
func replayKey(m envelope.Message) ledger.IdempotencyKey {
	return ledger.IdempotencyKey(fmt.Sprintf("%d:%s%s", len(m.ThreadID), m.ThreadID, m.ID))
}
```

#### Options: Ledger is optional, nil means build one

`Options` gains three new fields:

```go
type Options struct {
	// ... existing fields unchanged ...

	// Ledger provides replay protection over the receive ladder. Built
	// as a bounded in-memory ledger, sharing Bus for its events, when
	// nil.
	Ledger *ledger.Ledger
	// ReplayLease bounds one line's replay claim. Zero resolves to
	// DefaultReplayLease. A negative value fails Validate. Size this
	// above Handler.Handle's expected p99 latency; see the warning in
	// the Scope section above.
	ReplayLease time.Duration
	// ReplayCapacity caps the entry count of the ledger New builds
	// internally when Ledger is nil. Zero resolves to
	// DefaultReplayCapacity. A negative value fails Validate. Ignored
	// when Ledger is set; the caller-supplied Ledger owns its own
	// Store's capacity.
	ReplayCapacity int
}
```

`New` builds a default `*ledger.Ledger` when `opts.Ledger` is nil,
over a `ledger.MemStore` bounded by the resolved `ReplayCapacity`,
sharing the endpoint's resolved `Bus` for `ledger`'s own events
(`AdmittedEvent`, `ClaimedEvent`, `CompletedEvent`, `BlockedEvent`):
this is a diagnostic bonus, not a required wiring, following the
existing best-effort-diagnostics pattern this package already uses
for `agent.EmitMessageDelivered`/`EmitMessageAcked`.

This follows the "nil means build one" pattern for `Options.Ledger`.
`Options.Bus`'s own default build is behavior-neutral: nothing
subscribes to a fresh bus except this package's two no-op diagnostic
handlers, so a caller sees identical behavior whether it supplies
`Bus` or not. `Options.Ledger`'s default build is not neutral in the
same way: it changes what the endpoint rejects, and it commits real
memory sized by `ReplayCapacity`. The `Options.Bus` precedent justifies
the "nil means build one" shape, not the specific default capacity.
The `ReplayCapacity` field exists because that gap matters: a caller
can tune the default-built ledger's memory footprint without opting
out of the convenience by supplying a whole `*ledger.Ledger` itself.

`DefaultReplayCapacity` (100000) is a starting default, not a measured
figure. `ledger.MemStore`'s per-record cost is small (a status, a
lease deadline, and the caller's `Task`/`Needs` values), so 100000
entries stays well under typical single-process memory budgets. Revise
this constant once real traffic data from a deployed endpoint exists.

New constants, in `dispatch/options.go`:

```go
// DefaultReplayLease bounds one line's replay claim when
// Options.ReplayLease is zero. This is not a crash-detection timeout:
// size it above Handler.Handle's expected p99 latency, or a slow
// handler's own replay can re-run work before its first claim
// completes. See the Scope section warning above.
const DefaultReplayLease = 30 * time.Second

// DefaultReplayCapacity caps the entry count of the ledger New builds
// internally when Options.Ledger is nil. A caller-supplied Ledger is
// not bounded by this constant; the caller owns its Store's capacity.
const DefaultReplayCapacity = 100000
```

`Options.Validate` gains two more checks, after `MaxBodyBytes`: a
negative `ReplayLease` returns the new sentinel `ErrBadReplayLease`; a
negative `ReplayCapacity` returns the same sentinel, since both guard
one replay-configuration concern and a distinct sentinel per field
adds no caller-visible value.

New sentinel, in `dispatch/options.go`, alongside the package's other
`Options.Validate` sentinels:

```go
// ErrBadReplayLease reports a negative Options.ReplayLease or a
// negative Options.ReplayCapacity. Options.Validate returns this
// sentinel for either field.
var ErrBadReplayLease = errors.New("dispatch: replay lease and capacity must not be negative")
```

`New` resolves the actor and owner identities for the ceremony from
the endpoint's own `ID` field: `ledger.Actor(opts.ID)` and
`ledger.OwnerID(opts.ID)`. No new required `Options` field: one
identity already exists and the endpoint is the only claimant.

#### Response on a detected replay

`taskrun.Task.Task` (the ledger's stored payload, `any`) holds only
`Description`, a caller-chosen label; `taskrun` never stores the
original ack. Replaying the original ack line is not cheaply
achievable without a second, dedicated ack-store, which this gap
does not take on. `dispatch` answers a detected replay with a
distinct error line instead, matching the package's existing
"one JSON error object per failed stage" convention.

New sentinel, in `dispatch/options.go`:

```go
// ErrReplay reports a message the ledger already admitted: a
// completed, failed, or blocked key, or a key still claimed by an
// in-flight duplicate. Endpoint.Handler answers this with a "replay:"
// error line instead of running resolve or handle again.
var ErrReplay = errors.New("dispatch: message already processed")
```

`dispatch/ladder.go` maps four ledger/taskrun outcomes to `ErrReplay`:
`taskrun.ErrTaskDone`, `taskrun.ErrTaskFailed`, `taskrun.ErrTaskBlocked`,
and `ledger.ErrLeaseActive` (an in-flight duplicate: the original
claim has not completed yet). Any other error `taskrun.Run` returns
is either the work func's own error, already prefixed `"resolve: "`
or `"handle: "` from inside the closure, or an operational ledger/
store fault; both cases pass through `encodeErrorLine` unchanged, so
existing stage-prefixed error text is unaffected.

```go
// isReplay reports whether err is one of the ledger outcomes that
// mean "this key already has, or is already getting, an admitted
// outcome": a terminal record, or a live claim held by an in-flight
// duplicate.
func isReplay(err error) bool {
	return errors.Is(err, taskrun.ErrTaskDone) ||
		errors.Is(err, taskrun.ErrTaskFailed) ||
		errors.Is(err, taskrun.ErrTaskBlocked) ||
		errors.Is(err, ledger.ErrLeaseActive)
}
```

`processLine`'s new shape, replacing the `resolve`/`handle`/`NewAck`
block:

```go
var ack envelope.Ack
work := func(ctx context.Context) error {
	h, err := e.resolve(ctx, m)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	restatement, err := h.Handle(ctx, m)
	if err != nil {
		return fmt.Errorf("handle: %w", err)
	}
	ack, err = envelope.NewAck(m, e.id, restatement)
	if err != nil {
		return fmt.Errorf("handle: %w", err)
	}
	return nil
}
task := taskrun.Task{Key: replayKey(m), Seq: 1, Description: string(m.Intent)}
if err := taskrun.Run(ctx, e.taskOpts, task, work); err != nil {
	if isReplay(err) {
		return encodeErrorLine(fmt.Errorf("replay: %w", ErrReplay))
	}
	return encodeErrorLine(err)
}
ack = ack.Confirm()
```

`Task.Seq` is the constant `1` for every line: dispatch has no
resubmission-ordering concept, only a one-shot admit. `Admit`
rejects a second submission at the same key whose `Seq` is not
greater than the stored one (`ledger/ledger.go`, `admitEligible`), so
a constant `Seq` of `1` makes every replay at the same key ineligible
to rebase, which is the property this gap needs.

`Endpoint` gains one new unexported field, `taskOpts taskrun.Options`,
built once in `New` and reused by every `processLine` call.

#### Accepted limitation: a short lease re-runs work, not only on a crash

The re-run window opens whenever `ReplayLease` is shorter than
`Handler.Handle`'s actual latency for that message. A crashed claimant
is one case of this general rule, not the whole rule. `taskrun.Run`
claims once and calls `work(ctx)` synchronously, with no lease renewal
while `work` runs (`taskrun/taskrun.go:57-99`). So any handler slower
than the configured lease hits this window during ordinary operation,
whether or not the claimant crashes.

Concretely: a message whose original claim's lease
(`ReplayLease`/`DefaultReplayLease`) expires before `Complete` runs
leaves the record `StatusClaimed` with a past `LeaseUntil`, crashed
claimant or not. A later replay of the same message then finds `Claim`
eligible again (`ledger/claim.go`) and reruns `work` once. This is
`taskrun`'s own crash-recovery behavior, not a defect in this gap fix:
it trades "replay never re-runs work" for "a claim past its lease
eventually gets a retry." Document this in `docs/packages/dispatch.md`'s
Failure modes section; do not treat it as a bug to fix in this gap.

This matters because the Scope section above anticipates an
`agentrun`-backed `Handler` behind `Handle`. An LLM-backed handler can
exceed a 30-second default lease during normal operation, not only
during a crash, so `ReplayLease` needs deliberate sizing for that
handler, not the default. See the required warning in the Scope
section above and in `docs/examples/dispatch.md`.

Design note: should `Options.Validate` refuse an unreasonably short
`ReplayLease`? `Validate` cannot know a caller's real handler latency,
so it picks no floor tied to that. It does reject a nonzero
`ReplayLease` under one second, a fixed sanity floor distinct from a
latency guess. This catches a unit-confusion bug, for example a
duration meant as milliseconds landing in the field as nanoseconds,
independent of any assumption about a real handler's speed. A zero
`ReplayLease` still selects `DefaultReplayLease` and passes `Validate`
unchanged. The documentation warning stays the primary defense against
a lease sized below actual handler latency; a numeric floor tied to
that latency is future work once real handler-latency data exists.

#### Bounded memory

The paragraph that stood here was disproved. It claimed that
`ledger.MemStore` tombstones a terminal record under
`MemStoreOptions.MaxEntries`, and that idempotency holds across
eviction. The tombstone freed no map entry, so the cap bounded nothing.
"Bounded replay window" below carries the corrected contract.

#### policy/layers.json

The `dispatch` row grows from
`["agent", "envelope", "events", "room"]` to
`["agent", "envelope", "events", "ledger", "room", "taskrun"]`.
`dispatch` needs `ledger` directly for the `*ledger.Ledger` `Options`
field and the default-build path, and `taskrun` for `Run`, `Task`,
`Options`, and the three replay sentinels. Neither `ledger` nor
`taskrun` imports `agent`, `dispatch`, or any package that imports
either, so this adds no cycle.

#### Tests

New file `dispatch/dispatch_test/replay_test.go`, table-driven where
the case set grows:

- `TestReplayHandlerRunsOnce` — a `Handler` whose `Handle` increments
  a counter. Post the same signed message twice over one
  `httptest.NewServer`. Assert the first reply is a confirmed ack,
  the second is a `"replay:"` error line matching
  `dispatch.ErrReplay` (parsed via `decodeErrorLine` or a string
  match), and the counter reads exactly `1`.
- `TestReplayOrderPrecedesVerify` — a first-time submission proves
  nothing about ladder order: an unadmitted key is never a replay
  either way, so a single unsigned post cannot distinguish
  verify-then-replay from replay-then-verify. Instead: post one valid
  signed message (it admits and completes normally, asserted through
  the confirmed ack). Post a second message at the same `ThreadID`/`ID`
  but with a tampered signature. Assert the reply is a `"verify:"`
  error line, not a `"replay:"` line: if verify ran first, the bad
  signature is caught before the replay check runs; if the replay
  check ran first, the reply would be `"replay:"` instead. This proves
  verify's position precedes replay's.
- `TestReplayDifferentMessagesBothProcess` — two distinct signed
  messages (different `ID`, same `ThreadID`) both produce confirmed
  acks; the handler counter reads `2`.
- `TestReplayConcurrentDuplicates` — `N` goroutines (`N >= 8`) POST
  the same signed message concurrently against one server. Assert the
  handler ran exactly once (counter `== 1`) and every reply line is
  either a confirmed ack or a `"replay:"` `dispatch.ErrReplay` line;
  document in the test comment that a concurrent duplicate may
  observe `ledger.ErrLeaseActive` (still in flight) or
  `taskrun.ErrTaskDone` (already completed), and both map to the same
  wire-visible `ErrReplay` line, so the test asserts the counter and
  the reply shape, not which specific sentinel a given duplicate saw.
  Run with `go test -race`.
- `TestReplayKeyDistinguishesThreadBoundary` — an internal test
  (`dispatch` package, not `dispatch_test`) asserting `replayKey`
  produces distinct keys for `{ThreadID: "a:b", ID: "c"}` and
  `{ThreadID: "a", ID: "b:c"}`, proving the length-prefixed encoding
  resolves the concatenation ambiguity a plain `":"` join would not.
  Lives beside the existing `dispatch/ladder_internal_test.go`
  pattern.
- `TestOptionsRejectsBadReplayLease` — extends the existing
  `options_test.go` table with a negative `ReplayLease` case and a
  negative `ReplayCapacity` case, each asserting
  `errors.Is(err, dispatch.ErrBadReplayLease)`.
- `TestNewBuildsDefaultLedger` — `New` with no `Options.Ledger` set,
  then a normal round trip through `Handler`, proves the zero-config
  path still gets replay protection (reuses
  `TestReplayHandlerRunsOnce`'s shape against an `Endpoint` built with
  `Options.Ledger` left nil).

#### Verification

- `policy/layers.json` carries the updated `dispatch` row above.
- `make api-update` lands the `api/dispatch.txt` delta: three new
  `Options` fields (`Ledger *ledger.Ledger`, `ReplayLease
  time.Duration`, `ReplayCapacity int`), two new constants
  (`DefaultReplayLease`, `DefaultReplayCapacity`), and two new
  sentinels (`ErrReplay`, `ErrBadReplayLease`). No signature changes
  to any existing exported symbol.
- `make verify` passes; `dispatch` and the module total hold the 85
  floor.
- `go test -race ./dispatch/...` passes, including
  `TestReplayConcurrentDuplicates`.
- `docs/packages/dispatch.md` gains a replay-protection subsection
  and the two new Failure modes entries (`ErrReplay`,
  `ErrBadReplayLease`), plus the lease-sizing accepted-limitation
  note naming its real trigger (a lease shorter than handler latency,
  not only a crash).
- `docs/examples/dispatch.md` notes that `Options.Ledger` defaults to
  a bounded in-memory ledger, shows the one-line override for a
  caller-supplied `*ledger.Ledger`, shows setting `ReplayCapacity`
  without opting out of the default ledger, and carries the
  `ReplayLease` p99-sizing warning next to its `Handler` walkthrough.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
- `scripts/mutation_denylist/dispatch.json` locks a mutation floor of
  95, from a measured 95.24% (40 killed, 2 survived). Two survivors
  remain: `decodeErrorLine`'s malformed-JSON branch and
  `resolveReplayCapacity`'s zero-selects-default branch, both pure
  unexported logic with no behavior difference `dispatch_test`
  (the external package `check_mutation.py` runs) can observe through
  the public API. `MemStore` eviction still enforces idempotency
  regardless of capacity size, so the two configurations are
  black-box indistinguishable at reasonable test cost. `make
  mutation-gate` includes `dispatch` at this floor.

### Bounded replay window

Status: planned. This section corrects a confirmed false claim about
`ReplayCapacity` and records the new replay semantics.

#### The defect this depends on

`ledger.MemStore` never deleted an evicted record, so
`Options.ReplayCapacity` bounded no memory. `dispatch.Endpoint` is an
HTTP surface and `dispatch/ladder.go:20-22` mints one replay key per
`(ThreadID, ID)` pair, so a remote caller grew the map without a
bound. An aborted request left a `StatusClaimed` record that no
eviction path could ever reach. The fix lives in `ledger`. See
`docs/plans/ledger.md`, "Bounded MemStore with lease reclamation".

#### No production change in dispatch

`New` already builds `ledger.NewMemStoreWithOptions` with
`MaxEntries` set from the resolved capacity. `MemStoreOptions.Now`
defaults to `time.Now`, which is the right clock for an endpoint. So
`dispatch` needs doc corrections and tests only.

#### The corrected contract

Replay protection is now a bounded window, not a permanent guarantee.

- A key evicted under `ReplayCapacity` is processed again if it
  arrives later. The endpoint answers a fresh ack, not `ErrReplay`.
- `ReplayCapacity` bounds the records that hold no live claim. It does
  not bound the records that do. A record claimed by an in-flight
  `Handle` call is never evicted.
- An aborted or panicking request leaves a claimed record. That record
  becomes evictable one `ReplayLease` after its claim, and eviction
  deletes it on a later write. It no longer pins memory forever.
- A pending record can be evicted between `Admit` and `Claim` under
  cap pressure. `taskrun.Run` then returns `ledger.ErrNoKey`, which
  `isReplay` does not match, so the line answers an error, not a
  replay.
- An expired claim can be evicted while its own handler still runs.
  `taskrun.Run`'s `Complete` then returns `ledger.ErrNoKey` after the
  work already succeeded. `isReplay` does not match `ledger.ErrNoKey`
  (`dispatch/ladder.go:36-42`), so `processLine` falls to
  `encodeErrorLine`, discards a correctly computed ack, and returns a
  raw internal ledger error string to a remote client. Treat that line
  shape as a known limit of a cap sized below handler latency. Sizing
  `ReplayLease` above handler p99 latency, which this plan already
  requires, keeps the window shut.

Memory has two terms, and one mitigation does not cover both.

The live-handler term is bounded by in-flight request count. A
concurrency cap at the reverse proxy bounds it.

The aborted term is not. An aborted request is no longer in flight, yet
it holds its record for a full `ReplayLease`, 30 seconds by default
(`dispatch/options.go:68`). Its size is roughly the abort rate times
`ReplayLease`, which is orders of magnitude above the concurrency cap
when round-trip time is short. Bounding it needs a request-RATE limit,
not a concurrency limit. State both mitigations, and do not offer the
concurrency cap alone.

#### Doc sites corrected

- `dispatch/options.go:51`, the `ReplayCapacity` doc comment. State
  the bounded window and the live-claim exemption.
- `dispatch/options.go`, the `DefaultReplayCapacity` doc comment. Same
  rule, same words.
- `docs/packages/dispatch.md:73-78`, "Replay protection". Replace the
  permanent-protection wording with the bounded window, and add the
  aborted-request behavior.
- `docs/examples/dispatch.md`, the paragraph on `ReplayCapacity`. Add
  one sentence naming the bounded window.

#### Tests

New file `dispatch/dispatch_test/replay_window_test.go`. Each test
builds its own `*ledger.Ledger` over a capped `ledger.MemStore` and
passes it in `Options.Ledger`, so the test can read the store through
`Ledger.Snapshot`. The endpoint's own default ledger is unexported and
unreadable.

- `TestReplayCapacityBoundsRecordCount` — build a ledger over a
  `MemStore` with `MaxEntries: 2`. Post eight distinct signed messages
  over one `httptest` server. Assert every reply is a confirmed ack,
  then assert `Snapshot` holds at most two records. FAILS TODAY: the
  snapshot holds eight.
- `TestEvictedKeyIsProcessedAgain` — `MaxEntries: 1`. Post message `x`
  and assert a confirmed ack. Post four other messages to evict `x`.
  Post `x` again and assert a confirmed ack, and that the handler
  counter reads two for `x`. This pins the bounded window as
  deliberate. FAILS TODAY: the second post of `x` answers `ErrReplay`.
- `TestAbortedRequestReleasesItsRecord` — `MaxEntries: 2` and a
  mutex-guarded test clock passed as `MemStoreOptions.Now`. Use a
  handler that blocks until the client cancels its request context.
  Post one message with a context the test cancels, so the record
  stays `StatusClaimed`. Advance the clock past `ReplayLease`. Post
  three more messages. Assert `Snapshot` holds at most two records.
  This is the end-to-end proof of the reported defect. FAILS TODAY:
  the abandoned record is never reclaimed and the count grows.

  Seed the test clock at `time.Now()` and only ever advance it. Two
  clocks are in play and they must not disagree. `Endpoint` builds its
  `taskrun.Options` internally and `dispatch.Options` exposes no `Now`
  field (`dispatch/options.go:25-57`), so `Claim` stamps `LeaseUntil`
  from the wall clock. `MemStoreOptions.Now` is the only clock the test
  controls. A test clock seeded anywhere before `time.Now()` makes
  every wall-clock `LeaseUntil` read as live forever, and nothing is
  ever reclaimed. Do not seed it from `fixedNow` or from a zero
  `time.Time`.

Every test above runs under `go test -race ./dispatch/...`.

#### Verification

- No `dispatch` production code changes, so `make api-update` produces
  no `api/dispatch.txt` diff.
- `policy/layers.json` is unchanged. The row
  `"dispatch": ["agent", "envelope", "events", "ledger", "room", "taskrun"]`
  already allows every import the new tests need.
- `make verify` passes; `dispatch` and the module total hold the 85
  coverage floor.
- `go test -race ./dispatch/...` passes.
- `make mutation PKG=dispatch` holds the floor of 95. The floor entry
  in `scripts/mutation_denylist/dispatch.json` stays at 95. Never lower
  the floor. `resolveReplayCapacity`'s survivor stays a survivor: all
  three tests above pass their own `Options.Ledger`, and
  `ReplayCapacity` is ignored when `Ledger` is set
  (`dispatch/options.go:53-56`). So `resolveReplayCapacity` runs only
  on the nil-`Ledger` path, whose store stays unexported and
  unreadable.
- `python3 scripts/check_prose.py`, `check_labels.py`,
  `check_plan.py`, and `check_deps.py` pass.
- This work lands as its own commit, after the `ledger` commit and the
  `taskrun` doc commit.
