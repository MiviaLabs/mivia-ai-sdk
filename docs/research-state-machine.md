# Research: a workflow state primitive

Date: 2026-08. Question: does this SDK need its own workflow state
package, or should it use an existing Go library? The same primitive
will back agents and other automations. This report records the
findings and the decision. It challenges the build-vs-buy answer with
facts.

Update: the scope grew past a simple step list. The team now needs
entry and exit actions, workflow chaining, parallel step panels, and
DAG step decomposition. The decision splits the work across two
packages. A typed state machine handles statuses, gates, inputs and
outputs, and entry and exit actions. A step runner handles steps,
panels, parallel execution, and chaining. Both are v1 scope. The
build-don't-buy verdict stands. See docs/plans/machine.md and
docs/plans/flow.md for the two plans.

## The requirement

The team wants a generic, extensible state primitive for workflows and
automations. It must define steps, gates, inputs, outputs, and states
or statuses. It must stay simple. It must not be overengineered. Later,
agents and other automations in the system will use it. The word
"generic" narrows the search. A niche enum package does not qualify.

Two existing plans already reserve space for this work. The flow plan
in docs/plans/flow.md describes a step list with dependencies. The a2a
plan and the agent research record route multi-step work to a future
flow package. This report covers the same concern from the library
angle.

## The landscape

No Go library covers the whole requirement as one small, generic,
stdlib-only construct. The ecosystem splits into two camps. One camp
is simple but narrow: flat finite state machines. The other camp is
capable but heavy: distributed workflow engines. The middle ground is
thin.

The meeting note is the deciding constraint. This repo has one hard
rule: standard library only, no third-party imports. The gate
check_gomod.py enforces it. The a2a plan grants one possible exception
for the official A2A client. That exception is about transport, not
about state. Any pull-in must survive this rule first.

### The simple camp

`looplab/fsm` is the classic flat finite state machine. It has zero
dependencies and a small footprint. It gives states, events, and
callbacks. It does not give guards as objects, typed payloads, or
inputs and outputs. It is a base machine, not a workflow layer.

`cocoonspace/fsm` is a high-performance variant. It has 89 stars and no
maintenance since 2023. `enetx/fsm` is generic but small. Both stay in
the same narrow band.

### The capable camp

Temporal and Cadence are distributed execution platforms. They need a
server, a database, and several services. They are runtimes, not
importable libraries. They are the definition of overengineering for
this SDK.

`cschleiden/go-workflows` is a durable workflow framework. Its go.mod
pulls many subsystems. It is not stdlib-only. `luno/workflow` uses
generics and type-safe status enums. It needs persistence and eventing
adapters. Both solve horizontal scaling the SDK does not need.
`Azure/go-workflow` has three library imports and is a DAG executor,
not a state machine.

### The real contenders

Two libraries warrant a close look.

`qmuntal/stateless` is a faithful port of the .NET stateless library.
It is actively maintained. It covers states, triggers, guards, and
entry and exit actions. That is the richest coverage of the five
concerns. It is stdlib-only in imports. It requires Go 1.24, which
this repo does not meet (go.mod says go 1.22). It uses reflection for
parameterized triggers, and a type mismatch panics. It has no step
model. It is a state machine, not a workflow step engine.

`luno/workflow` is the closest generic workflow library. It gives
steps with from-status, worker, and to-status. It needs store and
stream backends. It is a full orchestration framework. It violates the
stdlib-only rule for its production path.

A note on dead ends. `cristalhq/go-state-machine` returns 404 and no
longer exists. `QmHao/stateless` moved to `qmuntal/stateless`. The
Sourcegraph search confirms the move. Do not reference the dead names.

## Why buy fails here

Every useful candidate breaks at least one branch of the contract.
The capable camp needs servers or persistence. The simple camp lacks
gates, inputs, and outputs. `qmuntal/stateless` drags in reflection
and a newer Go. None gives compound steps with a runner.

Adopting a foreign library also means wrapping it. The SDK would hide
the foreign API behind one of its own. That wrapper is code the team
writes and maintains anyway. The foreign surface adds learning cost
and version churn.

The build-vs-buy test asks one question. Is the thing so complex that
buying beats building? A typed finite state machine with guards and
step metadata is small. The envelope package already models states as
typed enums with Validate methods. The Ack lifecycle is a three-state
machine in the repo today. The skills are in place.

## Why build wins

A small, typed, stdlib-only machine fits the repo conventions. The
repo already treats states and statuses as typed constants with
Validate methods. A hand-rolled machine is a handful of files. It
matches the architecture rule that new concerns get new subpackages.

The machine is smaller than a wrapper around a third-party library.
It adds no dependency directive, so check_gomod.py stays green. It
needs a plan, an API lock, and a policy row, and the gates already
require those for any new package.

The requirement is a specification, not a download. A state machine
with steps, gates, inputs, outputs, and states is a definition
language. That language is the asset the team wants. Building it in
house keeps the shape ours and the semantics explicit.

## The adversarial challenge

A hostile reviewer should push on the verdict. The strongest counter
is that a naive team builds a worse engine than a mature library. The
expanded scope raises that risk. DAG scheduling and parallel panels
are real complexity. The mitigation is to split the concerns and keep
each package small.

