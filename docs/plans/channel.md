# Plan: channel

Status: shipped. Folds phase 37's design into the package's own
permanent plan, the way phase 14's plan absorbed phase 31's design
once phase 31 landed.

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

Outside: any concrete transport (terminal prompt, Slack, email,
webhook, an agent-to-agent call). Those stay caller-built; `channel`
never builds one and commits no future phase to building one.
Outside: any change to `envelope`, `agent`, `tools`, `events`, or the
phase 36 plan. Neither existing hook depends on `channel`; both
already accept a plain function value, and a `channel.Notifier`-backed
closure is one implementation shape a caller may build for either, not
a required one. `channel` adds no signature change to either package.
Outside: persisting a `Question` or an `Answer`. Outside: any UI.

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
