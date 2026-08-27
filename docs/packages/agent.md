# Package reference: agent

The agent package is the composition layer. It binds one identity, one
capability card, and one step plan into a single declarative value,
then drives that plan through signed, acked, hash-chained envelope
messages. The exported surface below mirrors `api/agent.txt`.

## Types

- `Agent` — an opaque struct binding one `*identity.Identity`, one
  `discovery.Card`, and one `*flow.Definition`. Build it with `New`;
  the fields stay unexported.
- `AckWait` — `func(ctx context.Context, msg envelope.Message)
  (envelope.Ack, error)`. `Run` calls it once per step `flow.Run`
  gates behind `Confirm`, with the signed step message.

## Functions and methods

- `New(id, card, plan)` — builds an `Agent`.
- `Agent.Name()` — the card's `Name` field.
- `Agent.Capabilities()` — the card's `Capabilities` slice.
- `Agent.Plan()` — the step plan `New` bound to the agent. Returns the
  same `*flow.Definition` pointer `New` stored.
- `Agent.Signer()` — the hex signer string of the identity `New`
  bound to the agent. Returns "" for a nil `Agent` or one bound to a
  nil identity.
- `Agent.Run(ctx, threadID, m, in, wait, bus, hb, room, budget)` —
  drives the bound plan through `flow.Run`.
- `EmitMessageDelivered(ctx, bus, m)` — verifies `m`'s signature, then
  emits `MessageDeliveredEvent`.
- `EmitMessageAcked(ctx, bus, a)` — validates `a`, then emits
  `MessageAckedEvent`.
- `EmitThreadVerified(ctx, bus, msgs)` — verifies `msgs` as one
  hash-linked thread, then emits `ThreadVerifiedEvent`.

## Constants

- `MessageDeliveredEvent` — the event kind `EmitMessageDelivered`
  emits.
- `MessageAckedEvent` — the event kind `EmitMessageAcked` emits.
- `ThreadVerifiedEvent` — the event kind `EmitThreadVerified` emits.

## Failure modes

Use `errors.Is` to test these.

- `ErrNoIdentity` ("agent: identity is required") — `New` returns it
  when `id` is nil, and `Run` returns it as its first check when the
  receiver `a` or `a.id` is nil. Pinned by
  `agent_test/definition_test.go`.
- `ErrNoPlan` ("agent: plan is required") — `New` returns it when
  `plan` is nil. Pinned by `agent_test/definition_test.go`.
- `ErrNoBus` ("agent: bus is required") — `EmitMessageDelivered`,
  `EmitMessageAcked`, `EmitThreadVerified`, and `Run` return it when
  `bus` is nil. Pinned by `agent_test/translator_test.go` and
  `agent_test/run_test.go`.
- `ErrEscalated` ("agent: step escalated") — an `AckWait`
  implementation wraps it to route a step to a human instead of
  resolving an ack; `Run` propagates it unchanged. Pinned by
  `agent_test/run_test.go` and `agent_test/lifecycle_integration_test.go`.
- `ErrNoWait` ("agent: wait is required") — `Run` returns it when
  `wait` is nil. Pinned by `agent_test/run_test.go`.
- `ErrNoThread` ("agent: thread id is required") — `Run` returns it
  when `threadID` is empty. Pinned by `agent_test/run_test.go`.
- `ErrOverBudget` ("agent: context budget exceeded") — `Run`'s
  `confirmStep` wraps it when a gated step's `Fits` check against a
  non-nil `budget` fails. Pinned by `agent_test/run_budget_test.go`.

## Invariants

### New

- `New` checks `id` for nil first, then calls `card.Validate()`, then
  checks `plan` for nil, in that order. It returns the first error
  hit.
- A nil `id` returns `ErrNoIdentity`.
- A card that fails `Validate` returns that error wrapped, since
  `discovery` exports no sentinel of its own.
- A nil `plan` returns `ErrNoPlan`.
- `New` does not re-run `flow`'s cycle check. A plan built through
  `flow.New` already passed it.

### Run

