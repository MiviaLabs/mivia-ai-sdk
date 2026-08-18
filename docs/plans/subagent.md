# Plan: subagent

## Goal

The subagent package exposes the SDK's blocks as tools. One built
runner becomes a spawnable subagent tool, several run in parallel,
and flows, ledgers, memory, rooms, schedulers, heartbeats, model
turns, human questions, capability cards, triggers, and mailboxes
become optional internal tools. An orchestrator composes all of it
from one registry.

## Scope

Inside:

- `AsTool`: wrap one built `agentrun.Runner` as a `tools.Tool`. Each
  call drives one full run on a fresh thread. The result is the
  named artifact, or the final status.
- `RunAll`: run prepared runners concurrently and join in spec
  order. One member's error never cancels its siblings.
- A ctx-carried spawn-depth guard, `ErrMaxDepth`, defaulting to
  three and configurable per tool.
- Event forwarding: a caller-supplied bus receives the spawned run's
  agent events.
- Internal tools over JSON command payloads: `RoomTool`, `SchedulerTool`,
  `HeartbeatTool`, `LedgerTool`, `MemoryTool`, and `DiscoveryTool`.
  `LedgerTool.OpRun` wraps the full taskrun ceremony; a blocked or
  replayed key fails with the ceremony's own sentinel.
- Internal tools over direct string payloads: `FlowTool` runs a flow
  plan and reports the final status; `ProviderTool` runs one turn
  through a caller-supplied Completer; `TriggerTool` fires a named
  trigger; `ChannelTool` asks a human through a Notifier.
- The message plane: `Mailbox` holds signed messages for one
  recipient; `SendTool` signs with a caller identity and delivers;
  `InboxTool` drains payloads as JSON. Any sender - orchestrator,
  sibling subagent, or human wiring - uses the same surface.

Outside:

- Any scheduler of its own. Parallelism is `RunAll`; flow panels
  cannot drive tools, because waves never reach the ack chain.
- Any new trust boundary. A subagent tool runs in-process; remote
  boundaries stay with `a2aack` and `dispatch`.
- Model calls of the SDK's own. `ProviderTool` wraps a caller's
  Completer; no concrete client ships.
- Room message transport. A room holds membership; `RoomTool`
  admits a subagent's signer. Message delivery stays with the
  mailbox in-process and `dispatch` over HTTP.

## API

```go
func AsTool(name string, r *agentrun.Runner, opts ToolOptions) tools.Tool
type Spec struct{ Name string; Runner *agentrun.Runner; In machine.InOut }
type Result struct{ Name string; Status machine.Status; Err error }
func RunAll(ctx context.Context, specs []Spec) []Result
var ErrMaxDepth, ErrBadCommand, ErrMailboxFull

func FlowTool(name string, plan *flow.Definition, m *machine.Definition, bus *events.Bus) tools.Tool
func LedgerTool(name string, l *ledger.Ledger, actor ledger.Actor, lease time.Duration) tools.Tool
func MemoryTool(name string, s *memory.Store) tools.Tool
func RoomTool(name string, r *room.Room, actor string) tools.Tool
func SchedulerTool(name string, s *scheduler.Scheduler, job scheduler.Job) tools.Tool
func HeartbeatTool(name string, m *heartbeat.Monitor) tools.Tool
func DiscoveryTool(name string) tools.Tool
func ProviderTool(name string, c provider.Completer) tools.Tool
func TriggerTool(name string, reg *trigger.Registry) tools.Tool
func ChannelTool(name string, ask channel.Notifier, recipient string) tools.Tool

func NewMailbox(capacity int) (*Mailbox, error)
func (m *Mailbox) Deliver(msg envelope.Message) error
func (m *Mailbox) Take() []envelope.Message
func SendTool(name string, box *Mailbox, id *identity.Identity) tools.Tool
func InboxTool(name string, box *Mailbox) tools.Tool
```

`policy/layers.json` grants subagent the
`["agent", "agentrun", "channel", "discovery", "envelope", "events",
"flow", "heartbeat", "identity", "ledger", "machine", "memory",
"provider", "room", "scheduler", "taskrun", "tools", "trigger"]`
edges. The package imports each block only through its public API.

## Tests

Tests live in `subagent/subagent_test/`, one external package:

- `astool_test.go` — status and artifact results, failure
  propagation, repeated spawns on fresh threads.
- `runall_test.go` — proved overlap through start gates, join order,
  and sibling isolation on error.
- `depth_test.go` — the self-spawn bound and its configuration.
- `internal_tools_test.go` — the flow, ledger, and memory tools
  over real blocks.
- `commandtools_test.go` — room, scheduler, heartbeat, and
  discovery commands.
- `directtools_test.go` — provider, channel, and trigger calls.
- `toolerrors_test.go` — every command tool's bad-command sentinel
  and each tool's failure propagation.
- `mailbox_test.go` — the mailbox contract and both message tools.
- `observe_test.go` — event forwarding onto a parent bus.

## Verification

- `make verify` passes; subagent and the module total hold the 85
  floor.
- `go test -race ./subagent/...` passes.
- `make api-update` lands `api/subagent.txt` in the same change.
- The e2e system scenarios in `docs/plans/e2e.md` drive the package
  end to end.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
