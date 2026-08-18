# Phase 37: escalation and notification channel abstraction

Status: future. Plan-only; it has not yet gone through plan review. It
adds one new package, `channel`, with no dependency on a landed phase.
It ships independently of phase 36.

## Why a new package, not a new hook on an existing one

`agent.AckWait` already blocks `agent.Run` until a caller-supplied
function resolves one step, and wraps `agent.ErrEscalated` to route a
step to a human. Phase 36 (plan-only) adds `Approve`, a second
caller-supplied function with the same blocking shape, to gate one
tool call. Both are one-off function types, each shaped for its own
call site, with no shared vocabulary. A third call site is already
visible: `envelope.IntentQuery` names "ask for information" as a
message intent, but nothing in this module carries a typed answer
back to a query. Three call sites converging on the same shape,
ask-and-wait-for-an-answer, is the signal this repo's Building
blocks rule asks for before a new package earns its place: a real
consumer, not speculative generality.

Two directions were weighed for the shared shape.

- Reuse `events.Bus`: emit a typed event, let a subscriber do the
  channel work. Rejected as the sole mechanism. `Bus.Emit` runs every
  handler and returns `error`, with no typed value flowing back to
  the emitter; the bus is fire-and-forget by design, matching
  `events`' own plan. Escalation, approval, and a query answer all
  need a value back, not a notification. Retrofitting a synchronous
  return onto `Bus.Emit` would change a shipped, reviewed package's
  contract for every existing caller, not just this one.
- A new function-shaped interface, `Notifier`, that a caller
  implements once per real transport and plugs into any call site
  that already accepts a plain closure. Chosen. It gives
  `agent.AckWait`, phase 36's `Scope.Approve`, and a future
  `IntentQuery` answer path one shape to converge on, while changing
  no signature either package already ships or plans.

This plan resolves toward the second direction. It states the
reasoning here, the way phase 10's `a2aclient` plan documents why it
superseded an earlier sketch, instead of asserting the conclusion.

## Goal

Give every part of this SDK that must ask a question, of a human or
of another agent, and wait for a typed answer, one shared shape to
build a closure from: `Question`, `Answer`, and `Notifier`. `channel`
supplies the shape only. It sends no bytes over any real transport.

## Scope

Inside: the `Question` type, the `Answer` type, and the `Notifier`
function type, plus their `Validate` rules. `channel` is a leaf
package: no I/O, no goroutine, no persistence, no concrete transport.

Outside: any concrete transport (terminal prompt, Slack, email,
webhook, an agent-to-agent call). Those stay caller-built examples in
this plan's prose. This phase never builds one and commits no future
phase to building one. Outside: any change to `envelope`, `agent`,
`tools`, `events`, or the phase 36 plan. This phase does not edit
`agent/run.go` or `agent.AckWait`'s signature, and does not edit
`tools`'s `ScopeOptions.Approve` field or the phase 36 plan file.
Neither existing hook depends on `channel`; both already accept a
plain function value, and a `channel.Notifier`-backed closure is one
implementation shape a caller may build for either, not a required
one. Outside: persisting a `Question` or an `Answer`. Outside: any UI.

### Package name: `channel`, not `notify` or `escalation`

The user's proposed name is `channel`, matching the lowercase,
no-underscore style of `heartbeat`, `discovery`, `identity`, and
`tools`. Go's `chan` keyword names a distinct built-in concurrency
primitive, spelled `chan`, four letters, never `channel`; the package
name `channel` does not collide with it lexically or import as it.
The standard library ships no `channel` package, and no package in
this module uses that name today. This plan keeps `channel`.

### `Question` stays decoupled from `envelope`

`channel` could carry a full `envelope.Message` as a question's
payload, letting a `Notifier` forward a signed, verifiable message
end to end. This plan rejects that import. `tools` sets the strong
precedent: it stays a leaf with zero internal imports so any future
package in this module can depend on it without risking a cycle.
`channel` is the same kind of shared, low-level shape; `agent`,
`tools`, and a future query-answer caller would all need to reach it,
so it must stay import-light for the same reason `tools` does.
Importing `envelope` would also force every `Notifier` implementation,
including a plain terminal prompt that never touches the wire, to
build or accept a full signed `Message` just to answer a yes-or-no
question. `Question` and `Answer` instead carry their own minimal,
`channel`-owned fields. A caller that wants to carry a real
`envelope.Message` as a question's content does so by encoding it
(`Message.Encode`) into `Question.Payload`, a string, the same way a
caller already threads opaque content through `tools.InOut.Value`
or `machine.InOut`. `channel`'s row in `policy/layers.json` is `[]`.

### One method, both blocking and fire-and-forget