- `Run` checks the receiver `a` and `a.id` for nil first, then `wait`
  for nil, then `bus` for nil, then `threadID` for empty, in that
  order, before it touches `m` or the bound plan. Each check returns
  `machine.Status("")`, `in` unchanged, and its sentinel:
  `ErrNoIdentity`, `ErrNoWait`, `ErrNoBus`, or `ErrNoThread`.
- For each step `flow.Run` gates behind `Confirm`, `Run` builds an
  `envelope.Message` from the step's ID, `threadID`, and payload, with
  `Version`, `Intent`, and `Epistemic` set to values that pass
  `Validate` on their own.
- A step confirmed more than once in one thread keeps the thread's
  IDs unique: the second message appends `#2` to the step ID, the
  third `#3`, and so on. A looped child step and two Sub children
  sharing one step ID both hit this rule.
- `Run` signs the message with the agent's identity and chains it to
  the previous step message by setting `PrevHash` to that message's
  `Hash()`.
- `Run` calls `EmitMessageDelivered`, then calls `wait` with the
  signed message.
- A `wait` error returns unchanged, without calling
  `EmitMessageAcked`.
- A nil `wait` error runs `EmitMessageAcked`, then requires the ack's
  `Status` to equal `envelope.AckConfirmed` before the step counts as
  done. Any other status returns an error naming the step and the
  status seen.
- On a successful run with one or more gated steps, `Run` calls
  `EmitThreadVerified` exactly once, over every step message it
  built, in order.
- A run with zero gated steps returns without calling
  `EmitThreadVerified`.

### The optional heartbeat parameter

- `hb` is an optional step-liveness `*heartbeat.Monitor`. A nil `hb`
  skips every heartbeat call; `Run`'s behavior is otherwise unchanged.
- A non-nil `hb` beats one id, `a.id.Signer() + ":" + threadID`, right
  before each gated step's `wait` call.
- `Run` forgets that same id on every return path, through a deferred
  `hb.Forget` call set up once `Run` starts.
- A panel step, a step named in a `flow.Panel` with two or more
  members, never gets a beat. `flow.Run` never gates a panel wave of
  two or more members behind `Confirm`, so `Run` has no gated-step
  hook to beat from for that wave. This is a permanent characteristic
  of `Run`, not a limitation pending a fix.
- `Run` never calls `hb.Dead` and never aborts a step on staleness. A
  caller holding the same `Monitor` polls `Dead` on its own schedule.

### The optional room parameter

- `room` is an optional room name, a plain `string`. An empty `room`
  leaves `Message.Room` at the zero value; `Run`'s behavior is
  otherwise unchanged.
- A non-empty `room` makes `confirmStep` set `msg.Room = room` before
  `a.id.Sign` runs, on every gated step's built message, since
  `Sign` covers the whole canonical-JSON payload including `Room`.

### The optional budget parameter

- `budget` is an optional `*contextbudget.Limits`. A nil `budget`
  skips every budget check; `Run`'s behavior is otherwise unchanged.
- A non-nil `budget` runs `budget.Validate()` once, at the same point
  `Run` checks `wait`, `bus`, and `threadID`; an invalid budget
  returns `machine.Status("")`, `in` unchanged, and the wrapped
  `Validate` error.
- A non-nil, valid `budget` makes `Run` keep a running byte total,
  `runningBytes`, across the call: after each step's message is
  signed into `built`, `Run` adds that message's `len(payload)` to
  `runningBytes`.
- Right before the `wait` call for the step about to run, and before
  `hb.Beat`, `confirmStep` calls `budget.Fits` with `runningBytes`
  plus the about-to-run step's own payload byte length, and the
  1-indexed count of steps built so far.
- A `Fits` failure returns `ErrOverBudget`, wrapping the step ID,
  without calling `hb.Beat`, `wait`, or `EmitMessageAcked` for that
  step.
- A panel step, a step named in a `flow.Panel` with two or more
  members, never reaches `confirmStep`'s `wait` call, so a panel
  member's payload never adds to `runningBytes` and never trips
  `budget`. This mirrors the disclosed heartbeat panel gap above.

## Why this shape

