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
	Hooks    *hooks.Registry
	Tracer   *trace.Tracer
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
- `hooks_tracer_test.go` — the pre-tool veto before the tool, the
  post-tool veto after it, the pre-tool, tool, post-tool, stop ordering with
  payload pins, and the root-to-child span nesting.
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
  "heartbeat", "identity", "machine", "memory", "tools", "hooks",
"trace"]` edges.
- `make api-update` lands `api/agentrun.txt`, the `agent.Plan` and
  `agent.Signer` lines in `api/agent.txt`, and the `flow.Definition`
  `Steps` and `Panels` lines in `api/flow.txt`, in the same change.
- `make verify` passes; agentrun and the module total hold the 85
  floor.
- `go test -race ./agentrun/...` passes.
- `docs/packages/agentrun.md` and `docs/examples/agentrun.md` ship with
  the package; the example pair joins `scripts/check_examples_sync.py`.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.

## Addendum: Artifacts wire form for cross-process resume

Status: plan, ready for plan review. This addendum closes the
step-output half of cross-process resume. `flow.Checkpoint` already
carries which steps ran and their status across a pause and a
`flow.Resume`. `Artifacts` carries what each step produced, but has no
`Encode` or `Decode`, so a caller resuming a run in a new process
cannot restore step outputs. It changes only `agentrun/wire.go`. It
adds no new package and needs no `policy/layers.json` edge:
`encoding/json` is standard library.

### Addendum goal

Give `*Artifacts` a JSON wire form so a caller can persist step
outputs alongside a `flow.Checkpoint`, then rebuild an equivalent
`*Artifacts` in a new process before calling `New` with
`Options.Artifacts` set to the rebuilt value.

### Addendum scope

Inside:

- `(*Artifacts).Encode() ([]byte, error)`, reading `values` and `runs`
  under the existing mutex.
- `DecodeArtifacts(data []byte) (*Artifacts, error)`, a package-level
  function, matching the `flow.Decode` and `machine.Decode`
  convention of a package-level decode paired with a method-form
  encode.
- `(*Artifacts).Validate() error`, checking that every step named in
  the internal run history has a matching current value equal to its
  last run's value, and that every step holding a current value has
  at least one recorded run. `Encode` and `DecodeArtifacts` both call
  it, matching the `Checkpoint`/`Definition` pattern of validating on
  both sides of the wire.
- An unexported `wireArtifacts` struct carrying `Values` and `Runs` as
  exported fields, needed because `Artifacts.values` and
  `Artifacts.runs` are unexported and `encoding/json` cannot see them
  directly. This mirrors `machine.wireDefinition`, which exists for
  the same reason: `Definition`'s fields are unexported too. This
  differs from `flow.Checkpoint`, whose fields are already exported,
  so `Checkpoint.Encode` marshals the public struct with no
  intermediate type.

Outside:

- Any change to `Options`, `New`, or `Runner`. Setting
  `Options.Artifacts` to a `DecodeArtifacts` result needs no other
  code change: `Options.Artifacts` already accepts any `*Artifacts`
  value, decoded or built by `Set`.
- Versioning, migration, or compression of the wire form. A plain JSON
  round trip matches the house pattern; the format has no prior
  version to migrate from.
- Any change to `Run`'s exported fields. `Run.MessageID` and
  `Run.Value` are already exported plain strings; they round-trip
  through JSON unchanged, no wire type needed for `Run` itself.

### Addendum design: why a method Encode and a function DecodeArtifacts

`flow.Checkpoint.Encode` and `machine.Definition.Encode` are both
methods; `flow.Decode` and `machine.Decode` are both package-level
functions. `Encode` follows that method convention:
`(a *Artifacts) Encode() ([]byte, error)`.

The decode side departs from the bare `Decode` name `flow` and
`machine` use, because each of those packages has exactly one wire
type, so `flow.Decode` and `machine.Decode` read unambiguously.
`agentrun` already exports several types (`Options`, `Runner`,
`Artifacts`, `Run`); a bare `agentrun.Decode` would not say what it
decodes. `DecodeArtifacts` keeps the function-not-method convention
and names its result.

### Addendum concurrency

`Encode` locks `a.mu` once, for its entire body: the read of
`a.values` and `a.runs`, the invariant check, and the `json.Marshal`
call, all under the same critical section. `json.Marshal` does not
call back into `Artifacts`, so holding the lock through the call is
safe and avoids a separate snapshot-copy step. A concurrent `Set` or
`SetRun` blocks until `Encode` releases the lock, so `Encode` never
observes a torn write: every entry in the marshaled `runs` map
corresponds to a completed `Set` or `SetRun` call, and the marshaled
`values` map matches the last `runs` entry per step at the instant of
the lock. A nil `*Artifacts` receiver returns the JSON of an empty
`wireArtifacts` and never touches the mutex, matching the nil-safe
`Get` and `History` pattern already in `wire.go`.

`Validate` is exported and must stay safe to call on its own, on a
live `*Artifacts` a caller can mutate from another goroutine at any
time. `Validate` acquires `a.mu` itself, for its own read of
`a.values` and `a.runs`. `Encode` does not call the exported
`Validate`. `Encode` inlines the same invariant check under the one
lock it already holds, so `Encode` never nests two acquisitions of
`a.mu` in one call. This split matters even though the invariant is
tautologically true for every `Set`/`SetRun`-built `Artifacts`: a
later change trusting `Validate` to run only while `a.mu` is already
held would deadlock the first time `Encode` called it directly.
`DecodeArtifacts` calls the exported `Validate` on the freshly built
value, after `json.Unmarshal` returns and before any other goroutine
can reach the new `*Artifacts`, so that call sees no contention.

### Addendum API

New in `agentrun`:

```go
// Encode serializes a's current values and run history to JSON. It
// validates first. It is safe for concurrent use; a concurrent Set
// or SetRun blocks until Encode returns.
func (a *Artifacts) Encode() ([]byte, error)

