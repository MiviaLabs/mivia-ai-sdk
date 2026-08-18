# Plan: channel

Status: shipped. Folds phase 37's design and phase 43's NDJSON
transport into the package's own permanent plan, the way phase 14's
plan absorbed phase 31's design once phase 31 landed.

## Goal

Give every part of this SDK that must ask a question, of a human or
of another agent, and wait for a typed answer, one shared shape to
build a closure from: `Question`, `Answer`, and `Notifier`. `channel`
supplies the shape only. It sends no bytes over any real transport.

Three call sites already converge on this shape. `agent.AckWait`
blocks `agent.Run` until a caller-supplied function resolves one step.
Phase 36 shipped `ScopeOptions.Approve`, a second caller-supplied
function with the same blocking shape, to gate one tool call.
`envelope.IntentQuery` names "ask for information" as a
message intent, but nothing in this module carries a typed answer
back to a query. A real consumer at three call sites, not speculative
generality, earns `channel` its place under this SDK's Building
blocks rule.

`channel` supplies the question and answer shape only. A caller's
adapter closure still sources any extra field a specific hook's
return type needs, for example `envelope.Ack.From`, which neither
`Question` nor `Answer` carries.

`events.Bus` was considered and rejected as the sole mechanism.
`Bus.Emit` runs every handler and returns only `error`, with no typed
value flowing back to the emitter; the bus is fire-and-forget by
design. Escalation, approval, and a query answer all need a value
back. `channel` instead ships a new func type, `Notifier`, that a
caller implements once per real transport and plugs into any call
site that already accepts a plain closure. It changes no signature
either `agent` or `tools` already ships or plans.

## Scope

Inside: the `Question` type, the `Answer` type, and the `Notifier`
function type, plus their `Validate` rules. `channel` is a leaf
package: no I/O, no goroutine, no persistence, no concrete transport.

Outside: any concrete transport, except the NDJSON-over-stdio
reference transport phase 43 adds (see the NDJSON transport
subsections below). A Slack, email, or webhook transport stays
caller-built; `channel` never builds one and commits no future phase
to building one. Outside: any change to `envelope`, `agent`, `tools`,
`events`, or the phase 36 plan. Neither existing hook depends on
`channel`; both already accept a plain function value, and a
`channel.Notifier`-backed closure is one implementation shape a caller
may build for either, not a required one. `channel` adds no signature
change to either package. Outside: persisting a `Question` or an
`Answer`. Outside: any UI. Outside: a bidirectional, multiplexed,
many-questions-in-flight protocol; `NewNDJSONNotifier` stays 1:1 per
call, matching `Notifier`'s own signature.

### Package name: `channel`

Matches the lowercase, no-underscore style of `heartbeat`,
`discovery`, `identity`, and `tools`. Go's `chan` keyword names a
distinct built-in concurrency primitive, spelled `chan`, four letters,
never `channel`; the package name `channel` does not collide with it
lexically or on import. No package in this module uses that name
today.

### `Question` stays decoupled from `envelope`

`channel` does not import `envelope`. `tools` sets the precedent: it
stays a leaf with zero internal imports so any future package in this
module can depend on it without risking a cycle. `channel` is the same
kind of shared, low-level shape; `agent`, `tools`, and a future
query-answer caller all need to reach it. Importing `envelope` would
also force every `Notifier` implementation, including a plain terminal
prompt that never touches the wire, to build or accept a full signed
`Message` just to answer a yes-or-no question. `Question` and `Answer`
carry their own minimal, `channel`-owned fields. A caller that wants
to carry a real `envelope.Message` as a question's content encodes it
(`Message.Encode`) into `Question.Payload`, a string, the same way a
caller already threads opaque content through `tools.InOut.Value` or
`machine.InOut`.

### One call shape, both blocking and fire-and-forget

`Notifier` is a plain func type, not an interface, and it has no
method. Calling it always returns an `Answer` and an error, the same
way a single-method interface named `Notify` would behave, without
the wrapper type. A caller that wants fire-and-forget ignores the
returned `Answer`, the same way a caller of phase 36's `Approve` can
already discard a value it does not need. A second, no-answer func
type would duplicate `events.Bus.Emit`'s already-shipped job inside a
new package. The surface stays at one func type.

### No `Kind` or `Urgency` classification

No caller in this module today branches on a `Question`'s urgency or
kind. Adding either field now would be speculative generality with no
caller. A future change adds a classification field only once a real
`Notifier` implementation needs to route on it, following
`ScopeOptions.ApprovalThreshold`'s pattern of adding a field to an
already-shipped shape when a caller appears.