`agent` is the composition layer. It imports seven other packages:
`identity`, `discovery`, `flow`, `envelope`, `events`, `heartbeat`,
and `contextbudget`. None of those seven packages imports `agent`
back. Dependency direction flows inward, from the leaf building
blocks toward the package that wires them together, so `agent`
composes signing, workflow stepping, event emission, liveness
tracking, and budget accounting without any of those packages knowing
an agent exists.

## Composing with provider, tools, mcp, ledger, and memory

`agent` imports none of `provider`, `tools`, `mcp`, `ledger`, or
`memory`. Each composes with `Run` through a seam `Run` already
exposes, not through a new import.

- `provider`. A caller calls `provider.RunTurn` against a `Completer`
  while building a step's `Payload` string, before `flow.New` runs.
  `Run` fixes `Payload` before it starts; `wait` has no way to feed
  generated content back into an already-signed message. Plan-
  construction time is the only place a model turn's output plugs in.
- `tools`. An `AckWait` closure reads `msg.Payload`, decides which
  tool call it names, and calls `Registry.RunScoped(ctx, name, in,
  scope)` before it builds the returned `envelope.Ack`. `AckWait`'s
  existing signature already gives the closure everything `RunScoped`
  needs. A nil `scope` skips both `Allowed` and `approve`.
- `mcp`. `mcp.RegisterAll` maps an MCP server's tools onto
  `tools.Tool` and adds them to a `tools.Registry`, the same `Registry`
  an `AckWait` closure already holds. A caller calls it once before
  `Run` starts; `agent` never needs to import `mcp`.
- `ledger`. A caller calls `ledger.Claim`, runs `agent.Run` as the
  claimed task's body, then calls `ledger.Complete` with the status
  `Run`'s own returned error decides: `ledger.StatusFailed` on a
  non-nil error, `ledger.StatusCompleted` otherwise.
- `memory`. A caller puts shared context into a `memory.Store` at
  plan-construction time and threads the returned ref into a
  `Step.Payload`, or puts a tool's result into the `Store` inside
  `AckWait` after `RunScoped` returns.

See [../examples/agent-composition.md](../examples/agent-composition.md)
for a runnable program composing all five.

## Cross-references

- [identity.md](identity.md) — the key `New` binds and `Run` signs
  with.
- [discovery.md](discovery.md) — the capability card `New` validates.
- [heartbeat.md](heartbeat.md) — the optional liveness monitor `Run`
  beats.
- [flow.md](flow.md) — the step graph and runner `Run` drives.
- [../architecture.md](../architecture.md) — the module map, with the
  full import graph.

## Usage

```go
id, _ := identity.New()
card := discovery.Card{
    Name:         "invoice-agent",
    Capabilities: []string{"invoice.review"},
}
plan, _ := flow.New([]flow.Step{
    {ID: "review", To: "reviewed", Payload: "review invoice 42"},
}, nil)

a, err := agent.New(id, card, plan)
if err != nil {
    // identity was nil, the card failed Validate, or plan was nil
}

wait := func(ctx context.Context, msg envelope.Message) (envelope.Ack, error) {
    // a real transport would send msg and block for the receiver's ack
    return envelope.Ack{
        MessageID: msg.ID,
        Status:    envelope.AckConfirmed,
    }, nil
}

bus := events.New()
noop := func(context.Context, events.Event) error { return nil }
_ = bus.Subscribe(agent.MessageDeliveredEvent, noop)
_ = bus.Subscribe(agent.MessageAckedEvent, noop)
_ = bus.Subscribe(agent.ThreadVerifiedEvent, noop)
// Run emits on bus per step and once per run; each name needs a
// subscriber, or events.Bus.Emit rejects the call.

m, _ := machine.New(
    machine.Status("pending"),
    machine.Transition{From: "pending", To: "reviewed", Trigger: "advance"},
)

status, out, err := a.Run(context.Background(), "task-42", m, machine.InOut{}, wait, bus, nil, "", nil)
if err != nil {
    // a step failed, escalated (errors.Is(err, agent.ErrEscalated)),
    // or an entry check rejected an argument
}
_ = status
_ = out
```