The second counter is YAGNI. The SDK has one workspace and no shipped
workflow yet. A workflow engine before a consumer is speculative
generality, which AGENTS.md rejects. The team answered this: the
automations are coming. The design lands now, the code grows stepwise.
Build the state machine first. Add panel scheduling only when a real
task needs parallel steps.

The third counter is overengineering. Entry and exit actions, panels,
chaining, and DAG decomposition are the features that turn a small
machine into Temporal. The mitigation is a hard layer boundary. The
machine never schedules. The runner never knows a status count. Each
stays replaceable and testable on its own.

The fourth counter is the stdlib-only rule itself. The rule is the
reason buy fails. A reviewer might ask whether the rule should relax.
The answer is no for state and flow. State and scheduling are not
transport. The one carved-out exception stays transport only.

## The decision

Build both packages in house, stdlib-only. Do not buy an engine. The
verdict from the earlier analysis holds. Every candidate library
violates the contract or lacks the needed features.

Two packages split the concern. This split follows the architecture
rule that one package owns one concern.

The machine package owns status mechanics. It holds typed statuses,
gates, inputs, outputs, entry actions, and exit actions. It has no
graph. It has no scheduler. It moves one record between statuses when
a gate passes. See docs/plans/machine.md.

The flow package owns step scheduling. It holds a step graph, panels,
parallel execution, and chaining. It composes the machine for each
step's status transitions. It has a Validate method on the definition.
See docs/plans/flow.md.

Both packages are v1 scope. The team builds the full shape, not a
minimal slice. Build order stays stepwise. Land the machine first.
Land panels and chaining only when a real task needs them.

The decision matches the record in research-agents.md. The execution
loop belongs to the flow package. Agents compose blocks. The machine
and the flow packages are two blocks in that composition.

## The need is real

The scheduler framework attached to the report was mild caution. The
team has a real consumer. Another system needs these capabilities now.
They are not speculative generality. A future-looking plan keeps the
code, but the requirement has a date.

The same consumer sets the quality bar. The code must be correct, not
hardened. It needs to meet the need, not outlive an industry. Simple
wins. The patterns below prove simple is enough.

## The proven patterns

Every requirement maps to a published, simple pattern. None drags in a
framework. The research sources are in the References section.

The action model covers entry, exit, and guard actions. A transition
is a table row: from, trigger, guard, to, on-exit, on-entry. The
machine resolves from and trigger, runs the guard, then the actions.
XState, PyTransitions, and qmuntal cover this split. The shape below
is the general form.

```go
type Action func(ctx Context) error
type Guard  func(ctx Context) (bool, error)

type Transition struct {
    From, To Status
    Trigger  Trigger
    Guard    Guard
    OnEntry  Action
    OnExit   Action
}
```

The DAG scheduler covers steps and decomposition. Kahn's algorithm
sorts a graph in topo order in about twenty lines. Its wave list gives
parallel panels for free. A level is a wave of nodes with no remaining
dependencies. Those nodes run together. The same pass detects cycles.
This is the core of flow.

Declarative workflow-as-data is one pattern. A step is three fields:
id, depends-on, run. Dagu spells it this way in YAML. The topo sort
validates the graph. The business logic lives in the run function.

Stdlib parallel execution has a proven shape. Put tasks in goroutines.
Gather results with a WaitGroup and a buffered channel. Combine errors
with errors.Join, which is stdlib since Go 1.20. errgroup is not
stdlib and not needed. This keeps the flow package dependency-free.

Chaining is function composition. A step is a callable that takes an
input and returns an output. Sequential chaining feeds one output to
the next input. A sub-workflow is a step that calls a nested workflow
and returns its result. Temporal spells both ways; the pattern itself
is plain function calls.

Persistence stays light. Store the current status and the payload in
one row. On load, run Validate on the stored status. Event sourcing is
the wrong tool here. It drags in an event store for a need that asks
for current state.

## Simple over hardened

The rewrite goal is a contract for the builder. Correct over hardened.
Meet the need over ride the storm. The panel scheduler runs in
goroutines with context cancellation. It does not retry, persist, or
replay. When the real consumer asks for those, they arrive on their
own plan.

## References

- looplab/fsm: https://github.com/looplab/fsm
- qmuntal/stateless: https://github.com/qmuntal/stateless
- cocoonspace/fsm: https://github.com/cocoonspace/fsm
- luno/workflow: https://github.com/luno/workflow
- Temporal: https://github.com/temporalio/temporal
- Cadence: https://github.com/uber/cadence
- cschleiden/go-workflows: https://github.com/cschleiden/go-workflows
- PyTransitions (action model): https://github.com/pytransitions/transitions
- XState (guards and actions): https://xstate.js.org/docs/guides/machines.html
- Kahn's algorithm: https://en.wikipedia.org/wiki/Topological_sorting
- Dagu (declarative steps): https://github.com/dagu-org/dagu
- errors.Join (Go stdlib): https://pkg.go.dev/errors
- errgroup (why not stdlib): https://pkg.go.dev/golang.org/x/sync/errgroup
- Go pipeline fan-in idiom: https://go.dev/blog/pipelines
- Fowler state machine pattern: https://www.martinfowler.com/psw2/state-machine.pdf

See also docs/plans/flow.md for the reserved step-runner plan. See
docs/research-agents.md for the agent block map and the execution-loop
decision. See AGENTS.md for the stdlib-only rule.
