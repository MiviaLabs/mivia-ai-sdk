# Package reference: agentrun

The agentrun package is the composition layer. It turns an agent, a
machine, and optional blocks into a runnable pipeline. One
`agentrun.New` validates the transition matrix, the tool names, the
budget, and the option combinations. One `Runner.Run` drives the wired
run. The built ack chain runs each gated step's tool by ID, stores and
records the result, and confirms the ack. The exported surface below
mirrors `api/agentrun.txt`.

## Types and values

- `Options` — the config one `New` call wires into a `Runner`. `Agent`
  and `Machine` are required. `Wait` and `Tools` are mutually exclusive
  ack resolvers; one of them must be set. `Scope`, `Store`, `Ask`, and
  `Artifacts` each need `Tools`. `Ask` needs a non-empty `AskTo`.
- `Runner` — the composed pipeline `New` returns. Build it with `New`;
  the fields stay unexported.
- `Artifacts` — a record of each gated step's tool result, keyed by step
  ID. Safe for concurrent use. Build a zero value with `&Artifacts{}`.

## Functions and methods

- `New(opts)` — validates `opts`, then wires the blocks into a `Runner`.
  It subscribes one no-op handler to each of the three agent event names.
- `Runner.Run(ctx, threadID, in)` — drives the wired agent through the
  wired machine. An empty `threadID` fails before any block runs.
- `Runner.Bus()` — returns the resolved event bus `New` subscribed.
- `ValidateMatrix(plan, m)` — checks the plan's transition rows exist in
  `m`. Static; it does not prove the walk never aborts.
- `Artifacts.Set(step, value)` — stores `value` under `step`.
- `Artifacts.Get(step)` — returns the value under `step` and whether
  `step` held one.
- `PayloadOf(step, a)` — builds a `flow.PayloadFrom` closure that reads
  `step`'s artifact from `a`.

The agent accessors added in the same change:

- `Agent.Plan()` — the step plan `agent.New` bound.
- `Agent.Signer()` — the hex signer string of the bound identity.

The flow accessors added in the same change:

- `Definition.Steps()` — a shallow copy of the step slice; Sub children
  nest.
- `Definition.Panels()` — a copy of the panel slice.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrNoAgent` — `Options.Agent` was nil.
- `ErrNoMachine` — `Options.Machine` was nil.
- `ErrNoResolver` — neither `Wait` nor `Tools` was set.
- `ErrAmbiguousWait` — both `Wait` and `Tools` were set.
- `ErrNoTools` — `Scope`, `Store`, `Ask`, or `Artifacts` was set without
  `Tools`.
- `ErrNoRecipient` — `Ask` was set with an empty `AskTo`.
- `ErrResultNotText` — a tool result was not a string.

## Invariants

- `New` runs every check in a fixed order and returns the first failure.
- `New` subscribes one no-op handler to each of `MessageDeliveredEvent`,
  `MessageAckedEvent`, and `ThreadVerifiedEvent` on the resolved bus,
  so `Bus().Emit` never raises the "no subscriber" fault for the three
  agent event names.
- `ValidateMatrix` models the declared happy-path predecessors. It does
  not model route-excluded or skipped-need paths. Each declared
  predecessor must hold exactly one row `From=p` `To=step.To`; zero rows
  and two rows both fail, naming the step, the predecessor, and the
  target.
- The built ack chain resolves a step's tool by `step.ID`. A non-string
  result fails with `ErrResultNotText`, naming the tool. An empty-string
  result is a runtime fault from `envelope.NewAck`, not
  `ErrResultNotText`.
- A chain error wrapping `agent.ErrEscalated`, when `Ask` is set, routes
  to one `Ask` round trip. An approved answer confirms the ack with the
  answer payload; a declined answer fails the step.

## Why this shape

`agentrun` does not import `mcp`, `a2aclient`, or `ledger`. Their wiring
stays at caller seams. It does not change `agent.Run`'s signature; it
wraps it. Machine derivation from a plan stays out of scope. The
composition example in `docs/examples/_agentrun/` shows the same wiring
the older `_agentcomposition` example writes by hand, compressed into one
`Options` literal. See [../plans/agentrun.md](../plans/agentrun.md).

## Cross-references

- [agent.md](agent.md) — the composed agent `Runner` drives.
- [flow.md](flow.md) — `PayloadOf` builds a `flow.PayloadFrom` closure.
- [tools.md](tools.md) — the ack chain runs steps through the registry.
- [channel.md](channel.md) — `Ask` routes escalation through a Notifier.

## Usage

```go
artifacts := &agentrun.Artifacts{}
plan, _ := flow.New([]flow.Step{
    {ID: "review", To: "reviewed", Payload: "review invoice 42"},
    {ID: "ship", To: "shipped", Needs: []string{"review"},
        PayloadFrom: agentrun.PayloadOf("review", artifacts)},
}, nil)
a, _ := agent.New(id, card, plan)
reg := tools.New()        // hold one tool per step ID
store, _ := memory.New(4096)
m, _ := machine.New("queued",
    machine.Transition{From: "queued", To: "reviewed", Trigger: "send"},
    machine.Transition{From: "reviewed", To: "shipped", Trigger: "send"},
)

runner, err := agentrun.New(agentrun.Options{Agent: a, Machine: m,
    Tools: reg, Store: store, Artifacts: artifacts})
if err != nil {
    // a check failed at New, not mid-run
}
status, _, err := runner.Run(ctx, "thread-pipeline-1", machine.InOut{})
// status == shipped, artifacts.Get("ship") holds step two's result
```