## API

Every entry lands in `api/channel.txt` via `make api-update`.

- `type Question struct { ID string; Recipient string; Payload string }`
  — one thing being asked. `ID` names the question so an `Answer` can
  reference it; the caller sets `ID`, `channel` never generates one.
  `Recipient` names who or what should answer: a human handle, an
  agent identity, or any string the caller's `Notifier` interprets.
  `Payload` carries the question's content as an opaque string; a
  caller that wants a signed `envelope.Message` here encodes it first
  and decodes it inside its own `Notifier`.
- `type Answer struct { QuestionID string; Approved bool; Payload string }`
  — one response. `QuestionID` echoes the `Question.ID` it answers.
  `Approved` gives a yes-or-no reading for the common case, matching
  phase 36's `Approve`'s `(bool, error)` shape and `agent.AckWait`'s
  confirmed-or-not shape. `Payload` carries free-form response
  content, for example a query's actual answer text; a caller that
  needs only yes-or-no leaves it empty.
- `(q Question) Validate() error` — rejects an empty `ID`, an empty
  `Recipient`, and an empty `Payload`, each with its own sentinel
  error, matching `events.Event.Validate`'s per-field rejection shape.
  A whitespace-only string counts as empty: `Validate` trims with
  `strings.TrimSpace` before comparing, matching
  `envelope.Ack.Validate`'s and `envelope.NewAck`'s convention.
- `(a Answer) Validate() error` — rejects an empty `QuestionID`, after
  the same `strings.TrimSpace` trim. Rejects nothing else; a decline
  (`Approved: false`) needs no `Payload`, and `Approved` is a plain
  bool with no invalid state.
- `type Notifier func(ctx context.Context, q Question) (Answer, error)`
  — a caller-implemented channel: prompt a human, call Slack, call
  another agent, or any transport the caller owns. `channel` ships no
  implementation. `Notifier` is a func type with no method, so a
  caller assigns any matching closure with no wrapper type.
- Sentinel errors: `ErrEmptyID`, `ErrEmptyRecipient`, `ErrEmptyPayload`,
  `ErrEmptyQuestionID` — returned by `Validate`, tested with
  `errors.Is`.

No interface beyond the `Notifier` func type. No constructor: both
`Question` and `Answer` are plain structs a caller builds with a
literal, matching `events.Event`'s shape, not `tools.Registry`'s
constructor-gated shape, because neither type holds unexported state.

No JSON tags on `Question` or `Answer`. `channel` types are Go-only,
matching the leaf-package, no-I/O framing; a caller that needs a wire
form wraps the fields in its own transport-specific type.

### NDJSON transport (phase 43, shipped): `NewNDJSONNotifier`

Folded from phase 43's now-retired plan draft; this subsection states
the shape the plan locks. `mivia-agent`'s desktop app already speaks
newline-delimited JSON (NDJSON) over stdio for its own `--json` line
mode and its `internal/hub` process-to-process protocol.
`NewNDJSONNotifier` ships a `channel.Notifier` that speaks that same
convention, so an integration does not hand-write the same encode,
decode, and buffer-sizing logic.

- `func NewNDJSONNotifier(r io.Reader, w io.Writer) Notifier` — builds
  a `Notifier` that writes one JSON-encoded question line to `w` and
  blocks reading one JSON-encoded answer line from `r`. The question
  line carries `"type":"question"` plus `id`, `recipient`, and
  `payload`, snake_case. The answer line carries `"type":"answer"`
  plus `question_id`, `approved`, and `payload`. Both wire structs stay
  internal to `channel/ndjson_notifier.go`; `Question` and `Answer`
  keep zero JSON tags, unchanged. The scanner that reads the answer
  line sizes its buffer `64*1024` initial, `1024*1024` cap, matching
  `mivia-agent`'s own `hub.connection.go` and
  `chat_repl_linemode.go` sizing.
- `ErrAnswerMismatch` — returned when a decoded answer line's
  `question_id` does not match the `Question.ID` the same call sent.
  Checked with `errors.Is`.
- `ErrNotifierBusy` — returned when a call arrives while another call
  on the same `NewNDJSONNotifier` closure already holds its internal
  lock. Checked with `errors.Is`.

**Concurrency contract: one caller at a time.** `io.Reader` and
`io.Writer` give no way to abandon a pending call; two calls that both
write to `w` or both read from `r` at once would corrupt the NDJSON
stream for both. The closure defends itself with an internal
`sync.Mutex` and `TryLock`, not a blocking `Lock`:

