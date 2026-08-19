# Plan: e2e

## Goal

The e2e package proves the composed SDK works end to end. Each
scenario wires real high-level blocks together and drives one full
run, asserting the outputs every layer promised. Package suites prove
each block alone; e2e proves the handoffs and the full orchestration.

## Why this package exists

The unit suites hold their floors, yet two review findings passed
them all: a validator that never checked sub-workflow rows, and
accessors that leaked mutation. Both were composition bugs. No test
drove a real run through every plan shape at once. No test wrapped
one high-level block around another. This package holds those tests.

## Scope

Inside:

- A small exported harness: deterministic tools, an event recorder,
  a thread-capturing ack resolver, and an agent builder. Every
  harness symbol has at least one scenario using it in the same
  change.
- Scenarios that compose at least two high-level blocks. A scenario
  that exercises one package alone belongs in that package's suite,
  not here.
- Metamorphic checks folded into scenarios: replay determinism,
  thread integrity, idempotent replay.
- One file per scenario, named for the behavior it pins.

Outside:

- Any test double for an SDK block. Tools, answers, and clocks may
  be scripted; blocks never.
- Performance budgets. The `bench` target owns those.
- Any new production behavior. This package ships the harness only.

## API

```go
// harness.go
func NewAgent(name string, plan *flow.Definition) (*agent.Agent, error)

type PrefixTool struct{ ToolName, Prefix string }
func (t PrefixTool) Name() string
func (t PrefixTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error)

type EscalateTool struct{ ToolName string }
func (t EscalateTool) Name() string
func (t EscalateTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error)

type Recorder struct{}
func NewRecorder() *Recorder
func (r *Recorder) Handler() events.Handler
func (r *Recorder) Names() []events.Name

type ThreadCapture struct{}
func NewThreadCapture() *ThreadCapture
func (t *ThreadCapture) Wait(ctx context.Context, msg envelope.Message) (envelope.Ack, error)
func (t *ThreadCapture) Messages() []envelope.Message
```

The fault kit is a named capability of the harness. Each decorator
wraps one seam as an interface. `FaultOn`, `HangOn`, and `PanicOn` are
three independent, reusable fault modes over the same 1-based call
counter: the FaultOn-th call errors, wrapping `ErrFault`; the HangOn-th
call blocks until `ctx` is done, then returns `ctx.Err()`; the
PanicOn-th call panics with a value wrapping `ErrFault`. Zero disables
a mode. Every other call passes through unchanged. A scenario opts
into a hang or a panic the same way it already opts into an error: by
setting the matching field.

```go
// fault.go
var ErrFault error

type FaultStore struct{ Store ledger.Store; FaultOn, HangOn, PanicOn int32 }
func (f *FaultStore) Load(...) (ledger.TaskState, bool, error)
func (f *FaultStore) CompareAndSwap(...) (bool, error)
func (f *FaultStore) Range(...) error

type FaultNotifier struct{ Notifier channel.Notifier; FaultOn, HangOn, PanicOn int32 }
func (f *FaultNotifier) Notify(...) (channel.Answer, error)

type FaultCompleter struct{ Completer provider.Completer; FaultOn, HangOn, PanicOn int32 }
func (f *FaultCompleter) Name() string
func (f *FaultCompleter) Chat(...) (provider.Response, error)
func (f *FaultCompleter) ChatStream(...) (<-chan provider.Chunk, error)

type FaultWait struct{ Inner agent.AckWait; FaultOn, HangOn, PanicOn int32 }
func (f *FaultWait) Wait(...) (envelope.Ack, error)

type HangCompleter struct{}
func (h *HangCompleter) Name() string
func (h *HangCompleter) Chat(...) (provider.Response, error)
func (h *HangCompleter) ChatStream(...) (<-chan provider.Chunk, error)
```

`HangCompleter` stays as the unconditional convenience case: it blocks
on every call, not just its HangOn-th. `FaultCompleter{HangOn: n}` is
the general form for the other seams and for a provider that must
answer normally up to call n. Neither replaces the other; both ship.

A panicking call panics with `faultErr(seam)`, the same error value
`FaultOn` would have returned. A scenario that recovers the panic
itself asserts `errors.Is` against the recovered value, the same way
it already asserts a returned error.

The kit raises the seam only where the block already exposes an
interface. A block behind a concrete type keeps no decorator: memory's
`Store` is such a case and stays out of the kit. No production block is
refactored to create a seam.

The harness imports `agent`, `channel`, `discovery`, `envelope`,
`events`, `flow`, `identity`, `ledger`, `provider`, and `tools`;
`policy/layers.json` gains exactly that row. Scenario test files import
anything they need; the deps gate exempts test files, so new scenarios
never churn the policy.

