# Plan: agentrun

## Goal

The agentrun package turns an agent, a machine, and optional blocks
into a runnable pipeline. One `New` call validates the transition
matrix, the tool names, the budget, and the option combinations. One
`Run` method drives the wired run. It removes the hand-written `AckWait`
closure and the three no-op bus subscriptions every caller repeats.

## Scope

Inside:

- The `Options`, `Runner`, `Run`, `Bus`, and `ValidateMatrix` surface.
- Default wiring: a built bus with one no-op handler subscribed to each
  of the three agent event names.
- The built ack chain: run the step's tool by ID, store the result,
  and confirm the ack. A chain error wrapping `agent.ErrEscalated`
  routes to one `Ask` round trip when `Ask` is set.
- The `Artifacts` bag and the `PayloadOf` closure builder.
- The `flow.Definition.Steps`, `flow.Definition.Panels`, `agent.Plan`,
  and `agent.Signer` accessors the validator needs. They land in this
  same change.

Outside:

- Any import of `mcp`, `a2aclient`, or `ledger`. Their wiring stays at
  caller seams.
- Any change to `agent.Run`'s signature. agentrun wraps it.
- Machine derivation from a plan. That helper waits for its own caller
  evidence.

## API

```go
type Options struct {
	Agent    *agent.Agent
	Machine  *machine.Definition
	Receiver *identity.Identity
	Bus      *events.Bus
	Tools    *tools.Registry
	Scope    *tools.Scope
	Store    *memory.Store
	Ask      channel.Notifier
	AskTo    string
	Artifacts *Artifacts
	Room     string
	Budget   *contextbudget.Limits
	Monitor  *heartbeat.Monitor
	Wait     agent.AckWait
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

func PayloadOf(step string, a *Artifacts) func(machine.InOut) string

var ErrNoAgent, ErrNoMachine, ErrNoResolver, ErrAmbiguousWait,
	ErrNoTools, ErrNoRecipient, ErrResultNotText
```

`Agent` and `Machine` are required. `Wait` and `Tools` are mutually
exclusive ack resolvers; one of them must be set. `Scope`, `Store`,
`Ask`, and `Artifacts` each need `Tools`. `Ask` needs a non-empty
`AskTo`. A set `Budget` must pass its own `Validate`. The transition
matrix must pass `ValidateMatrix`. The check simulates the runner's
declaration-order walk, so sequential roots and siblings chain: a
step's rows start from the statuses the walk rests on. It recurses
into every `Sub` child, whose own walk starts from the machine's
initial status. A loop that can re-iterate needs re-entry rows
between its distinct child finals. Every Confirm-gated step ID must
resolve in the registry when `Tools` is set.

The agent accessors added in this same change:

```go
func (a *Agent) Plan() *flow.Definition
func (a *Agent) Signer() string
```

The flow accessors added in this same change:

```go
func (d Definition) Steps() []Step   // deep copy; Sub children copy recursively
func (d Definition) Panels() []Panel // deep copy
```

## Tests

Tests live in `agentrun/agentrun_test/`, one external package. The
following files exist:

- `options_test.go` — table-driven over every `New` rejection, a case
  with neither resolver returning `ErrNoResolver`, and the accept path.
- `resolution_test.go` — the tool-resolution rules: big-panel members
  skip the check, `Sub` children resolve recursively, and a set
  `Receiver` is accepted.
- `matrix_test.go` — table-driven over `ValidateMatrix`: a root step
  missing a row; a dependent missing a row; a wave firing from the
  standing set; sibling roots chaining before a wave; a fallback
  needing failed needs' predecessors, final statuses, and the
  pre-fire row; a fallback mixing failed and succeeded needs; `Sub`
  and `Loop` needs checked against child finals; a child with
  internal needs excluding its internal step; a deep multi-level
  `Sub` chain; an ambiguous row pair; the route-exclusion scope
  limit; the accept path.
- `matrix_subrows_test.go` — `Sub` children validate their own rows
  at every depth; a non-root `Sub` step needs terminal rows only; a
  loop that can re-iterate needs re-entry rows between distinct
  finals, and `Max: 1` exempts them; sequential roots chain; sibling
  dependents chain.
- `run_integration_test.go` — a real two-step agent; assert artifacts,
  stored refs, acked events, thread verification, the non-text result,
  and the empty-string result.
- `escalation_integration_test.go` — a tool error wrapping
  `agent.ErrEscalated`, with `Ask` approving, declining, and erroring.
- `budget_test.go` — a `Limits` value that trips on step two.
- `artifacts_concurrent_test.go` — concurrent `Artifacts` use under
  the race detector, plus the nil-receiver behavior.
- `payloadof_test.go` — `PayloadOf` reads the stored artifact, and a
  nil `Artifacts` reads as empty.
- `scope_test.go` — privileged-tool denial and allowance through a
  wired `Scope`.
- `helpers_test.go` — the shared builders and test doubles.

## Verification

- `policy/layers.json` grants agentrun the
  `["agent", "channel", "contextbudget", "envelope", "events", "flow",
  "heartbeat", "identity", "machine", "memory", "tools"]` edges.
- `make api-update` lands `api/agentrun.txt`, the `agent.Plan` and
  `agent.Signer` lines in `api/agent.txt`, and the `flow.Definition`
  `Steps` and `Panels` lines in `api/flow.txt`, in the same change.
- `make verify` passes; agentrun and the module total hold the 85
  floor.
- `go test -race ./agentrun/...` passes.
- `docs/packages/agentrun.md` and `docs/examples/agentrun.md` ship with
  the package; the example pair joins `scripts/check_examples_sync.py`.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.