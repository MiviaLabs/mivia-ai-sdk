# Phase 49: agentrun composition layer

Status: future. Plan-only; it has not gone through plan review yet.
Depends on phase 48, which has shipped. It adds one new top-level
package. Its four accessors shipped with phase 48.

## Why this phase exists

The compiled composition example runs one agent with one step in 253
lines (`docs/examples/_agentcomposition/main.go`). Three costs repeat
in every caller:

- `agent.Run` takes nine positional parameters; three are trailing
  optionals (`agent/run.go:87`). The example passes `nil, "", nil` in
  a row.
- `events.Bus.Emit` fails a name with no subscriber
  (`events/bus.go:81`). Every caller writes the no-op subscription
  ritual (`main.go:140`) or the run errors.
- Every caller rebuilds the same `AckWait` closure: run tool, store
  result, confirm ack (`main.go:154`).

Nothing validates a plan against its machine up front. A missing
transition row fails at run time inside `pickTransition`
(`flow/runner.go:401`). The caller learns the mismatch mid-run.

## Goal

One config struct turns an agent, a machine, and optional blocks into
a runnable pipeline. One `New` validates everything the run needs:
the transition matrix, the tool names, the budget, and the option
combinations. One `Run` call drives it.

## Scope

Inside:

- New package `agentrun` with `Options`, `New`, `Runner`, `Run`, and
  `Bus`.
- Default wiring: a built bus with no-op handlers subscribed to the
  three agent event names.
- A built ack chain: per gated step, run the tool named by the step
  ID, store the result, confirm the ack.
- Human escalation: an `Ask` notifier receives a `channel.Question`
  when the chain or the run escalates.
- An `Artifacts` bag plus a `PayloadOf` builder, so phase 48's
  `PayloadFrom` closures read a prior step's result without captured
  pointers.
- `ValidateMatrix`, exported: the static plan-versus-machine check.
- Accessors the validator needs, already shipped with phase 48:
  `flow.Definition.Steps`, `flow.Definition.Panels`, `agent.Plan`,
  and `agent.Signer`, the signer hex for the default ack `From`.

Outside:

- Any import of `mcp`, `a2aclient`, or `ledger`. Their wiring stays
  at caller seams: `mcp.RegisterAll` fills the `tools.Registry` the
  caller passes; `a2aack` and `taskrun` are their own phases.
- Any change to `agent.Run`'s signature. `agentrun` wraps it.
- Machine derivation from a plan. That helper waits for its own
  caller evidence.
- Provider-driven payload drafting. The example's one call site does
  not justify a template engine.

## API

```go
package agentrun

type Options struct {
	Agent    *agent.Agent
	Machine  *machine.Definition
	Receiver *identity.Identity // ack From; defaults to Agent.Signer()
	Bus      *events.Bus        // built and subscribed when nil
	Tools    *tools.Registry    // built ack chain runs tools by step ID
	Scope    *tools.Scope       // optional; needs Tools
	Store    *memory.Store      // optional; needs the chain
	Ask      channel.Notifier   // optional; needs the chain
	AskTo    string             // Question.Recipient; needs Ask
	Artifacts *Artifacts        // optional; chain writes results here
	Room     string
	Budget   *contextbudget.Limits
	Monitor  *heartbeat.Monitor
	Wait     agent.AckWait      // overrides the built chain
}

func New(opts Options) (*Runner, error)

type Runner struct{}

func (r *Runner) Run(ctx context.Context, threadID string,
	in machine.InOut) (machine.Status, machine.InOut, error)

func (r *Runner) Bus() *events.Bus

func ValidateMatrix(plan *flow.Definition, m *machine.Definition) error

type Artifacts struct{}

func (a *Artifacts) Set(step, value string)
func (a *Artifacts) Get(step) (string, bool)

// PayloadOf builds a flow PayloadFrom closure reading one step's
// artifact.
func PayloadOf(step string, a *Artifacts) func(machine.InOut) string

var (
	ErrNoAgent        = errors.New("agentrun: agent is required")
	ErrNoMachine      = errors.New("agentrun: machine is required")
	ErrAmbiguousWait  = errors.New("agentrun: Wait overrides Tools; set one")
	ErrNoTools        = errors.New("agentrun: Scope, Store, Ask, or Artifacts needs Tools")
	ErrNoRecipient    = errors.New("agentrun: Ask needs AskTo")
	ErrResultNotText  = errors.New("agentrun: tool result is not a string")
)

// agent package, same change:
//   func (a *Agent) Plan() *flow.Definition
//   func (a *Agent) Signer() string

// flow package, same change:
//   func (d *Definition) Steps() []Step   // copy; Sub children nest
//   func (d *Definition) Panels() []Panel // copy
```

`Runner.Run` checks `threadID` first, then calls `agent.Run` with the
wired values. `Wait`, when set, is the only ack resolver. The built
chain, when `Tools` is set, resolves each gated step:

1. `RunScoped(ctx, step.ID, tools.InOut{Value: msg.Payload}, Scope)`.
2. The result must be a string: a non-string `Out.Value` fails the
   step with `ErrResultNotText`, naming the tool.
3. `Store.Put` of the result, when `Store` is set.
4. `Artifacts.Set(step.ID, result)`, when `Artifacts` is set.
5. `NewAck(msg, receiver, result)` and `Confirm`.

A chain error wrapping `agent.ErrEscalated`, with `Ask` set, becomes
one `Ask` round trip. An approved answer confirms the ack with the
answer payload. A declined answer fails the step.

## Validation

`New` runs every check before any wiring, in a fixed order, and
returns the first failure. Each option rule names its error above;
the budget and matrix rules return their own wrapped errors.

- `Agent` non-nil; `Machine` non-nil.
- `Wait` and `Tools` are mutually exclusive.
- `Scope`, `Store`, `Ask`, and `Artifacts` each require `Tools`.
- `Ask` requires a non-empty `AskTo`.
- `Budget`, when set, must pass its own `Validate`.
- `ValidateMatrix(Agent.Plan(), Machine)` must pass.
- With `Tools` set, every step ID in `Agent.Plan().Steps()`, recursing
  into `Sub` definitions, must resolve in the registry. A missing
  name fails at `New`, not mid-run.

`ValidateMatrix` is the transition-matrix check. It walks
`Plan().Steps()` and `Plan().Panels()` and computes each step's
predecessor status set:

- A step with no needs starts from `{Machine.Initial()}`.
- A plain need contributes its `To`. A need with `Sub` or `Loop`
  contributes the child graph's reachable final statuses, computed
  recursively, because the runner targets the child's final status,
  not the parent's `To`.
- A step admitted through `AdmissionOnFailed` adds, for each failed
  need, both that need's predecessor set and its contributed final
  statuses: a `Fire` failure leaves the pre-fire status, while a
  `Route` or loop failure fires after the transition and leaves the
  post-step status.
- A panel wave unions its members' predecessor sets; panel `To`
  homogeneity is already enforced by `flow.New`.

For each predecessor `p` in a step's set, the machine must hold
exactly one row `From=p` `To=step.To`. Zero rows abort the walk at
run time in `pickTransition`; two rows abort it as an ambiguity that
no fallback catches. The error names the step, the predecessor, and
the missing or ambiguous target.

`machine.Definition.Validate` rejects duplicate `From`+`Trigger`
rows, not duplicate `From`+`To`, so the exactly-one rule is load
bearing.

Scope limit, stated: the matrix models the declared happy-path
predecessors. It does not model route-excluded or skipped-need
paths, where a `Route` or `AdmissionOnFinished` shifts the live
status outside every need's `To`. The caller's table owns those
rows. The guarantee is: every declared predecessor has exactly one
row. It is not a proof the walk never aborts.

`New` subscribes one no-op handler to each of `MessageDeliveredEvent`,
`MessageAckedEvent`, and `ThreadVerifiedEvent` on the resolved bus,
then returns it through `Bus()` for caller additions.

## Tests

`agentrun/agentrun_test/`, one external test package:

- `options_test.go`, table-driven: every `New` rejection above, plus
  the accept path.
- `matrix_test.go`, table-driven: a root step missing its row; a
  dependent missing its row; a fallback step needing both the failed
  need's predecessors and its final statuses; a `Sub` need checked
  against the child's final statuses, not the parent's `To`; an
  ambiguous pair of rows for one predecessor; the accept path.
- `run_integration_test.go`: a real two-step agent over real blocks.
  Step one runs a tool; step two's `PayloadFrom` reads step one's
  artifact through `PayloadOf`. Assert the ack restatements, the
  stored refs, the thread verification, and the final status.
- `escalation_integration_test.go`: a tool error wrapping
  `agent.ErrEscalated`, resolved by a test-local `channel.Notifier`.
  One sub-test approves; one declines.
- `budget_test.go`: a `Limits` value that trips on step two, asserting
  `agent.ErrOverBudget` and no ack for that step.

A compiled example, `docs/examples/_agentrun/main.go`, sits beside
the existing composition example and shrinks the same wiring to one
`Options` literal. It is a walkthrough, not a test.

## Verification

- `policy/layers.json` gains
  `"agentrun": ["agent", "channel", "contextbudget", "envelope", "events", "flow", "heartbeat", "identity", "machine", "memory", "tools"]`.
- `make api-update` lands `api/agentrun.txt`, the `agent.Plan` and
  `agent.Signer` lines in `api/agent.txt`, and the `flow.Definition`
  `Steps` and `Panels` lines in `api/flow.txt`, in the same change.
- `make verify` passes; `agentrun` and the module total hold the 85
  floor.
- `go test -race ./agentrun/...` passes.
- `docs/plans/agentrun.md` appears from `TEMPLATE.md` when the
  package lands; this phase plan folds into it.
- `docs/packages/agentrun.md` and `docs/examples/agentrun.md` ship
  with the package; the example pair joins
  `scripts/check_examples_sync.py`'s `PAIRS`.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