- A call that acquires the lock releases it only once every phase its
  I/O touched truly finishes: the write completes or errors, and, if
  the write succeeded, a line arrives, `r` errors, or `r` closes. It
  never releases early on `ctx` cancellation, because the background
  write or read goroutine keeps running past that point.
- A call that fails to acquire the lock returns `ErrNotifierBusy` at
  once, touching neither `r` nor `w`.
- This is a deliberate, permanent-lockout limit, not an oversight: if
  the peer never drains a blocked write, and never answers or closes
  or errors `r` after one call's `ctx` is canceled, the closure stays
  locked forever, and every later call on that closure returns
  `ErrNotifierBusy` indefinitely. The recourse is closing whichever
  side is still stuck, `w` or `r`: that makes the stale `Write` or
  `Scan` return an error, which releases the lock and makes the
  closure usable again.
- A late-arriving line after a `ctx`-canceled call has exactly one
  reader (the leaked goroutine), which finds no one left waiting on
  its result and drops the line; nothing routes a stale answer to a
  call that did not ask for it.

Per-call steps: `q.Validate()` runs first, before any I/O; a failure
releases the lock and returns at once. `writeQuestion` runs the
blocking `encodeQuestionLine` call in a goroutine and selects on
`ctx.Done()` against that goroutine's result, mirroring `readAnswer`'s
own pattern:

- If the write finishes first and fails, the call is terminal: the
  lock releases immediately, wrapped `channel: ndjson: %w`.
- If the write finishes first and succeeds, the lock is not released
  here; the caller hands the still-held lock to `readAnswer`.
- If `ctx.Done()` fires first, `writeQuestion` returns `ctx.Err()` at
  once, but a separate goroutine keeps waiting for the abandoned write
  to resolve. `select` picks pseudo-randomly when both cases are ready
  at once, so the write may in fact have already delivered `q` to the
  peer even though this branch ran. That goroutine,
  `continueAfterAbandonedWrite`, branches on the outcome: a write
  failure is terminal, so it releases the lock; a write success is
  not terminal, so, since the original call already returned and has
  no live caller left to invoke `readAnswer`, this goroutine runs the
  same `scanAnswerLine` step itself and releases the lock once that
  resolves. This keeps a second caller locked out until both phases of
  the abandoned call finish, never letting it start its own `Write` on
  `w` while a delivered-but-unanswered question is still outstanding.

`readAnswer` blocks reading one line, respecting `ctx` through a
select against a channel a background goroutine closes when its `Scan`
resolves; the lock releases from inside that goroutine, right before
it signals its result, so a caller never observes the lock as still
held once it receives a result. A decode error, a wrong `type`, or a
`question_id` mismatch returns a non-nil error and no `Answer`. On
success, `Answer.Validate()` runs once more before the value crosses
back to the caller, matching this package's own
Encode/Decode-validates-before-crossing-boundary convention; this
check is defense-in-depth, since `Question.Validate` already
guarantees `wantID` is non-empty upstream.

`channel` gains no new import for this transport: `encoding/json`,
`bufio`, `io`, `context`, `fmt`, and `errors` are all standard
library. `policy/layers.json`'s `"channel": []` row stays unchanged.

## Tests

Test files live in `channel/channel_test/`, an external test package.

- `question_validate_test.go` — red-green cases for `Question.Validate`:
  empty `ID`, empty `Recipient`, empty `Payload`, a whitespace-only
  `ID` (fails with `ErrEmptyID`, proving the trim rule), and a fully
  populated value that passes. Each empty-field case asserts the exact
  sentinel with `errors.Is`.
- `answer_validate_test.go` — red-green cases for `Answer.Validate`:
  empty `QuestionID` fails with `ErrEmptyQuestionID`. A whitespace-only
  `QuestionID` fails with `ErrEmptyQuestionID`, proving the trim rule.
  `Approved: true` with empty `Payload` passes. `Approved: false` with
  empty `Payload` passes. A fully populated value passes.