## Scenarios

Each scenario states its wiring, inputs, and asserted outputs.

- `pipeline_test.go` — wires identity, flow, machine, tools, memory,
  and agentrun. Input: one plan holding a sequential step, a
  two-member panel, a two-step sub-workflow, and a two-iteration
  loop, plus the matching machine rows and a registry of prefix
  tools. Outputs: the final status, every artifact string, store
  refs holding the same bytes, one acked event per gated step, one
  thread-verified event, and `ValidateMatrix` accepting the same
  machine.
- `thread_test.go` — wires identity, agent, and agentrun. Input: a
  two-step plan under a `ThreadCapture` resolver, run twice on fresh
  threads. Outputs: the captured messages verify as one thread
  through `envelope.VerifyThread`, the prev-hash chain links them in
  run order, and the second run reproduces the same final status.
- `escalation_test.go` — wires channel, tools, and agentrun. Input:
  an escalating tool, `Ask` over an in-memory NDJSON pipe, and an
  approved answer payload. Outputs: the question carries the step
  payload, the approved payload becomes the ack restatement, and the
  run completes. A declined answer fails the run naming the step.
- `taskrun_ceremony_test.go` — wires ledger, taskrun, and agentrun.
  Input: a work func that runs one agentrun pipeline, a second task
  needing a failed one, and a replay of the completed key. Outputs:
  work ran once, the ledger holds completed, the dependent returns
  `ErrTaskBlocked`, and the replay returns `ErrTaskDone` without
  re-running work.
- `faults_store_test.go` — wires ledger, agentrun, and the harness
  `FaultStore`. Input: a two-step pipeline whose second step runs an
  Admit, Claim, Complete ceremony over a store set to fault on the
  fifth call. Outputs: the run error matches `ErrFault` and names the
  store, step one's artifact survives in the bag, and step two never
  confirms.
- `faults_notifier_test.go` — wires channel, tools, agentrun, and the
  harness `FaultNotifier`. Input: an escalating tool with an `Ask`
  notifier set to fault on the first ask. Outputs: the escalation
  surfaces as a step failure that matches `ErrFault` and names the
  notifier, and the run returns instead of hanging.
- `faults_subagent_test.go` — wires subagent, agentrun, and the
  harness `FaultCompleter` and `FaultWait`. Input: an orchestrator
  step fans three subagents out through `RunAll`; the middle one is a
  model subagent whose provider faults on the first call. Outputs:
  `RunAll` returns three results, the two siblings land with nil Err,
  the failing spec's Err matches `ErrFault`, and the orchestrator run
  reports it. A second test pins `FaultWait` failing the run on the
  first ack.
- `faults_hang_test.go` — wires subagent, agentrun, and the harness
  `HangCompleter`. Input: a one-step runner whose provider never
  answers, with a run context that carries a deadline. Outputs: the
  run returns once the deadline fires, and the error wraps
  `context.DeadlineExceeded`.
- `faults_panic_test.go` — wires subagent and agentrun with the
  harness `FaultCompleter{PanicOn: 1}`, on a one-step, non-panel plan
  so the panicking tool call runs in the same goroutine as the test's
  own call to `Runner.Run`. Input: a one-step runner whose sole tool
  panics on its first call. Outputs: `Run` never returns normally; the
  panic propagates out of `Run` uncaught, and the test's own
  `recover`, deferred around the `Run` call, observes a value
  matching `errors.Is(recovered.(error), e2e.ErrFault)`. This pins the
  documented contract: neither `flow.Run` nor `agentrun.Runner.Run`
  recovers a panic from a Fire call on the sequential path; a panic is
  the caller's own fault, and it is the caller's job to recover one if
  it wants a run's panic reported as a result instead of a crash. See
  "Disclosed limits" below for the goroutine-concurrency case this
  scenario deliberately does not cover.

A fallback step stays out of the pipeline scenario by design. In an
agentrun run the tool chain answers inside the ack, and a rejected
ack stays fatal; a fallback catches only Fire and Route failures.
That contract belongs to flow's own suite.

## Growth backlog

Later scenarios land as their own files, one commit each. The list
is ranked by value; each closes a confirmed gap.

- Late admission blocks: a dependent admitted after its need failed
  returns `ErrTaskBlocked` and never runs work. The ledger now blocks
  at admission; this scenario holds the line end to end.
- Orchestrator over a2aack: a loopback A2A task wired as
  `agentrun.Options.Wait`, between two local steps, with the remote
  artifact feeding the next step through `PayloadOf`.
