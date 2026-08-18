# Phase 55: subagent kit

Status: shipped. The package plan lives at docs/plans/subagent.md;
no standalone deviation from this contract remains.

## Why this phase exists

Nothing exposes an agent or a runner as a `tools.Tool`. The only
tool producer in the SDK is `mcp.Client`. An orchestrator cannot
place a subordinate agent in its own registry, run several at once,
or hand one SDK block to another as a callable capability.

Three near-misses exist today. `flow.Sub` nests an in-process child
graph inside one run, with no own identity and no callable surface.
`a2aack` adapts one remote A2A task into one step's ack. `dispatch`
receives envelope lines and answers with restatements. None lets an
agent spawn, observe, and join subordinate agents as tools.

## Goal

One package, `subagent`: a runner becomes a tool, several run in
parallel, and the SDK's own blocks become optional internal tools a
subordinate agent can call.

## Scope

Inside:

- `AsTool`: wrap one built `agentrun.Runner` as a `tools.Tool`. The
  tool's Run drives one full runner run on a fresh thread. The input
  string seeds the step payload path; the result is a string: the
  named artifact, or the final status when no artifact is named.
- `RunAll`: run N prepared runners concurrently and join. A flow
  panel of subagent tools covers the flow-driven path; `RunAll`
  covers callers without a flow.
- Optional internal tools, each a plain `tools.Tool` the caller
  registers: `FlowTool` runs a `flow.Definition` against a machine
  and reports the final status; `LedgerTool` records one completed
  task through the full taskrun ceremony and reports key state;
  `MemoryTool` puts and gets blobs through a bound `memory.Store`.
- Further internal tools over the SDK's own blocks: `RoomTool`
  manages room membership, `SchedulerTool` schedules one bound job,
  `HeartbeatTool` reports liveness, `DiscoveryTool` matches one
  capability card, `ProviderTool` runs one caller-supplied model
  turn, `TriggerTool` fires a named trigger, and `ChannelTool` asks
  a human through a Notifier.
- The message plane: a bounded `Mailbox` of signed messages,
  `SendTool` signing with a caller identity, and `InboxTool`
  draining payloads. Orchestrators, sibling subagents, and human
  wiring all use the same surface, in both directions.
- Observation: every spawned run forwards its events onto a
  caller-supplied bus, so a parent observes subordinate runs live.
- A spawn-depth guard: the kit carries a depth counter in ctx;
  `AsTool` refuses to spawn past the bound with `ErrMaxDepth`. The
  default is three; a caller raises or lowers it per tool.
- A composition walkthrough: a remote subagent built from
  `a2aack.Wait` behind `AsTool`, and a `dispatch`-backed one.

Outside:

- Any scheduler of its own. Parallelism is `RunAll`; no new timers,
  pools, or queues. A flow panel cannot drive tools, because waves
  never reach the ack chain.
- Any new trust boundary. A subagent tool runs in-process under the
  parent's process; remote boundaries stay with `a2aack` and
  `dispatch`.
- Any model calls. `provider.Completer` stays caller-wired.
- Discovery-driven routing. `discovery.Match` to a transport choice
  waits for a caller that needs it.

## API

```go
package subagent

// ToolOptions tunes one subagent tool.
type ToolOptions struct {
	Artifact string      // artifact name returned; empty means status
	Depth    int         // max spawn depth; 0 means the default 3
	Bus      *events.Bus // receives the spawned run's events
}

func AsTool(name string, r *agentrun.Runner, opts ToolOptions) tools.Tool

// Spec names one runner and its input record for RunAll.
type Spec struct {
	Name   string
	Runner *agentrun.Runner
	In     machine.InOut
}

// Result reports one RunAll member's outcome.
type Result struct {
	Name   string
	Status machine.Status
	Err    error
}

// RunAll runs every spec concurrently and joins, in spec order.
func RunAll(ctx context.Context, specs []Spec) []Result

func FlowTool(name string, plan *flow.Definition,
	m *machine.Definition, bus *events.Bus) tools.Tool
func LedgerTool(name string, l *ledger.Ledger,
	actor ledger.Actor, lease time.Duration) tools.Tool
func MemoryTool(name string, s *memory.Store) tools.Tool

var ErrMaxDepth = errors.New("subagent: max spawn depth reached")
```

`policy/layers.json` gains
`"subagent": ["agentrun", "events", "flow", "ledger", "machine", "memory", "tools"]`.

## Input and result contracts

A subagent tool takes a string payload, so the agentrun ack chain
can drive it like any step tool. It returns a string: the named
artifact, or the final status. Richer results cross through
`memory.Store` refs and the parent's `Artifacts` bag, both already
in the composition. A repeated spawn on one parent thread mints the
agent's `#N` message suffix, so loops over subagents stay valid.

## Tests

`subagent/subagent_test/`, one external package:

- `astool_test.go` — one orchestrator step runs one subagent; the
  artifact returns; the parent thread verifies; a failing subagent
  fails the step with its own error.
- `runall_test.go` — three specs run concurrently; results land in
  spec order; one member's error does not cancel the others.
- `depth_test.go` — a self-spawning tool stops at the bound with
  `ErrMaxDepth`.
- `internal_tools_test.go` — `FlowTool` reports a child plan's
  final status and outcomes; `LedgerTool` admits and completes, and
  a blocked dependent reports `ErrTaskBlocked`; `MemoryTool` puts
  and gets through `envelope.ContextRef`.
- `observe_test.go` — the parent's bus receives the spawned run's
  delivered, acked, and verified events.

## Verification

- `make verify` passes; `subagent` and the module total hold the 85
  floor.
- `go test -race ./subagent/...` passes; `RunAll` members share no
  state without a lock.
- `make api-update` lands `api/subagent.txt` in the same change.
- `docs/plans/subagent.md`, `docs/packages/subagent.md`, and one
  walkthrough ship with the package.
- The e2e scenarios named in `docs/plans/e2e.md`'s system section
  land with or after this phase.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