`agent.AckWait` and phase 36's `Approve` both block until a value
comes back; nothing in this module today needs a notify call with no
answer, because `events.Bus.Emit` already covers fire-and-forget.
This plan gives `Notifier` one method, `Notify`, that always returns
an `Answer` and an error. A caller that wants fire-and-forget ignores
the returned `Answer`, the same way a caller of phase 36's `Approve`
can already discard a value it does not need. Adding a second,
no-answer method would duplicate `events.Bus.Emit`'s already-shipped
job inside a new package, which is the same duplication the direction
comparison above rejects. This plan keeps the surface at one method.

### No `Kind` or `Urgency` classification

Phase 31's `ExecutionClass` and `envelope.Intent` each classify a
value because a real caller in this module already branches on the
classification: `Scope.Allowed` reads `ExecutionClass`, and
`Validate` reads `Intent`. No caller in this module today branches on
a `Question`'s urgency or kind; a terminal-only `Notifier` has one
delivery path regardless of urgency, because it has no second path to
choose between. Adding `Kind` or `Urgency` now would be speculative
generality with no caller, which the Building blocks rule and this
skill's overengineering lens both reject. A future phase adds a
classification field only once a real `Notifier` implementation needs
to route on it, following `ScopeOptions.ApprovalThreshold`'s pattern
of adding a field to an already-shipped shape when a caller appears.

## API

Every entry lands in `api/channel.txt` via `make api-update` once
this phase builds.

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
- `(a Answer) Validate() error` — rejects an empty `QuestionID` and,
  when `Approved` is false and `Payload` is also empty, accepts the
  value; a decline needs no payload. Rejects nothing else; `Approved`
  is a plain bool with no invalid state.
- `type Notifier func(ctx context.Context, q Question) (Answer, error)`
  — a caller-implemented channel: prompt a human, call Slack, call
  another agent, or any transport the caller owns. `channel` ships no
  implementation. `Notify` on a `Notifier` value calls the function
  directly; `Notifier` is a func type, not an interface with a method
  named `Notify`, so a caller assigns any matching closure with no
  wrapper type.
- Sentinel errors: `ErrEmptyID`, `ErrEmptyRecipient`, `ErrEmptyPayload`,
  `ErrEmptyQuestionID` — returned by `Validate`, tested with
  `errors.Is`.

No interface beyond the `Notifier` func type. No constructor: both
`Question` and `Answer` are plain structs a caller builds with a
literal, matching `events.Event`'s shape, not `tools.Registry`'s
constructor-gated shape, because neither type holds unexported state.

## Tests

Test files live in `channel/channel_test/`, following `PHASES.md`'s
flat test layout.

- `question_validate_test.go` — red-green cases for `Question.Validate`:
  empty `ID`, empty `Recipient`, empty `Payload`, and a fully populated
  value that passes. Each empty-field case asserts the exact sentinel
  with `errors.Is`.
- `answer_validate_test.go` — red-green cases for `Answer.Validate`:
  empty `QuestionID` fails with `ErrEmptyQuestionID`. `Approved: true`
  with empty `Payload` passes. `Approved: false` with empty `Payload`
  passes. A fully populated value passes.
- `notifier_test.go` — red-green cases proving `Notifier` is callable
  as a plain closure with no adapter type: a stub `Notifier` that
  echoes `q.ID` into `Answer.QuestionID` and returns `Approved: true`;
  a stub that returns a non-nil error and no `Answer` fields set; a
  stub that ignores `ctx` cancellation, proving `channel` enforces no
  context behavior of its own (that is a `Notifier` implementation's
  concern, not this package's).
- `notifier_integration_test.go` — builds one closure with the shape
  phase 36's `Approve` field and `agent.AckWait` already expect, using
  only `channel.Question`, `channel.Answer`, and a `channel.Notifier`
  value, to prove the composition claim in prose: a `channel.Notifier`
  can back either hook's closure with no change to either package.
  This test lives inside `channel`'s own test package and imports no
  package beyond `context` and standard fixtures, so it proves the
  shape compiles against a locally declared function type matching
  `agent.AckWait`'s and phase 36's `Approve`'s signatures, without
  giving `channel` an import edge to `agent` or `tools`.

## Verification

`make verify` passes, once this phase's code lands. The coverage
floor for `channel` holds at 85 or above. `api/channel.txt` is created
by `make api-update` in this phase's own change, locking `Question`,
`Answer`, `Notifier`, both `Validate` methods, and the four sentinel
errors. `policy/layers.json` gains a `channel` row set to `[]`, added
by this plan ahead of the code, matching the gate's own rule that a
new package needs a row before it has code. `scripts/check_deps.py`
passes with no edge from `channel` to any other internal package, and
no edge from `agent` or `tools` to `channel` (composition happens in
caller code, not inside either package). `scripts/check_plan.py`
passes once `docs/plans/channel.md` exists, written from
`docs/plans/TEMPLATE.md`, folding this phase's design into the
package's own plan the way phase 14's plan absorbed phase 31's design
once phase 31 landed.

`AGENTS.md`'s package layout list gains a `channel/` bullet, matching
the existing bullets' level of detail: package name, one-sentence
purpose, and its import edges (none).
