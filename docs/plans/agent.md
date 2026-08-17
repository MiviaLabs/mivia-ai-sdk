# Plan: agent

Status: phase 12 and phase 20 shipped. Phase 20's contract is
docs/plans/agents/phase20_envelope_composition.md. Phase 12 depends on
identity, discovery, and flow, all shipped. Phase 20 adds envelope and
events, both shipped. Phase 13 adds the execution loop; phase 14 adds
tools.

## Goal

Define one agent declaratively. An Agent binds an identity, a
capability card, and a step plan into one value. The definition is
data. It states who the agent is, what it can do, and what it runs.
It does not run yet.

## Scope

Inside: the Agent type, New, Name, and Capabilities. New wires an
identity, a discovery card, and a flow plan into one Agent.

Outside: the execution loop, the tool registry, the memory store, and
the transport binding. Those belong to phase 13 and later phases. This
package owns no goroutine, no context.Context walk, and no network
call.

The package imports identity, discovery, and flow. No other internal
import through phase 12. Stdlib only: errors and fmt, for the sentinel
errors and the wrapped card error. The policy row was
`"agent": ["identity", "discovery", "flow"]` through phase 12.

### Phase 20 addition: scope

Phase contract: docs/plans/agents/phase20_envelope_composition.md.
This phase adds a thin translator inside the same `agent` package. It
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

## API

The surface below is the lock target. It lands in `api/agent.txt` via
make api-update.

- `type Agent struct` holds an `*identity.Identity`, a
  `discovery.Card`, and a `*flow.Definition`. All three fields stay
  unexported. A caller reaches them through `Name`, `Capabilities`,
  and the accessors phase 13 adds for the run loop.
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
state behind a false copy. Second, phase 13 adds mutable execution
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

### Phase 20 addition: the envelope-to-events translator

Three exported functions, three exported `events.Name` constants, and
one exported sentinel land in this phase. They join the phase 12
surface above. Nothing from phase 12 changes.

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
   its own test suite. Phase 20's race test proves the translator adds
   no shared mutable state of its own. Many goroutines call
   `EmitMessageDelivered`, `EmitMessageAcked`, and `EmitThreadVerified`
   against one shared bus. An atomic counter proves each call still
   delivers exactly once.

The expected `api/agent.txt` lock, phase 20 lines included:

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

### Phase 20 addition: translator tests

Test files land in `agent/agent_test/`, alongside the phase 12 files:

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
  - A nil bus argument to each of the three functions: expect
    errors.Is against ErrNoBus.
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

### Phase 20 addition: verification

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
