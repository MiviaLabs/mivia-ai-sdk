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
- `Hooks` — the `hooks.Registry` gating the run: PointPreTool vetoes
  before the tool, PointPostTool observes the confirmed ack; a veto fails the step,
  PointStop reports the final status. The pre and post points fire
  only with `Tools`; the stop point fires with either resolver.
- `Tracer` — the `trace.Tracer` opening a root span per run and a
  child span per tool call, so a run's span tree reads after the run
  through `Tracer.Spans`.
- `Artifacts` — a record of each gated step's tool result, keyed by step
  ID. A step repeated inside a loop overwrites the entry, so the bare ID
  holds the latest iteration's result. Every run also appends to a
  per-step history: `History(step)` returns every `Run` in order, each
  carrying its signed message ID, so earlier failures and rejections
  stay readable. Safe for concurrent use. Build a zero value with
  `&Artifacts{}`.

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

- `Definition.Steps()` — a deep copy of the step slice; each step's
  Needs, Retry, Loop, and Sub child all copy recursively.
- `Definition.Panels()` — a deep copy of the panel slice; each member
  slice copies too.

## Failure modes

Use `errors.Is` to test these.

- `ErrNoAgent` ("agentrun: agent is required") — `New` returns it when
  `Options.Agent` is nil. Pinned by `agentrun_test/options_test.go`.
- `ErrNoMachine` ("agentrun: machine is required") — `New` returns it
  when `Options.Machine` is nil. Pinned by
  `agentrun_test/options_test.go`.
- `ErrNoResolver` ("agentrun: Wait or Tools is required") — `New`
  returns it when neither `Wait` nor `Tools` is set. Pinned by
  `agentrun_test/options_test.go`.
- `ErrAmbiguousWait` ("agentrun: Wait and Tools both set; set one") —
  `New` returns it when both `Wait` and `Tools` are set. Pinned by
  `agentrun_test/options_test.go`.
- `ErrNoTools` ("agentrun: Scope, Store, Ask, or Artifacts needs
  Tools") — `New` returns it when `Scope`, `Store`, `Ask`, or
  `Artifacts` is set without `Tools`. Pinned by
  `agentrun_test/options_test.go`.
- `ErrNoRecipient` ("agentrun: Ask needs AskTo") — `New` returns it
  when `Ask` is set with an empty `AskTo`. Pinned by
  `agentrun_test/options_test.go`.
- `ErrResultNotText` ("agentrun: tool result is not a string") — the
  built ack chain wraps it when a gated step's tool result is not a
  string. Pinned by `agentrun_test/run_integration_test.go`.
- `ErrReceiverEmpty` ("agentrun: Receiver signer is empty") — `New`
  returns it when the resolved receiver signer is empty, whether from
  `Options.Receiver` or from `Options.Agent`. Pinned by
  `agentrun_test/options_test.go`.

`Runner.Run` also propagates two sentinels the `agent` package
defines, through `errors.Is`: `agent.ErrNoThread`, when `threadID` is
empty, and `agent.ErrEscalated`, when a gated step's ack resolver
escalates to a human. Pinned by `agentrun_test/options_test.go` and
`agentrun_test/escalation_integration_test.go`.

## Invariants

- `New` runs every check in a fixed order and returns the first failure.
- `New` subscribes one no-op handler to each of `MessageDeliveredEvent`,
  `MessageAckedEvent`, and `ThreadVerifiedEvent` on the resolved bus,
  so `Bus().Emit` never raises the "no subscriber" fault for the three
  agent event names.
- `ValidateMatrix` simulates the runner's declaration-order walk on
  the all-run path. Sequential roots and siblings chain: each step's
  rows start from the statuses the walk rests on, not from the
  initial status. Zero rows and two rows both fail, naming the step,
  the source, and the target. It recurses into every `Sub` child,
  whose own walk starts from the machine's initial status. A loop
  that can re-iterate needs a re-entry row between every pair of
  distinct child finals. It does not model route-excluded or
  skipped-need paths, and a loop landing the same final twice always
  faults at run time: `machine.New` forbids the self row it needs.
- The built ack chain resolves a step's tool by `step.ID`. A suffixed
  repeat, which agent.Run mints for a step confirmed twice, resolves
  the plain tool name and records its artifact under the suffixed
  ID. A non-string
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
- See [hooks.md](hooks.md) for the registry behind `Options.Hooks`
  and [trace.md](trace.md) for the tracer behind `Options.Tracer`.