- `notifier_test.go` — red-green cases proving `Notifier` is callable
  as a plain closure with no adapter type: a stub `Notifier` that
  echoes `q.ID` into `Answer.QuestionID` and returns `Approved: true`;
  a stub that returns a non-nil error and no `Answer` fields set; a
  stub that ignores `ctx` cancellation, proving `channel` enforces no
  context behavior of its own (that is a `Notifier` implementation's
  concern, not this package's).
- `notifier_integration_test.go` — imports `agent` directly (test
  files are exempt from `policy/layers.json`; see the precedent set by
  `agent/agent_test/exchange_integration_test.go`,
  `events/events_test/cross_subsystem_integration_test.go`,
  `identity/identity_test/sign_integration_test.go`, and
  `flow/flow_test/chain_integration_test.go`, all of which cross
  layers from their own `_test` subpackage on purpose). It declares a
  `channel.Notifier` closure, wraps it in a second closure that
  sources the extra `envelope.Ack.From` field a fixed identity string
  supplies, and assigns the result to a variable of the real
  `agent.AckWait` type, `func(ctx context.Context, msg envelope.Message)
  (envelope.Ack, error)`. Assigning compiles only if a
  `channel.Notifier`-backed closure satisfies `agent.AckWait`'s exact
  signature, proving the composition claim against the real type, not
  a hand-copied stand-in. This test covers `agent.AckWait` only.
  Phase 36 has since shipped `tools.ScopeOptions.Approve`; no test in
  this package yet proves a `channel.Notifier`-backed closure against
  that field's signature. A future change closes that gap the same
  way this test closes it for `agent.AckWait`.

### NDJSON transport (phase 43, shipped) tests

- `ndjson_notifier_test.go` — red-green cases over `io.Pipe`-backed
  reader/writer pairs: a round trip with a matching answer; an
  `ErrAnswerMismatch` on a wrong `question_id`; a malformed line; a
  wrong `type`; a peer that closes without answering; a `ctx`
  cancellation that returns promptly with `ctx.Err()`; an invalid
  `Question` that releases the lock, proven by a following call
  succeeding; a line at the 1 MB scanner cap that decodes, and one past
  the cap that fails; a `w` write failure that releases the lock; a
  second call while a first is still blocked, returning
  `ErrNotifierBusy`; a `ctx`-canceled first call followed by a second
  call while the first's background goroutine is still pending
  (`ErrNotifierBusy`), then a late answer that the leaked goroutine
  drops, then a third call that succeeds; the same busy sequence
  followed by closing `r` instead, proving the close-to-recover path;
  and a `-race`-only concurrent-call case where two goroutines call one
  closure over one shared `io.Pipe` pair, asserting one succeeds and
  one returns `ErrNotifierBusy` with no data race.
- `ndjson_notifier_integration_test.go` — imports `agent` directly,
  the same cross-layer precedent `notifier_integration_test.go` uses:
  builds an `NewNDJSONNotifier` closure over an `io.Pipe` pair, wraps
  it in a closure sourcing `envelope.Ack.From`, and assigns the result
  to a real `agent.AckWait` variable, proving the NDJSON transport
  composes with the same real call site.
- `ndjson_notifier_bench_test.go` — benchmarks one round trip over an
  `io.Pipe` pair with a fixture goroutine answering immediately,
  reporting ns/op and allocs/op, plus a companion test asserting
  `testing.AllocsPerRun` stays within a fixed budget calibrated to the
  measured baseline.

## Verification

`make verify` passes. The coverage floor for `channel` holds at 85 or
above. `api/channel.txt` is created
by `make api-update` in this package's own change, locking `Question`,
`Answer`, `Notifier`, both `Validate` methods, and the four sentinel
errors. `policy/layers.json`'s `channel` row stays `[]`.
`scripts/check_deps.py` passes with no edge from `channel` to any
other internal package, and no edge from `agent` or `tools` to
`channel` (composition happens in caller code, not inside either
package). `scripts/check_plan.py` passes against this file.

`AGENTS.md`'s package layout list carries a `channel/` bullet,
matching the existing bullets' level of detail: package name,
one-sentence purpose, and its import edges (none).

### NDJSON transport (phase 43, shipped) verification

`make verify` passes with `NewNDJSONNotifier`, `ErrAnswerMismatch`,
and `ErrNotifierBusy` added. `go test -race ./channel/...` passes,
covering the concurrent-call case. The coverage floor of 85 holds for
`channel` and for the total, with `writeQuestion` and `readAnswer`
counted in. `api/channel.txt` gains the three new symbols through
`make api-update`, committed in the same change as the code.
`policy/layers.json`'s `"channel": []` row stays unchanged.
`docs/packages/channel.md` gains an NDJSON transport section
describing the wire shape, the one-caller-at-a-time contract
(including the permanent-lockout limit and its close-`r`-or-`w`
recourse), and the `mivia-agent` convention it mirrors. `docs/examples/
channel-ndjson-stdio.md` is added, compiled and run against the real
module, with a matching one-line entry in `docs/README.md`'s Examples
list. No conformance vector change: `channel` still carries no signed
or hash-chained wire form.
