# Plan: agent

Status: planned. Phase 12 of the agent work. The phase contract is
docs/plans/agents/phase12_agent_definition.md. This package depends on
identity, discovery, and flow, all shipped. Phase 13 adds the
execution loop; phase 14 adds tools.

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
import. Stdlib only: errors and fmt, for the sentinel errors and the
wrapped card error. The policy row is
`"agent": ["identity", "discovery", "flow"]`.

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
