# Plan: workflow/run

## Goal

The run package turns an agent, a machine, and optional blocks
into a runnable pipeline. One `New` call validates the transition
matrix, the tool names, the budget, and the option combinations. One
`Run` method drives the wired run. It removes the hand-written `AckWait`
closure every caller repeats.

## Scope

Inside:

- The `Options`, `Runner`, `Run`, `Bus`, and `ValidateMatrix` surface.
- Default wiring: a bus built when `Options.Bus` is nil; no handler is
  subscribed. Callers add handlers through `Bus().Subscribe`.
- The built ack chain: run the step's tool by ID, store the result,
  and confirm the ack. A chain error wrapping `workflow.ErrEscalated`
  routes to one `Ask` round trip when `Ask` is set.
- The `Artifacts` bag and the `PayloadOf` closure builder.
- The `flow.Definition.Steps`, `flow.Definition.Panels`, `workflow.Plan`,
  and `workflow.Signer` accessors the validator needs. They land in this
  same change.

Outside:

- Any import of `mcp`, `a2aclient`, or `ledger`. Their wiring stays at
  caller seams.
- Any change to `workflow.Run`'s signature. workflow/run wraps it.
- Machine derivation from a plan. That helper waits for its own caller
  evidence.

## API

```go
type Options struct {
	Agent    *workflow.Agent
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
	Wait     workflow.AckWait
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

The workflow accessors added in this same change:

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

Tests live in `workflow/run/run_test/`, one external package. The
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
  `workflow.ErrEscalated`, with `Ask` approving, declining, and erroring.
- `budget_test.go` — a `Limits` value that trips on step two.
- `artifacts_concurrent_test.go` — concurrent `Artifacts` use under
  the race detector, plus the nil-receiver behavior.
- `payloadof_test.go` — `PayloadOf` reads the stored artifact, and a
  nil `Artifacts` reads as empty.
- `scope_test.go` — privileged-tool denial and allowance through a
  wired `Scope`.
- `helpers_test.go` — the shared builders and test doubles.

## Verification

- `policy/layers.json` grants workflow/run the
  `["workflow", "channel", "contextbudget", "envelope", "events", "flow",
  "heartbeat", "identity", "machine", "memory", "tools", "hooks",
"trace"]` edges.
- `make api-update` lands `api/workflow/run.txt`, the `workflow.Plan` and
  `workflow.Signer` lines in `api/workflow.txt`, and the `flow.Definition`
  `Steps` and `Panels` lines in `api/flow.txt`, in the same change.
- `make verify` passes; workflow/run and the module total hold the 85
  floor.
- `go test -race ./workflow/run/...` passes.
- `docs/packages/workflow/run.md` and `docs/examples/workflow-run.md` ship with
  the package; the example pair joins `scripts/check_examples_sync.py`.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.

## Addendum: Artifacts wire form for cross-process resume

Status: shipped. This addendum closes the step-output half of
cross-process resume. `flow.Checkpoint` already
carries which steps ran and their status across a pause and a
`flow.Resume`. `Artifacts` carries what each step produced, but has no
`Encode` or `Decode`, so a caller resuming a run in a new process
cannot restore step outputs. It changes only `workflow/run/wire.go`. It
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
  function, matching the `flow.Decode`
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
  directly. This differs from `flow.Checkpoint`, whose fields are
  already exported, so `Checkpoint.Encode` marshals the public struct
  with no intermediate type.

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

`flow.Checkpoint.Encode` is a
method; `flow.Decode` is a package-level
function. `Encode` follows that method convention:
`(a *Artifacts) Encode() ([]byte, error)`.

The decode side departs from the bare `Decode` name `flow`
uses, because that package has exactly one wire
type, so `flow.Decode` reads unambiguously.
`run` already exports several types (`Options`, `Runner`,
`Artifacts`, `Run`); a bare `run.Decode` would not say what it
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

New in `run`:

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

One exported symbol change beyond these three: a sentinel error
`ErrArtifactsInconsistent`, alongside the package's existing sentinels
(`ErrNoAgent`, `ErrArgumentDecode`). `Validate` wraps it for both
invariant violations, so the two malformed-decode tests can assert
which direction failed with `errors.Is` instead of a message
substring. `make api-update` lands these four symbols in
`api/workflow/run.txt` in the same commit as the code.

### Addendum tests

Tests live in `workflow/run/run_test/`, alongside the existing
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

- `make api-update`; commit the `api/workflow/run.txt` diff with the three
  new lines listed above, in the same commit as the code.
- `policy/layers.json` needs no new `run` edge; `encoding/json` is
  standard library.
- `make verify` passes; `run` and the module total hold the 85
  floor.
- `go test -race ./workflow/run/...` passes.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
## Addendum: schema probe in the ack chain
Status: shipped.


`Runner.chain` resolves the step tool and asserts `tools.SchemaTool`
directly to gate payload decode. Change the gate to the published
probe:

- `if _, ok := tools.SchemaOf(t); ok` gates the decode branch.
- The decode call then reads `t` as `tools.SchemaTool`. The assertion
  cannot fail after a true probe.

This lands in the same change as the `runconfig` step tool collapse.
See `docs/plans/runconfig.md`, "Addendum: one step tool wrapper", for
the coupling analysis and the shared test and verification duties. The
edit is behavior-preserving: `tools.SchemaOf` returns true exactly
when the tool implements `tools.SchemaTool`. Existing tests in
`workflow/run/run_test/schema_decode_test.go` pin the decode and
plain-payload paths; they stay green. No exported surface changes and
no `api/` diff is expected.

## Addendum: drop the default placeholder bus handlers
Status: shipped.


`New` no longer subscribes placeholder bus handlers for the three agent
event names. `events.Bus.Emit` returns nil for a name with no subscriber,
so the workaround is dead weight. Delete the loop and the `noop`
closure at `workflow/run/options.go:147-152`. The Scope bullet naming the
default placeholder wiring is removed with the code. See
docs/plans/events.md, "Addendum: Emit accepts an unobserved event",
for the contract, the test rewrites, and the verification set.

Every commit in this change that rewrites a mandated test carries an
`Allow-Test-Change` commit-message trailer. The trailer names the
rewrites. See docs/plans/events.md, Verification.

## Addendum: an equivalence test for ValidateMatrix
Status: shipped.


Part of the maintenance addenda batch. See
docs/plans/agents/maintenance-addenda-batch.md, item 5.

`workflow/run/matrix.go:99` and `workflow/run/matrix.go:200` re-implement
flow's declaration-order scan, which `nextReadyGroup` at
`flow/runner.go:304` owns. Nothing
compared the two. Add
`workflow/run/run_test/matrix_equivalence_test.go` in the external
`run_test` package.

### What the test proves, and what it does not

The claim: the simulator demands exactly the set of transition rows
the run consumes, attributed to the same units.

Set equality is the claim. Order equivalence is not. Record the
residual gap in the test file's own doc comment:

- Nothing pins the two scans' relative ordering beyond what the row
  set forces. Two walk orders with identical demand sets are
  indistinguishable to these assertions.
- `walkSim`'s walk is machine-independent. It reads only
  `m.Initial()`; the machine affects `checkRow` alone.
- Route exclusions stay outside the comparison. So do skipped units.

`ValidateMatrix` returns only an error and exposes no order value, as
`api/workflow/run.txt:13` shows. The error text, which names the unit and
the demanded status pair, is the only side channel.

### The fixture

Statuses: `queued` (initial), `sx`, `sy`, `gathered`, `routed`,
`done`.

Steps in declaration order: `root` to `sx` with no needs; `root2` to
`sy` with no needs; `panelA` to `gathered` needing `root`; `panelB` to
`gathered` needing `root2`; `router` to `routed` needing both panel
members and carrying a `Route` returning `["finish"]`; `finish` to
`done` needing `router`. One panel holds `panelA` and `panelB`.

Two independent roots are required. A total order cannot let
declaration order decide anything, because no two units are ever ready
at once. `root` and `root2` are both ready at the start, so
declaration order alone picks `root` first. With `nextUnit`'s scan
reversed, the test fails and names `root2` and the `queued` to `sy`
pair.

The route is not a discriminator. The simulator does not model `Route`
at all, and a route that excludes nothing takes the same status path
as no route. Keep it anyway: it cheaply pins that a non-excluding
route does not perturb the chain. A route that excludes a sibling
stays out of scope.

Two fixture constraints came from running it. `flow.New` rejects a
panel member that is a direct dependent of a routed step, so the route
sits on `router`. The route must return every direct dependent,
because the simulator walks the all-run path.

### The three assertions

1. Run `flow.Run` over a complete machine with a recording `Confirm`
   and a recording `onCheckpoint`. Assert the `Confirm` order is
   `root`, `root2`, `router`, `finish`. This pins the documented gap:
   `Run` skips `Confirm` for a panel of two or more members.
2. Build a machine holding only the recorded status chain, one row per
   checkpoint. Assert `ValidateMatrix` returns nil. This proves the
   simulator demands no row outside what the run consumed.
3. For each recorded chain link, build the complete machine minus that
   one row. Assert `ValidateMatrix` fails, and assert the error text
   names both statuses and the expected unit label. The unit label is
   the load-bearing half: `"panelA panelB"` pins that the simulator
   attributes the wave's row to the wave, not to a member.

The recorded chain is `sx`, `sy`, `gathered`, `routed`, `done`. Five
links give five drop-one cases, and each one failed with its expected
message. None passed vacuously.

The two scans agree on the demanded row set. No divergence was found,
so no production change is needed.

### Addendum tests

- `workflow/run/run_test/matrix_equivalence_test.go` adds
  `TestValidateMatrixMatchesRunOrder`. It reuses `mustFlow`,
  `mustMachine`, and `assertMatrixFails` from `matrix_test.go`.
- A helper builds a complete machine over every ordered pair of
  distinct statuses, minus the pairs a case drops. Each row gets a
  distinct trigger, and every status stays reachable from `queued`.

### Addendum verification

- `make verify` passes. The `run` coverage floor holds.
- `go test -race ./workflow/run/... ./flow/...` passes.
- No `api/` diff. `policy/layers.json` already grants `run` the
  `flow` edge, and the test package is external, which the deps gate
  exempts.

## Addendum: rename from agentrun to workflow/run

Status: shipped. The package moved from `agentrun/` to
`workflow/run/` in this change. The import path is
`github.com/MiviaLabs/mivia-ai-sdk/workflow/run`. The package clause
is `run`. The test directory is `workflow/run/run_test/`; its package
clause is `run_test`. The internal test file `wire_internal_test.go`
also becomes `package run`.

Exported identifiers keep their names. `Options`, `Runner`, `Run`,
`New`, `Artifacts`, `PayloadOf`, `ValidateMatrix`, `DecodeArtifacts`,
and every sentinel error are unchanged. Only the package name, the
import path, and qualifiers change. `run.Options.Agent` keeps the
field name `Agent`; its type is `*workflow.Agent`.

The lock is `api/workflow/run.txt`. Its body equals the old
`api/agentrun.txt` body except two qualifier lines: `Agent
*agent.Agent` becomes `Agent *workflow.Agent`, and `Wait
agent.AckWait` becomes `Wait workflow.AckWait`. Sentinel error text
prefixes change from `agentrun:` to `run:`. No
test asserts the old text. The `policy/layers.json` row key is
`workflow/run`; the allowed-import list is unchanged, with `agent`
renamed to `workflow`. The pending-symbols key becomes
`workflow/run.DecodeArtifacts`.

The full builder step list, the README Quick Start spec, the docs and
example renames, and the verification greps live in
`docs/plans/workflow.md`, "Rename: agent moved to workflow". That
section is the plan of record for the whole rename. This package's
own steps are the directory moves, the `run` package clause, the
`workflow/run` import paths, the `run.` qualifiers, the
`api/workflow/run.txt` lock, and the pending-symbols key rename.
