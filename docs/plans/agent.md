# Plan: agent

Status: shipped. `Agent` depends on identity, discovery, and flow,
all shipped. The envelope-to-events translator adds envelope and
events. The execution loop is the in-process runner (see below); the
`tools` package adds the tool registry separately. `Run` also carries
an optional step-liveness heartbeat and an optional room name, so a
built step message can pass `room.Room.Accepts`. `Run` also carries
an optional context budget, `*contextbudget.Limits`, that gates each
gated step's `wait` call against a cumulative byte and event cap; see
`docs/plans/contextbudget.md`.

## Goal

Define one agent declaratively. An Agent binds an identity, a
capability card, and a step plan into one value. The definition is
data. It states who the agent is, what it can do, and what it runs.
It does not run yet.

## Scope

Inside: the Agent type, New, Name, and Capabilities. New wires an
identity, a discovery card, and a flow plan into one Agent.

Outside: the execution loop, the tool registry, the memory store, and
the transport binding. Those belong to `Run` and later additions. This
package owns no goroutine, no context.Context walk, and no network
call.

The package imports identity, discovery, and flow only, for the
`Agent` definition itself. Stdlib only: errors and fmt, for the sentinel
errors and the wrapped card error. The later additions below widen the
policy row.

### The envelope-to-events translator: scope

This adds a thin translator inside the same `agent` package. It
turns a delivered `envelope.Message`, an `envelope.Ack`, or an
`envelope.VerifyThread` outcome into one `events.Event`. It emits that
event onto a caller-owned `events.Bus`. The translator is
composition-layer code. It is the one place allowed to import both
`envelope` and `events`, because neither of those two packages may
import the other.

Inside: three free functions (`EmitMessageDelivered`,
`EmitMessageAcked`, `EmitThreadVerified`), three `events.Name`
constants, and one new sentinel, `ErrNoBus`. Outside: room admission,
the transport step, and any change to the envelope wire contract or
the events bus contract. The translator does not sign, encode, or
transport a message. It only verifies an already-received value and
emits.

The package imports gain `envelope` and `events` in this phase. The
policy row becomes `"agent": ["identity", "discovery", "flow",
"envelope", "events"]`. `envelope`'s row and `events`'s row stay
empty. Neither package gains an import of the other or of `agent`.

### The run entry point: scope

`(*Agent).Run` drives the agent's bound `*flow.Definition` through
`flow.Run`, in-process. It signs each gated step as an
`envelope.Message`, waits on a caller-supplied ack resolver, and
emits the translator events above at the right points. It routes an
escalated step back to the caller through a sentinel error.

An earlier sketch proposed sending each step "through the a2a adapter
or the in-process runner." `Run` descopes to the in-process path only
and never imports or assumes a network transport; the `a2aclient`
package owns the remote-send path separately.

Inside: `Run`, the `AckWait` function type, and three new sentinels
(`ErrEscalated`, `ErrNoWait`, `ErrNoThread`). `Run` reuses the
translator functions (`EmitMessageDelivered`, `EmitMessageAcked`,
`EmitThreadVerified`) and the translator's sentinel `ErrNoBus`; it adds no
new emit function. Outside: the tool registry and the memory store
(phases 14 and 15). Outside: any network transport binding; that
belongs to the future `a2a` package.

The package imports gain `machine` in this phase, because `Run` takes
a `*machine.Definition` parameter to hand to `flow.Run`. The policy row
becomes `"agent": ["identity", "discovery", "flow", "envelope",
"events", "machine"]`. `machine`'s own row stays `["events"]`; it gains
no new import.

### Step-liveness heartbeat: scope

This closes the one real liveness gap in `Run`: the caller-supplied
`wait` call can block forever, with no stall signal. `Run` gains one
trailing, optional parameter, `hb *heartbeat.Monitor`. `Run` beats it
once per gated step, right before `wait`, and forgets it once, on
every return path. `Run` never reads `Dead` itself; an external
caller, holding the same `Monitor`, polls `Dead` and reacts on its
own, for example by canceling `ctx`.

Inside: the new `hb` parameter, one beat call, one deferred forget
call. Outside: a `Dead` check inside `Run`, a retry or cancellation
policy, and a `MissedEvent` emission from inside `agent`.

Disclosed scope limit: coverage reaches only a gated, singleton step,
the kind `confirmStep` gates behind `wait`. `flow.Run`'s panel wave
runs every panel member concurrently with no `Confirm` or `wait` call
at all, so a panel of two or more members never reaches a beat call
and `hb.Dead` can never report a stalled panel member. This is a
known, disclosed limit, not a bug.

The package imports `heartbeat`. The policy row is
`"agent": ["identity", "discovery", "flow", "envelope",
"events", "machine", "heartbeat"]`. `heartbeat`'s own row stays
`["events"]`; it gains no new import and does not import `agent`.

`Run` adds no `*machine.Definition` field to `Agent`. `Agent` stays
the declarative binding `New` builds: an identity, a
card, and a plan, no per-run state. The status model and the starting
record are `Run` parameters, matching `flow.Run`'s own shape, so one
`Agent` value can run against more than one machine model or resume
with more than one starting record, without `New`'s signature or
`Agent`'s fields changing.

### Room stamping: scope

This closes a room-admission gap: `Run` never sets `Message.Room`
before it signs a step message, so a `room.Room.Accepts` call can
never admit a `Run`-built message. `Run` gains one trailing, optional
parameter, `room string`. `confirmStep` stamps it onto each built
message, as `msg.Room = room`, before `a.id.Sign(msg)` runs, because
`envelope.Sign` covers `Room` in its signed payload and nothing after
`Sign` can add it without invalidating the signature.

