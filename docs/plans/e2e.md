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

The harness imports `agent`, `discovery`, `envelope`, `events`,
`flow`, `identity`, and `tools`; `policy/layers.json` gains exactly
that row. Scenario test files import anything they need; the deps
gate exempts test files, so new scenarios never churn the policy.

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
  survive, step two never acks.
- Stale lease takeover around real work: the fenced loser's
  `Complete` returns `ErrFenced`; the taker's lands.
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

- Route exclusion propagates only through `AdmissionOnSucceeded`.
  The default admission lets a step run after a skipped need.
- An ack rejection stays fatal. A gate that must route to repair
  reports failure as output, never as a tool error.
- Engine restart through checkpoint resume is not reachable through
  `agentrun`; only `flow.Run` exposes the checkpoint hook today.

Two limits the scenarios surfaced are already fixed. A step repeated
inside a loop overwrites its bare-ID artifact with the latest
result, and every run appends to `Artifacts.History`, so a repair
loop can read the earlier rejections it is repairing, the sibling
repo's `prior_findings` pattern. A loop child that ends every
iteration on one status now re-enters without a self-row: the
parent's standing already matches the child final, so the re-entry
fires no transition. The delivery budget case runs three same-final
repair cycles before its terminal failure.

## Tests

The scenarios are the tests. They live in `e2e/e2e_test/`, one
external package. The harness itself is production code and holds
the coverage floor like any package.

## Verification

- `policy/layers.json` gains the `"e2e"` row listed under API.
- `make api-update` lands `api/e2e.txt` in the same change.
- `make verify` passes; e2e and the module total hold the 85 floor.
- `go test -race ./e2e/...` passes.
- Every scenario fails when its wiring breaks. Prove it once per
  drop: plant one wiring fault in a throwaway copy, run the suite,
  name the scenario that caught it.
- `AGENTS.md` gains the layout bullet; `docs/README.md` and
  `docs/packages/e2e.md` index the package.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