- Fallback catches a Fire failure under the real chain: a machine
  Guard fails a step with retries, a fallback reads `FailureFrom`,
  and a sibling escalates through `Ask`.
- Two agents over HTTP: a `dispatch` endpoint backed by one real
  `agentrun` runner, `Send` from another, one wrong-room line
  mid-stream. Lands with phase 52.
- Budget trips at step two: step one's artifacts and store ref
  survive, step two never acks. The artifact-survival half of this
  shipped with the fault kit: `faults_store_test.go` pins step one's
  artifact surviving a step-two failure that matches `ErrFault`. What
  remains is switching the killer from a store fault to
  `contextbudget.Limits`; the scenario body stays the same.
- Stale lease takeover around real work: the fenced loser's
  `Complete` returns `ErrFenced`; the taker's lands. The kit's
  `FaultStore` now exercises `Complete` under a failing store, so the
  takeover half is the remaining new wiring.
- A validated plan still aborts: a route-excluded dependent whose
  row exists only for the chosen sibling; the run error names the
  missing row. Pins the disclosed limit.
- Scheduled liveness: a scheduler job drives a run under a heartbeat
  monitor; a stalled ask reports dead on timeout.
- Eviction under the chain: the store evicts step one's blob; the
  run completes and `PayloadOf` still answers.

A subagent adapter is a missing concept, not a scenario: nothing
exposes a runner or agent as a `tools.Tool`. Phase 55 plans the
`subagent` package: `AsTool`, `RunAll`, and the internal
`FlowTool`, `LedgerTool`, and `MemoryTool`. The system scenarios
below land with it.

## System scenarios

These scenarios exercise the whole system over the shipped
`subagent` package. The first five have landed:

- `subagent_parallel_test.go` — one orchestrator step's tool fans
  several subagents out through `RunAll`. A panel wave cannot do
  this: waves never reach the ack chain where tools run.
- `subagent_tools_test.go` — one subagent's registry holds
  `FlowTool`, `LedgerTool`, and `MemoryTool`. It runs a child flow,
  completes ledger work, and stores a memory ref the orchestrator
  reads back.
- `subagent_observe_test.go` — the orchestrator's bus receives the
  spawned run's delivered, acked, and verified events live.
- `subagent_depth_test.go` — a self-spawning chain stops at the
  depth bound with `ErrMaxDepth`.
- `subagent_messaging_test.go` — the orchestrator and a human both
  send into the subagent's mailbox, the subagent drains both and
  replies into the orchestrator's, and the orchestrator admits the
  subagent's signer into a room on the way.
- `sqlite_ceremony_test.go` — behind the `ledger_sqlite` tag, the
  full taskrun ceremony around one agentrun pipeline over a SQLite
  file in a temp dir, closed and reopened from the same path: the
  completed record, the replay sentinel, and the blocked dependent
  all survive the reopen. `verify` runs this through its
  `verify-ledger-sqlite` tier.

Still on the backlog:

- `subagent_remote_test.go` — a remote subagent composed from
  `a2aack` behind `AsTool`, then a `dispatch`-backed variant; the
  orchestrator step completes over the real transport.

## mivia-agent parity scenarios

The mivia-agent repo drives real delivery workflows over its own
engine. These scenarios prove this SDK's composition layer can carry
the same shapes. Four files landed:

- `bugfix_flow_test.go` — mirrors `bug-fix.toml`'s hunt and triage. A
  bounded refinement loop re-enters the hunt on an
  insufficient-evidence verdict. A gate step routes on the triage
  verdict across confirmed, no_bug, and refine outcomes. The second
  test mirrors the evidence gates: a failed gate routes to its
  repair, and the loop re-enters the gate until it passes.
- `panel_review_test.go` — mirrors the `agent_panel` step with
  `allow_partial`. Three reviewer subagents run through `RunAll`
  inside one step's tool. One member fails, synthesis proceeds over
  the survivors, and the gate approves. Every member failing fails
  the run.
- `delivery_repair_test.go` — mirrors the delivery contract. A
  rejected title routes to the metadata repair, the loop retries
  delivery, and the second attempt opens. A stubborn host drains a
  one-repair budget and settles the run terminal.
- `feature_delivery_test.go` — mirrors `feature-delivery.toml`'s
  review loop and merge policy. The review loop reworks once, the
  evidence step runs, and delivery escalates to a human over the
  channel transport.

Mapping, workflow-engine shape to SDK block:

- Agent step — `flow.Step` with a tool in the ack chain.
- Gate output matching — `Route` reading the gate's recorded result.
- Bounded loop with `max_iterations` — `Loop` on a parent step over
  the repeated child; the guard reads live state.