Inside: the new `room` parameter, one field assignment inside
`confirmStep`, guarded so an empty `room` reproduces today's exact
behavior (`Message.Room` stays the zero value). Outside: a `Room`
field on `Agent`, a generic pre-sign decorator hook, and any change to
`room.Room.Accepts` or to `envelope.Sign`. A `Room` field on `Agent`
was rejected because room membership is a per-run concern, not part
of the agent's declarative binding; a decorator hook was rejected as
speculative generality with no second caller.

The package's imports do not change here. `agent` already
imports `envelope`, which already declares `Message.Room`; the policy
row stays `["identity", "discovery", "flow", "envelope", "events",
"machine", "heartbeat"]`.

## API

The surface below is the lock target. It lands in `api/agent.txt` via
make api-update.

- `type Agent struct` holds an `*identity.Identity`, a
  `discovery.Card`, and a `*flow.Definition`. All three fields stay
  unexported. A caller reaches them through `Name`, `Capabilities`,
  and `Run`, added below.
- `func New(id *identity.Identity, card discovery.Card, plan *flow.Definition) (*Agent, error)`
  builds an Agent from the three parts. It checks id for nil, calls
  card.Validate(), then checks plan for nil, in that order. It returns
  the first error hit. On success it returns a populated `*Agent` and
  a nil error.
- `func (a *Agent) Name() string` returns the card's Name field,
  unchanged. It applies no trim; Card.Validate already rejects a name
  that is blank after TrimSpace, and Name returns the stored value
  as-is, matching Card's own no-normalization rule.
- `func (a *Agent) Capabilities() []string` returns the card's
  Capabilities slice. It returns the same backing array Parse or the
  caller set, with no defensive copy. This matches
  discovery.Card, which carries the same caller-owned mutability.
- `var ErrNoIdentity` is the sentinel New returns when id is nil.
- `var ErrNoPlan` is the sentinel New returns when plan is nil.

### Parameter shapes, not the phase sketch's

The phase sketch proposed `New(id identity.Identity, card
discovery.Card, plan flow.Definition) (*Agent, error)`. This plan
changes two of the three parameter types after reading the real
identity and flow surfaces.

- `id` is `*identity.Identity`, not a value. identity.New and
  identity.Load both return `*Identity`. Every identity method
  (Sign, Signer, Validate) takes a pointer receiver, because Identity
  holds private key material as session state, not copyable data. A
  value parameter would force an extra copy of that key material for
  no benefit; agent.New accepts the same pointer the caller already
  holds.
- `plan` is `*flow.Definition`, not a value. flow.New returns
  `*Definition`, and flow.Run also takes `*Definition`. Definition
  exposes no field; every flow API already passes it by pointer.
  agent.New accepts the same pointer flow.New returned, with no
  dereference and no copy.
- `card` stays `discovery.Card`, a value, matching the phase sketch.
  Card uses value receivers throughout discovery: a small value type,
  two strings and a slice header, following envelope.Message's
  convention for wire-decoded data.

### Card validation: New calls card.Validate()

New calls `card.Validate()` itself. It does not require an
already-validated Card from the caller. Two reasons support this.

Card exposes no field-lock: a caller can build a Card by struct
literal, bypassing Parse and Validate, as the discovery plan states.
New cannot assume the card in front of it ever saw Validate. Calling
Validate is the only way to enforce "no name on the card" without
agent re-implementing discovery's TrimSpace rule.

Reusing card.Validate() also catches an empty capability list and a
duplicate capability, both invariants discovery already owns. New
does not duplicate that logic; it defers to discovery's single source
of truth and wraps the returned error: `fmt.Errorf("agent: invalid
card: %w", err)`. discovery.Validate exports no sentinel today, so
this wrap carries no errors.Is target beyond the wrapped error text.
A caller checks the error for nil; it does not check a discovery
sentinel, because none exists yet.

### Plan validation: New trusts a non-nil Definition

New checks `plan` for nil and returns `ErrNoPlan` when it is. New does
not re-run cycle detection.

flow.Definition exposes no field, so a caller cannot hand-build a
populated Definition by struct literal. Two shapes reach agent.New. A
Definition built through flow.New already passed flow.New's cycle
check. A zero-value Definition, such as `flow.Definition{}` or `var d
flow.Definition`, holds zero steps; it never touched flow.New and
never touched the cycle check.

The zero-value shape is not a gap. flow.Run special-cases a Definition
with zero steps: `len(d.steps) == 0` returns the current status
immediately, with no error, before any step runs. See
flow/runner.go. A `*flow.Definition` that skipped flow.New carries no
step and no cycle, so it has nothing for a cycle check to reject.
Either path, agent.New's plan argument is safe by the time it reaches
flow.Run.

This satisfies the phase requirement "rejects a step plan with a
cycle" by delegation, not duplication. agent.New's own contribution is
the nil check: a caller that forgets to build a plan, or passes a nil
pointer on purpose, gets `ErrNoPlan`, not a panic inside flow.Run
later.

### Receiver semantics: pointer, not value

Agent uses pointer receivers throughout. New returns `*Agent`. This
matches identity.Identity and events.Bus, which hold session state
behind a pointer, not envelope.Message, room.Room, or discovery.Card,
which are copyable data.