// DecodeArtifacts parses JSON produced by Encode and validates the
// result. The returned *Artifacts is ready for Get, History, Set,
// SetRun, and a later Encode.
func DecodeArtifacts(data []byte) (*Artifacts, error)

// Validate reports whether a holds internally consistent state:
// every step named in its run history has a current value equal to
// its last run's value, and every step holding a current value has
// at least one recorded run. Encode and DecodeArtifacts both call it.
func (a *Artifacts) Validate() error
```

No other exported symbol changes. `make api-update` lands these three
lines in `api/agentrun.txt` in the same commit as the code.

### Addendum tests

Tests live in `agentrun/agentrun_test/`, alongside the existing
`artifacts_concurrent_test.go`, or a new `artifacts_wire_test.go` if
that file would grow past its current focus.

- A round-trip test: `Set` and `SetRun` several steps with distinct
  `MessageID`s and multiple `History` entries per step, call `Encode`,
  call `DecodeArtifacts`, then assert every `Get` and `History` result
  on the decoded value matches the original. This is a new-capability
  test, not a bug reproduction: `Encode` and `DecodeArtifacts` do not
  exist in today's code, so the test fails to compile until the
  addendum ships.
- A concurrent test, run under `go test -race`: goroutines calling
  `Set` and `SetRun` on one `*Artifacts` while another goroutine calls
  `Encode` in a loop. Assert no race and that every `Encode` result
  decodes and passes `Validate`.
- A malformed-input test for `DecodeArtifacts`, covering both
  directions of the `Validate` invariant: invalid JSON returns an
  error; a structurally valid document where a step's current value
  differs from its last run's value returns an error from `Validate`;
  and a second, separate document where a step holds a current value
  but zero recorded runs returns an error from `Validate` too.
- An empty-value test: `(&Artifacts{}).Encode()` on a never-`Set`,
  non-nil value succeeds, and `DecodeArtifacts` on that output returns
  an `*Artifacts` whose `Get` and `History` calls read as empty.
- A nil-receiver test, distinct from the empty-value test above: `var
  a *Artifacts; a.Encode()` succeeds and returns the JSON of an empty
  `wireArtifacts`, exercising the true nil-pointer path the way
  existing tests already exercise it for `Get` and `History`.
- A resume integration test, in `run_integration_test.go` next to the
  existing `TestRunTwoStepsWithTools`, proving the addendum's stated
  purpose end to end, not only that the codec round-trips in
  isolation: run a first `Runner` through its first gated step so its
  `*Artifacts` records the step's result; call `Encode` on that
  `*Artifacts`; call `DecodeArtifacts` on the bytes; build a second,
  independent `Runner` from fresh `Options` whose `Artifacts` field is
  the decoded value; run the second `Runner` through its remaining
  step, where a `flow.PayloadFrom` built with `PayloadOf` reads the
  first step's result; assert the second run's output carries the
  first step's value, proving a later step in a new process can read a
  prior step's artifact through `PayloadOf`, not only through direct
  `Get`.

### Addendum verification

- `make api-update`; commit the `api/agentrun.txt` diff with the three
  new lines listed above, in the same commit as the code.
- `policy/layers.json` needs no new `agentrun` edge; `encoding/json` is
  standard library.
- `make verify` passes; `agentrun` and the module total hold the 85
  floor.
- `go test -race ./agentrun/...` passes.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.