- `on_failure` re-entry — `Retry` on Fire, or the repair Route.
- Panel with `allow_partial` — one step's tool fanning members out
  through `subagent.RunAll` and joining the verdicts.
- Context bindings with `max_bytes` — `PayloadFrom` chaining plus
  `contextbudget`'s per-call check.
- `delivery.on_pr_metadata_failure` — the delivery repair loop.
- `merge_policy = "approve"` — `Ask` over a channel transport.

Disclosed limits these scenarios pin:

- An ack rejection stays fatal. A gate that must route to repair
  reports failure as output, never as a tool error.
- Engine restart through checkpoint resume is not reachable through
  `agentrun`; only `flow.Run` exposes the checkpoint hook today.

Three limits the scenarios surfaced are already fixed. Route
exclusion now propagates by default: the admission zero value is
strict, and the parity scenarios carry no defensive `When` clauses.
The scenarios dropped them in the same change. A step repeated
inside a loop overwrites its bare-ID artifact with the latest
result, and every run appends to `Artifacts.History`, so a repair
loop can read the earlier rejections it is repairing, the sibling
repo's `prior_findings` pattern. A loop child that ends every
iteration on one status now re-enters without a self-row: the
parent's standing already matches the child final, so the re-entry
fires no transition. The delivery budget case runs three same-final
repair cycles before its terminal failure.

## Wiring scenarios

The four newest blocks compose at the top through these scenarios:

- `wiring_scenario_test.go` — one traced orchestrator run whose hook
  observes its step, whose step spawns a traced subagent, whose
  internal registry tool falls back to a usage-wrapped provider. The
  span tree nests four levels deep, the hooks fire in order, and the
  session total answers.
- `wiring_edge_test.go` — the failure and edge cases: a pre-tool
  veto kills the run before the spawn, an exhausted registry order
  surfaces `ErrAllFailed` at the top, two spawns keep isolated
  session totals over one accumulator, and a stop-hook veto fails
  the run after the walk while still reporting the final status.

## Disclosed limits: unrecovered panics inside a goroutine wave

`flow`'s panel wave (`flow/wave.go:50`) and `subagent.RunAll`
(`subagent/runall.go:37`) each spawn one goroutine per member and join
on a `sync.WaitGroup`. Neither goroutine carries a deferred `recover`.
A panic inside a panel member's `Fire`, or inside a spawned subagent's
`Runner.Run`, is not converted to a joined error the way a returned
Fire error already is: it terminates the whole process, taking every
sibling goroutine and the parent test binary down with it. This is a
confirmed production gap, found by reading both files; it is not
covered by `faults_panic_test.go`, which deliberately stays on the
single-goroutine, non-panel path, since a test that actually crashes
the process asserts nothing and reports as a broken test run rather
than a clean failure.

Closing this gap needs production code: a `defer recover()` in each
spawned goroutine that converts a caught panic into an error joined
with (or returned alongside) a sibling's Fire failure, mirroring the
existing per-member error path. That change is out of scope for this
test-only slice; it is flagged here for the plan-reviewer to decide
whether it becomes its own reviewed change to `flow` and `subagent`.

## Tests

The scenarios are the tests. They live in `e2e/e2e_test/`, one
external package. The harness itself is production code and holds
the coverage floor like any package.

Named suites for property pairs live one level down, inside each
owning package's own test directory, not here: apply a
transformation to a valid input, assert the outcome. Name each suite
function `TestMetamorphic*`, table-driven, one case per row. See
`envelope/metamorphic_test.go`, `ledger/ledger_test/metamorphic_test.go`,
`subagent/subagent_test/metamorphic_test.go`, and
`memory/memory_test/metamorphic_test.go` for the shipped examples,
alongside the round-trip precedents in `a2a/mapping_test.go`,
`agent/agent_test/exchange_integration_test.go`, and
`identity/sign_integration_test.go`.

## Verification

- `policy/layers.json` gains the `"e2e"` row listed under API.
- `make api-update` lands `api/e2e.txt` in the same change.
- `make verify` passes; e2e and the module total hold the 85 floor.
- `go test -race ./e2e/...` passes, including `faults_panic_test.go`.
- `faults_panic_test.go` asserts through a deferred `recover` around
  its own call to `Run`, never lets a panic escape the test binary,
  and the module's other suites stay unaffected: no shared state
  crosses from the panicking goroutine, since it is the same goroutine
  as the test.
- Every scenario fails when its wiring breaks. Prove it once per
  drop: plant one wiring fault in a throwaway copy, run the suite,
  name the scenario that caught it.
- `AGENTS.md` gains the layout bullet; `docs/README.md` and
  `docs/packages/e2e.md` index the package.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