Two reasons support the pointer choice. First, Agent holds an
`*identity.Identity` field directly: a value receiver on Agent would
still share the same underlying key material through that pointer, so
copying Agent by value buys no isolation and only hides the shared
state behind a false copy. Second, `Run` adds mutable execution
state, such as a tool registry and a memory store, to this same
struct. Starting with a pointer receiver now avoids a receiver-style
break when that state lands.

The expected lock content:

```text
package agent
  func (a *Agent) Capabilities() ([]string)
  func (a *Agent) Name() (string)
  func New(id *identity.Identity, card discovery.Card, plan *flow.Definition) (*Agent, error)
  type Agent struct {
}
  var ErrNoIdentity
  var ErrNoPlan
```

### The envelope-to-events translator: the API lock target

Three exported functions, three exported `events.Name` constants, and
one exported sentinel land with the translator. They join the
surface above. Nothing above changes.

- `const MessageDeliveredEvent events.Name = "agent.message_delivered"`
  names the event `EmitMessageDelivered` emits.
- `const MessageAckedEvent events.Name = "agent.message_acked"` names
  the event `EmitMessageAcked` emits.
- `const ThreadVerifiedEvent events.Name = "agent.thread_verified"`
  names the event `EmitThreadVerified` emits.
- `var ErrNoBus` is the sentinel every `EmitX` function returns when
  its `bus` argument is nil. It replaces a nil-pointer panic inside
  `events.Bus.Emit`. Panics inside a package violate AGENTS.md.
- `func EmitMessageDelivered(ctx context.Context, bus *events.Bus, m envelope.Message) error`
  checks `bus` for nil first and returns `ErrNoBus`. It then calls
  `m.VerifySignature()`. A verification failure returns that error
  and emits nothing. On success it emits one `Event` named
  `MessageDeliveredEvent`, with `Data` set to
  `fmt.Sprintf("message %s delivered", m.ID)`.
- `func EmitMessageAcked(ctx context.Context, bus *events.Bus, a envelope.Ack) error`
  checks `bus` for nil first and returns `ErrNoBus`. It then calls
  `a.Validate()`. A validation failure returns that error and emits
  nothing. On success it emits one `Event` named `MessageAckedEvent`,
  with `Data` set to
  `fmt.Sprintf("ack for message %s status %s", a.MessageID, a.Status)`.
- `func EmitThreadVerified(ctx context.Context, bus *events.Bus, msgs []envelope.Message) error`
  checks `bus` for nil first and returns `ErrNoBus`. It then calls
  `envelope.VerifyThread(msgs)`. A verification failure returns that
  error and emits nothing. On success it emits one `Event` named
  `ThreadVerifiedEvent`, with `Data` set to
  `fmt.Sprintf("thread of %d messages verified", len(msgs))`.

Each function returns the raw error from `bus.Emit`, unwrapped. A
missing subscriber is not this package's concern. The caller owns the
bus and decides whether a missing subscriber is an error worth acting
on.

#### Design decisions

1. Three `Name` constants, not one. `machine.MoveEvent` and
   `flow.StepCompletedEvent` each name one package's one emitted kind,
   and no existing package declares more than one `Name` constant.
   This phase extends that pattern from one kind per package to three
   kinds in one package. A delivered message, an ack, and a verified
   thread are three distinct kinds. A subscriber tells them apart
   through `Subscribe`, not by parsing `Data`. One shared name would
   force every subscriber to sniff the payload string.
2. Three functions, not one overloaded function. Each source kind
   pairs with one envelope call (`VerifySignature`, `Validate`,
   `VerifyThread`) and one `Name` constant. A single function taking
   an `any` argument would need a type switch for three fixed cases.
   No second caller asks for a fourth case. AGENTS.md rejects that
   abstraction as speculative generality.
3. Free functions, not `*Agent` methods. None of the three functions
   read `id`, `card`, or `plan`, the only fields `Agent` holds. Each
   needs only the `bus` and the envelope value the caller passes in.
   A method receiver with no use in the method body is a receiver in
   name only. A free function states that plainly.
4. The race test targets the translator's call path, not the bus
   directly. `events.Bus` already proves its own concurrency safety in
   its own test suite. The translator's race test proves it adds
   no shared mutable state of its own. Many goroutines call
   `EmitMessageDelivered`, `EmitMessageAcked`, and `EmitThreadVerified`
   against one shared bus. An atomic counter proves each call still
   delivers exactly once.

The expected `api/agent.txt` lock, translator lines included:

```text
package agent
  const MessageAckedEvent = "agent.message_acked"
  const MessageDeliveredEvent = "agent.message_delivered"
  const ThreadVerifiedEvent = "agent.thread_verified"
  func (a *Agent) Capabilities() ([]string)
  func (a *Agent) Name() (string)
  func EmitMessageAcked(ctx context.Context, bus *events.Bus, a envelope.Ack) (error)
  func EmitMessageDelivered(ctx context.Context, bus *events.Bus, m envelope.Message) (error)
  func EmitThreadVerified(ctx context.Context, bus *events.Bus, msgs []envelope.Message) (error)
  func New(id *identity.Identity, card discovery.Card, plan *flow.Definition) (*Agent, error)
  type Agent struct {
}
  var ErrNoBus
  var ErrNoIdentity
  var ErrNoPlan
```

### The run entry point: the API lock target

- `type AckWait func(ctx context.Context, msg envelope.Message) (envelope.Ack, error)`
  is the caller-supplied ack resolver. `Run` calls it once per step
  `flow.Run` gates behind `Confirm`, with the signed step message. It
  returns the receiver's real `envelope.Ack`, or an error. An
  implementation wraps `ErrEscalated` with `%w` to route the step to
  a human instead of resolving an ack.
- `func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus) (machine.Status, machine.InOut, error)`
  drives `a.plan` through `flow.Run`. It checks `wait` for nil, then
  `bus` for nil, then `threadID` for empty, in that order, before it
  touches `m` or `a.plan`; `flow.Run` itself rejects a nil `m` or a
  nil `d`. For each gated step, `Run` builds an `envelope.Message`
  from the step's `ID`, `threadID`, and `Payload`, with `Version:
  envelope.Version`, `Intent: envelope.IntentRequest`, and `Epistemic:
  envelope.EpistemicAssumed`, signs it with `a`'s identity, calls
  `EmitMessageDelivered`, then calls `wait`. If `wait` returns a
  non-nil error, `Run`'s `flow.Confirm` closure returns that error
  unchanged and skips `EmitMessageAcked`; a zero-value `Ack` would
  otherwise fail `Ack.Validate()` with an unrelated error and break
  `errors.Is` against a wrapped `ErrEscalated`. Only when `wait`
  returns a nil error does `Run` call `EmitMessageAcked` and require
  `Ack.Status == envelope.AckConfirmed` before it lets the step count
  as done. On a successful run with one or more gated steps, `Run`
  calls `EmitThreadVerified` once, over every step message it built,
  in order, after `flow.Run` returns with a nil error. `IntentRequest`
  and `EpistemicAssumed` let each built message pass `Validate()` on
  its own, which `EmitThreadVerified`'s call to `envelope.VerifyThread`
  requires.
- `var ErrEscalated error` is the sentinel an `AckWait` wraps to
  signal a step needs a human. Test with `errors.Is` against the
  error `Run` returns; `flow.Run`'s own wrap preserves the chain.
- `var ErrNoWait error` is the sentinel `Run` returns when `wait` is
  nil.
- `var ErrNoThread error` is the sentinel `Run` returns when
  `threadID` is empty.

`Run` adds no new field to `Agent` and no new emit function. It reuses
`EmitMessageDelivered`, `EmitMessageAcked`, `EmitThreadVerified`, and
`ErrNoBus`, all already exported by the translator above.

The expected `api/agent.txt` lock, run-entry-point lines added to the
translator block above:

```text
  func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus) (machine.Status, machine.InOut, error)
  type AckWait func(ctx context.Context, msg envelope.Message) (envelope.Ack, error)
  var ErrEscalated
  var ErrNoThread
  var ErrNoWait
```

### The heartbeat parameter: the API lock target

`Run`'s signature gains one trailing parameter:

`func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor) (machine.Status, machine.InOut, error)`

`hb == nil` skips every heartbeat call; `Run` behaves exactly as the
run-entry-point section above describes. `hb != nil` adds, per gated step,
one `hb.Beat(a.id.Signer()+":"+threadID, time.Now())` call right
before `wait`, using one id for the whole `Run` call, and one deferred
`hb.Forget` call on that same id that runs once, on every return path.
No new sentinel, constant, or type. `Run`'s existing sentinel checks
stay unchanged; `hb` gets no nil-check sentinel of its own, because a
nil `Monitor` is a valid, supported "no telemetry" choice, not a
caller error.

The expected `api/agent.txt` diff, against the run-entry-point block
above:

```text
- func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus) (machine.Status, machine.InOut, error)
+ func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor) (machine.Status, machine.InOut, error)
```

This is a breaking change to every existing call site of `Run`. Every
call site gains a trailing `nil` argument for `hb`.

### The room parameter: the API lock target

`Run`'s signature gains one trailing parameter:

`func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string) (machine.Status, machine.InOut, error)`

`room == ""` skips the assignment; `Run` behaves exactly as the
heartbeat-parameter section above describes. `room != ""` sets
`msg.Room = room` inside `confirmStep`, on every gated step's built
message, before `a.id.Sign(msg)` runs. No new sentinel, constant, or
type. `Run`'s existing sentinel checks stay unchanged; `room` gets no
nil-or-empty check of its own, because an empty room is a valid,
supported "no room" choice, not a caller error.

The expected `api/agent.txt` diff, against the heartbeat-parameter
block above:

```text
- func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor) (machine.Status, machine.InOut, error)
+ func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string) (machine.Status, machine.InOut, error)
```

This is a breaking change to every existing call site of `Run`. Every
call site gains a trailing `""` argument in this change, since no
existing test supplies a room name yet.

### The budget parameter: the API lock target

`Run`'s signature gains one trailing parameter, and the sentinel var
block gains one new sentinel, `ErrOverBudget`:

`func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string, budget *contextbudget.Limits) (machine.Status, machine.InOut, error)`

`budget == nil` skips every budget check; `Run` behaves exactly as
the room-parameter section above describes. `budget != nil` runs
`budget.Validate()` once, at the same point `Run` checks `wait`,
`bus`, and `threadID`; an invalid budget returns
`machine.Status("")`, `in` unchanged, and the wrapped `Validate`
error. A non-nil, valid `budget` makes `confirmStep` keep a running
byte total, `runningBytes`, across the call: after each step's
message is signed into `built`, `confirmStep` adds that message's
`len(payload)` to `runningBytes`. Right before the `wait` call for the
step about to run, and before `hb.Beat`, `confirmStep` calls
`budget.Fits` with `runningBytes` plus the about-to-run step's own
payload byte length, and the 1-indexed count of steps built so far. A
`Fits` failure returns `ErrOverBudget`, wrapping the step ID, without
calling `hb.Beat`, `wait`, or `EmitMessageAcked` for that step. A
panel step reaches no `confirmStep` `wait` call at all, so a panel
member's payload never adds to `runningBytes` and never trips
`budget`; see `docs/plans/contextbudget.md`'s disclosed scope limit.

The expected `api/agent.txt` diff, against the room-parameter block
above:

```text
- func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string) (machine.Status, machine.InOut, error)
+ func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor, room string, budget *contextbudget.Limits) (machine.Status, machine.InOut, error)
```

```text
  var ErrEscalated
  var ErrNoBus
  var ErrNoIdentity
  var ErrNoPlan
  var ErrNoThread
  var ErrNoWait
+ var ErrOverBudget
```

This is a breaking change to every existing call site of `Run`. Every
call site gains a trailing `nil` argument for `budget` in this
change.

### Plan and signer accessors: the API lock target

`func (a *Agent) Plan() *flow.Definition` returns the bound plan
unchanged, the same pointer `New` stored. `func (a *Agent)
Signer() string` returns the identity's signer hex. Both exist for
composition layers that validate a plan before a run. Phase 49's
`agentrun` matrix check is the first caller. The methods follow
`Name` and `Capabilities`: pointer receivers, and no nil-receiver
guarantee beyond what `New` already ensures.

## Tests

Test files live in `agent/agent_test/`:

- `definition_test.go` — the red-green cases for New, Name, and
  Capabilities. Assertions come first. The builder confirms they fail
  on the empty package, then implements the code to green. Table cases
  for New cover:
  - a nil identity, a valid card, a valid plan: expect errors.Is
    against ErrNoIdentity.
  - a card with a blank name: expect a non-nil wrapped error.
  - a card with an empty capability list: expect a non-nil wrapped
    error.
  - a card with a duplicate capability, differing only in case (for
    example `["read", "Read"]`): expect a non-nil wrapped error. This
    proves card.Validate's duplicate-capability rule reaches New's
    caller.
  - a card with a blank, whitespace-only capability entry: expect a
    non-nil wrapped error. This proves card.Validate's
    blank-after-trim rule reaches New's caller.
  - a nil plan, a valid identity, a valid card: expect errors.Is
    against ErrNoPlan.
  - a zero-value plan, `&flow.Definition{}`, never built through
    flow.New, paired with a valid identity and card: expect a
    populated Agent and a nil error. The plan is non-nil, so New
    accepts it; it carries no step, so it needs no cycle check.
  - a nil identity together with a nil plan, valid card: expect
    errors.Is against ErrNoIdentity, not ErrNoPlan. This proves New
    checks id before plan.
  - a card with a blank name together with a nil plan, valid
    identity: expect the wrapped card error, not ErrNoPlan. This
    proves New checks the card before the plan.
  - a fully valid triple: expect a populated Agent and a nil error.

  Name and Capabilities cases cover a valid card. The Capabilities
  case confirms aliasing, not mere equality: it takes the address of
  the first element in the returned slice and the address of the
  first element in the source card's Capabilities slice, and asserts
  the two addresses match. As a second proof, it mutates the returned
  slice's first element and asserts the source card's Capabilities
  slice changed too.
- `definition_integration_test.go` — build a real Identity with
  identity.New, a real Card by struct literal, and a real Definition
  with flow.New over a two-step, no-panel plan. Prove agent.New
  accepts the triple and Name and Capabilities resolve to the card's
  values. Feed the same Definition-building call a step pair that
  flow.New itself rejects for a cycle, confirm flow.New returns the
  error before agent.New ever runs, then feed agent.New a nil plan
  directly and confirm ErrNoPlan.
- `definition_bench_test.go` — benchmark New on a two-step plan with
  no panel. Target under one millisecond per call. AllocsPerRun states
  the allocation budget; the builder records the measured baseline in
  this file.

### Translator tests

Test files land in `agent/agent_test/`, alongside the definition files:

- `translator_test.go` — the red-green cases. Start with the
  assertions; confirm they fail against the empty package, then
  implement to green. Table cases:
  - A valid Message, a subscribed bus: EmitMessageDelivered returns
    nil and the handler receives MessageDeliveredEvent.
  - A Message with a bad signature: EmitMessageDelivered returns the
    VerifySignature error and the handler never runs.
  - A valid Ack, a subscribed bus: EmitMessageAcked returns nil and
    the handler receives MessageAckedEvent.
  - An Ack with a blank MessageID: EmitMessageAcked returns the
    Validate error and the handler never runs.
  - A two-message thread that verifies: EmitThreadVerified returns
    nil and the handler receives ThreadVerifiedEvent.
  - A thread with a broken hash chain: EmitThreadVerified returns the
    VerifyThread error and the handler never runs.
  - A nil bus argument to each of the three functions, paired with a
    valid envelope value: expect errors.Is against ErrNoBus.
  - A nil bus argument to each of the three functions, paired with an
    invalid envelope value: expect errors.Is against ErrNoBus, not the
    verify error. Reuse badSignatureMessage(t) for
    EmitMessageDelivered, blankMessageIDAck() for EmitMessageAcked,
    and brokenThread() for EmitThreadVerified. These three fixtures
    already exist in translator_test.go. This proves the nil-bus
    check runs before the verify call, for each function on its own.
  - No subscriber registered for the event name: expect the
    events.Bus.Emit "no subscriber" error, unwrapped.
- `translator_integration_test.go` — build a real events.Bus with
  events.New. Sign a real Message with a real identity.Identity. Call
  EmitMessageDelivered; prove the event arrives exactly once. Build a
  real Ack with envelope.NewAck; call EmitMessageAcked; prove it
  arrives once. Build a real two-message thread; call
  EmitThreadVerified; prove it arrives once. Run every case under
  `go test -race`. Add a fourth case: many goroutines call all three
  EmitX functions against one shared bus. An atomic counter proves
  each call still delivers exactly once, with no data race.

### Gap closure: nil-bus check-order coverage

A review found a gap. TestEmitNilBusReturnsErrNoBus paired bus == nil
only with valid envelope fixtures. A valid envelope passes verify
regardless of guard order, so the old test could not tell a
nil-bus-first implementation from a verify-first implementation. A
reviewer confirmed this by swapping the two guards in
EmitMessageDelivered, in EmitMessageAcked, and in EmitThreadVerified
in a scratch copy; the full test suite still reported ok.

The fix adds three cases to TestEmitNilBusReturnsErrNoBus's existing
cases table. TestEmitNilBusReturnsErrNoBus already builds its cases as
a slice of `struct{ name string; run func() error }` and loops with
t.Run. The three new cases fit that shape directly: no new test
function, no new fixture helper. Each new case pairs bus == nil with
one of the three existing invalid fixtures (badSignatureMessage,
blankMessageIDAck, brokenThread) already used elsewhere in
translator_test.go, and asserts errors.Is(err, agent.ErrNoBus). An
invalid envelope paired with a nil bus can only return ErrNoBus if the
nil-bus check runs first; a verify-first implementation would return
the verify error instead, and the new case would fail.

The three new cases use distinct names, not the three existing case
names. The existing cases are named "EmitMessageDelivered",
"EmitMessageAcked", and "EmitThreadVerified". A new case with the
same name would still pass; t.Run would append a silent #01 suffix
and hide the case in `go test -v` output. The new cases are named
"EmitMessageDeliveredInvalidEnvelope",
"EmitMessageAckedInvalidEnvelope", and
"EmitThreadVerifiedInvalidEnvelope".

This is a test-only change. agent/translator.go already checks bus
for nil before calling the verify step, in that order, for all three
EmitX functions. The plan API section above already documents that
order. No exported symbol changes, so api/agent.txt does not change.
No import edge changes, so policy/layers.json does not change. The
builder edits only agent/agent_test/translator_test.go.

### Run-loop tests

- `run_test.go` — red-green table cases for `Run`'s own checks: the
  nil-`wait`, nil-`bus`, and empty-`threadID` sentinels and their
  check order, a confirmed one-step run, a corrected one-step run, an
  escalated one-step run, a one-step run where `wait` returns a plain
  error wrapping nothing (proving the ack-error short-circuit is
  unconditional, not special-cased to escalation), and a zero-step
  plan.
- `run_integration_test.go` — a real two-step sequential plan proves
  the ack for step one confirms before `wait` runs for step two, each
  built message independently passes `Message.Validate()`, the bus
  receives the five expected events in order, and an escalated second
  step returns `errors.Is(err, agent.ErrEscalated)` with no
  `ThreadVerifiedEvent`. Runs under `go test -race`.
- `run_panel_integration_test.go` — the multi-member panel path: a
  two-member panel step alongside one gated step proves `wait` runs
  zero times and no message events fire for the panel members; a
  plan that is only a two-member panel proves `Run` succeeds with
  zero `wait` calls and no `ThreadVerifiedEvent`. Runs under `go test
  -race`.
- `run_bench_test.go` — a two-step run with an in-process,
  synchronous `AckWait`, target under two milliseconds, with an
  `AllocsPerRun` budget recorded by the builder.
- `lifecycle_integration_test.go` — the full-lifecycle proof beyond
  the run-loop table above: one one-member
  panel step and one sequential step, a real identity, card, plan, and
  machine model, asserting the exact ordered event sequence for a
  successful run and that a forced ack failure halts the walk without
  erasing the events already emitted for the steps that already
  passed.

### Liveness tests

- `liveness_test.go` — red-green cases for `hb == nil` (fully inert),
  `hb != nil` (beat lands before `wait`, `Forget` runs after `Run`
  returns on a success, an escalation, and a plain error), one id
  reused across a two-step run, and a one-nanosecond-timeout case that
  proves staleness deterministically with no `time.Sleep`.
- `liveness_integration_test.go` — a full successful run leaves
  `hb.Dead` empty, two goroutines run the same `*Agent` on two threads
  against one shared `Monitor` with no stale-beat failure, an
  external-sweep case where a second goroutine polls `Dead` and
  cancels `ctx` to unblock a stalled `wait`, and a panel-coverage-gap
  case: a two-member panel plan with no gated step, run with
  `hb != nil`, asserting `hb.Dead` stays empty and `hb.Alive` reads
  false for the panel wave's would-be id, pinning the disclosed scope
  limit above. Runs under `go test -race`.
- `run_bench_test.go` gains `BenchmarkRunWithHeartbeat`, compared
  against the existing nil-`hb` benchmark.

### Room-stamping tests

- `run_test.go` gains two cases in the existing `Run` table: a
  non-empty `room` argument proves the built message's `Room` equals
  the supplied name and still passes `Message.Validate()` and
  `Message.VerifySignature()`; an empty `room` argument proves
  `Message.Room` stays the zero value, reproducing today's behavior.
- `run_room_integration_test.go` — the cross-package proof: a real
  `room.Room`, admitted with the agent's signer, calls `Accepts` on a
  `Run`-built message signed with a non-empty `room` argument equal to
  the room's `ID()`, and the call returns nil. A second case reruns
  the same setup with `room` left empty and asserts `Accepts` now
  returns a non-nil error, pinning the gap as a regression check.
- Every existing call site to `a.Run(...)` gains a trailing `""`
  argument, mirroring the heartbeat parameter's mechanical rollout across the same
  files. No existing assertion changes.
- `docs/examples/agent-dispatch.md` gets fixed in the same phase:
  thread the room string through the example's `Run` call using the
  real `room.Room`'s `ID()`, and change the example's `wait` closure
  to return the `Accepts` error instead of only printing it, so the
  final printed status honestly reflects the run's real outcome.

### Accessor tests

`agent/agent_test/accessors_test.go` proves `Plan` returns the bound
definition pointer and `Signer` matches the identity's own `Signer`
value.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for agent and for the total.
- The agent row in policy/layers.json lists identity, discovery, and
  flow. The row lands with this plan, before the code.
- `api/agent.txt` lands through make api-update in the same change as
  the code. The lock matches the surface in the API section.
- docs/architecture.md and docs/README.md gain the agent plan
  reference in the same change as the code.
- The phase adds no conformance vectors. Agent composes existing
  wire-validated blocks; it defines no new wire schema of its own.

### Translator: verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for agent and for the total, with the
  translator's new lines counted in.
- The agent row in policy/layers.json gains envelope and events. The
  row change lands with this plan update, before the code.
- envelope's row and events's row in policy/layers.json stay empty.
  Neither package gains an import of the other or of agent.
- `api/agent.txt` gains three functions, three events.Name constants,
  and one sentinel, through make api-update in the same change as the
  code. `api/envelope.txt` and `api/events.txt` stay unchanged; this
  phase adds no exported symbol to either package.
- `agent/doc.go`'s file map gains the new file names this phase adds,
  for example translator.go and events.go.
- docs/architecture.md's agent/ bullet gains the three Emit functions
  and the events/envelope import edges, in the same change as the
  code.
- This phase adds no conformance vector. It defines no new wire
  schema. It composes envelope.Message, envelope.Ack, and
  envelope.VerifyThread, all already vector-covered in envelope.

### Gap closure: verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- `go test -run TestEmitNilBusReturnsErrNoBus -v ./agent/...` shows
  six subtests, each with a distinct name: EmitMessageDelivered,
  EmitMessageAcked, and EmitThreadVerified from the existing
  valid-envelope pairing; EmitMessageDeliveredInvalidEnvelope,
  EmitMessageAckedInvalidEnvelope, and
  EmitThreadVerifiedInvalidEnvelope for the new invalid-envelope
  pairing. All six pass, and none carries a t.Run #01 suffix.
- `api/agent.txt` does not change. `policy/layers.json` does not
  change. Neither gate has a diff to review for this fix.
- The builder touches only
  `agent/agent_test/translator_test.go`. A diff that touches
  `agent/translator.go` fails review; the check-order logic there is
  already correct.
- The reviewer repeats the swapped-guard reproduction from the
  finding: swap the nil-bus and verify guards in EmitMessageDelivered,
  in EmitMessageAcked, and in EmitThreadVerified, one function at a
  time, in a scratch copy. The updated test suite must show three
  fresh failures across the three scratch mutations, one per new
  case, where the old suite passed on all three.

### Run entry point: verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for agent and for the total, with
  `Run`'s new lines counted in.
- The agent row in policy/layers.json gains machine. The row change
  lands with this plan update, before the code.
- machine's row in policy/layers.json stays `["events"]`; machine
  gains no new import.
- `api/agent.txt` gains `AckWait`, `Run`, `ErrEscalated`, `ErrNoWait`,
  and `ErrNoThread`, through make api-update in the same change as the
  code. `ErrNoBus` is reused, not re-declared; `api/envelope.txt`,
  `api/events.txt`, and `api/machine.txt` stay unchanged.
- `agent/doc.go`'s file map gains the new file name this phase adds,
  for example run.go.
- docs/architecture.md's agent/ bullet gains `Run`, `AckWait`, and the
  machine import edge, in the same change as the code.
- This phase adds no conformance vector. It composes envelope.Message
  and envelope.Ack, both already vector-covered in envelope, and
  defines no new wire schema.
- docs/plans/a2a.md stays status future. This phase does not add code
  to a2a and does not require it.

### Heartbeat: verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `agent` and for the total, with
  the new heartbeat lines counted in.
- The `agent` row in `policy/layers.json` gains `heartbeat`. The row
  change lands with this plan update, before the code.
- `heartbeat`'s row in `policy/layers.json` stays `["events"]`; it
  gains no new import and does not import `agent`.
- `api/agent.txt` gains the changed `Run` line, through
  `make api-update` in the same change as the code. No other line
  changes; `api/heartbeat.txt` stays unchanged.
- `go test -race ./agent/...` passes, covering the two-goroutine and
  the external-sweep integration cases.
- `docs/architecture.md`'s `agent/` bullet gains one sentence on the
  optional `hb` parameter and the `heartbeat` import edge, in the same
  change as the code.
- This phase adds no conformance vector. `Run`'s heartbeat addition
  carries no wire form of its own.

### Room stamping: verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `agent` and for the total, with
  the new room-stamping line counted in.
- `policy/layers.json` does not change. The `agent` row already lists
  `envelope`; this phase adds no new import edge.
- `api/agent.txt` gains the changed `Run` line, through
  `make api-update` in the same change as the code. No other line
  changes; no other package's API lock changes.
- `go test -race ./agent/...` passes, covering the room integration
  case.
- `docs/architecture.md`'s `agent/` bullet gains one sentence on the
  optional `room` parameter, in the same change as the code.
- `docs/packages/agent.md` gains the updated `Run` signature and one
  new invariant line stating the room-stamping rule, in the same
  change as the code.
- `docs/architecture.md`'s Addressing bullet gains one sentence
  noting `agent.Run` can stamp a caller-chosen room name onto each
  step message before signing. Required by AGENTS.md: message-
  semantics changes update `docs/architecture.md` in the same
  change as the code.
- `docs/examples/agent-dispatch.md` gains the room-string fix,
  verified by re-running the program against the real module; the
  final printed status must match the program's real outcome.
- This phase adds no conformance vector. `Message.Room` already has
  wire-level coverage in `envelope`'s own vectors.

### Context budget: tests

- `run_budget_test.go` — a nil budget, a generous valid budget, an
  invalid budget surfacing the wrapped `Validate` error before `wait`
  runs, a `MaxEvents` cap smaller than the plan's step count tripping
  `ErrOverBudget` mid-plan, a cumulative `MaxBytes` cap that only the
  sum of two steps' payloads exceeds, a `MaxBytes` cap the first
  step's own payload alone exceeds, and the `Fits`-before-`hb.Beat`
  ordering proof (mirroring `TestLivenessFullRunLeavesDeadEmpty`).
- `run_panel_integration_test.go` gains
  `TestBudgetPanelWaveReachesNoCheck`, mirroring
  `TestLivenessPanelWaveReachesNoBeat`: a two-member panel plan with
  no gated step, run with a budget a checked panel would trip,
  completes with no `ErrOverBudget`, pinning the disclosed panel scope
  limit.
- Every existing call site to `a.Run(...)` gains a trailing `nil`
  argument, mirroring the heartbeat and room parameters' mechanical
  rollout across the same files. No existing assertion changes.

### Context budget: verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `agent`, for `contextbudget`, and
  for the total, with the new budget lines counted in.
- The `agent` row in `policy/layers.json` already lists
  `contextbudget`, landed ahead of this phase by commit 07cddc7; this
  phase makes no further edit to `policy/layers.json`.
- `api/agent.txt` gains the changed `Run` line and the new
  `ErrOverBudget` sentinel; `api/contextbudget.txt` is a new lock;
  both land through `make api-update` in the same change as the code.
- `go test -race ./agent/...` passes, covering the budget cases.
- `docs/architecture.md` gains a `contextbudget/` bullet, and the
  `agent/` bullet gains the `budget` parameter and `ErrOverBudget`, in
  the same change as the code.
- `docs/packages/agent.md` and `docs/examples/agent-dispatch.md` gain
  the new nine-argument `Run` signature, in the same change as the
  code.
- This phase adds no conformance vector. `Limits` defines no wire
  schema, and `agent.Run`'s budget check changes no byte on the wire.

### System integration: tests

`agent/agent_test/exchange_integration_test.go` and
`exchange_bench_test.go` prove every shipped block composes into one
real two-agent exchange, with no mock at the trust boundary: two real
`identity.Identity` values, a real `room.Room`, a real
`tools.Registry`, a real `memory.Store`, and a real `events.Bus`. The
request routes through `a2a.ToPart`/`a2a.FromPart` once, standing in
for a transport hop. `TestExchangeSignedRequestConfirmedAck` runs the
full path: Agent A sends a signed request; the test's `AckWait`
closure represents Agent B, verifying the round-tripped signature,
checking room admission through `Room.Accepts`, running a tool,
writing the result to the shared `memory.Store`, and confirming an
`envelope.Ack`. It asserts `Run` returns `machine.Status("fulfilled")`,
the captured message passes `envelope.VerifyThread` from the test's
own vantage point, the shared context landed in Agent B's store, and
the bus recorded `MessageDeliveredEvent`, `MessageAckedEvent`,
`StepCompletedEvent`, then `ThreadVerifiedEvent`, in order.
`TestExchangeRejectsUnadmittedReceiver` proves the trust boundary is
real: with Agent B's signer never admitted to the room, `Accepts`
rejects the message, `Run` returns a non-nil error, and neither the
tool nor the memory store is ever called. `BenchmarkExchange` times
`a.Run` alone, with a fresh thread ID per iteration; a companion
`TestExchangeAllocBudget` pins the allocation ceiling.

### System integration: verification

`make verify` passes. `go test -race ./agent/...` passes, including
`BenchmarkExchange` under `go test -bench`. This work adds no exported
symbol and no new package, so no `api/*.txt` file and no
`policy/layers.json` row changes. It adds no conformance vector: it
composes `envelope.Message`, `envelope.Ack`, and `a2a.Mapped`, each
already vector-covered in its own package.

### Accessors: verification

`make verify` passes. `api/agent.txt` gains the two methods via
`make api-update`, committed in the same change.
`policy/layers.json` stays unchanged. `docs/packages/agent.md`
documents both accessors in the same change as the code.
