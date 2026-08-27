# Package reference: runconfig

The runconfig package loads a JSON document into a validated
`agentrun` runner and its tool set. A deployment defines a runner as
data, without recompiling. `Load` feeds `flow.New`, `machine.New`, and
`agentrun.New`; it never re-runs their validation logic itself. The
exported surface below mirrors `api/runconfig.txt`. The full document
grammar lives in `docs/plans/runconfig.md`.

## Types

- `Definition` — the resolved document: `Plan *flow.Definition`,
  `Machine *machine.Definition`, `Options agentrun.Options`,
  `Tools []string` (the document's declared external tool names),
  `Bindings []Binding` (one per bound step, in plan order),
  `Blocks *Blocks` (caller-set internal tool sources), and
  `External *tools.Registry` (caller-set external tools by name).
  `Load` fills every field except `Blocks` and `External`, which the
  caller sets before calling `Runner`.
- `Binding` — ties one step to one tool source: `Step` (the bound step
  ID), `Tool` (the external tool name, set only when `Internal` is
  false), `Kind` (the internal family, set only when `Internal` is
  true), and `Internal` (separates the two cases).
- `Blocks` — holds one `tools.Tool` per internal `Kind`. Safe for
  concurrent use. Created only through `NewBlocks`.
- `Kind` — a string naming one subagent internal tool family.

## Constants

`Kind`'s twelve values name the subagent internal tool families a
document's `internal` step field may reference:

- `FlowKind` (`"flow"`) — a `FlowTool`, running a flow plan.
- `LedgerKind` (`"ledger"`) — a `LedgerTool`, recording one completed
  task through the taskrun ceremony.
- `MemoryKind` (`"memory"`) — a `MemoryTool`, storing and fetching
  content-addressed blobs.
- `RoomKind` (`"room"`) — a `RoomTool`, admitting and querying room
  membership.
- `SchedulerKind` (`"scheduler"`) — a `SchedulerTool`, scheduling and
  canceling a bound job.
- `HeartbeatKind` (`"heartbeat"`) — a `HeartbeatTool`, beating and
  checking liveness against a monitor.
- `DiscoveryKind` (`"discovery"`) — a `DiscoveryTool`, matching a
  capability card against a need.
- `TriggerKind` (`"trigger"`) — a `TriggerTool`, firing a named
  trigger.
- `ChannelKind` (`"channel"`) — a `ChannelTool`, asking a human through
  a `Notifier`.
- `ProviderKind` (`"provider"`) — a `ProviderTool`, running one model
  turn through a caller's `Completer`.
- `ProviderRegistryKind` (`"providerregistry"`) — a provider-registry
  backed tool, routing through ordered fallback.
- `AsToolKind` (`"astool"`) — a subagent exposed as a callable tool.

## Functions and methods

- `Load(data)` — resolves one JSON document into a `*Definition`.
  Rejects malformed JSON, a non-object root, a step with both `tool`
  and `internal`, a step that sets `sub` beside `tool` or `internal`,
  an empty step ID, an undeclared external tool, a blank or duplicate
  tool name, an unknown internal kind, and an unknown `when` value,
  each wrapped in `ErrBadDocument`. Also wraps in `ErrBadDocument` any
  rejection from `machine.New`, `flow.New`, or a nested `flow.New`
  call. A present `options.budget` maps onto `Options.Budget` as a
  `*contextbudget.Limits` with no range check; `Runner`'s call into
  `agentrun.New` rejects a negative field. `Load` never reads the
  environment.
- `NewBlocks()` — returns an empty `*Blocks`.
- `Blocks.Set(kind, t)` — registers `t` under `kind`, replacing any
  earlier tool set for that `Kind`.
- `Definition.Runner()` — builds a validated `*agentrun.Runner`. The
  caller must first set `Options.Agent`, register the document's
  external tools on `External`, and set every bound internal `Kind` on
  `Blocks`. `Runner` resolves each binding, builds one `tools.Registry`
  keyed by step ID, sets `Options.Machine` and `Options.Tools`, and
  passes `Options` to `agentrun.New`. Returns `agentrun.ErrNoAgent` for
  a nil `Options.Agent`, `ErrUnknownTool` for a missing external tool,
  and `ErrUnknownInternal` for a missing internal `Kind`.

## Failure modes

Use `errors.Is` to test these.

- `ErrBadDocument` ("runconfig: bad document") — `Load` returns it for
  every document-shape rejection listed under `Load` above: malformed
  JSON, a missing `machine` or `plan` section, a step with both
  bindings, a step with `sub` beside a binding, an empty step ID, an
  undeclared or duplicate or blank tool name, an unknown internal kind,
  an unknown `when` value, a bad retry duration string, or a
  constructor rejection from `machine.New` or `flow.New`. `Runner`
  also wraps it when `tools.Registry.Add` rejects a resolved step
  adapter, for example on a duplicate step ID.
- `ErrUnknownInternal` ("runconfig: unknown internal tool") —
  `Runner` returns it, through `resolve` in `runconfig/runner.go`, when
  a binding names a `Kind` absent from `Blocks`.
- `ErrUnknownTool` ("runconfig: unknown tool") — `Runner` returns it,
  through `resolve` in `runconfig/runner.go`, when a binding names an
  external tool absent from `External`.

## Document shape

One JSON document holds four top-level sections: `machine`, `plan`,
`options`, and `tools`. `machine` holds `initial` and a `transitions`
array of `{from, to, trigger}` rows. `plan` holds a `steps` array and a
`panels` array of step-ID arrays. Each step in `plan.steps` sets `id`,
and optionally `needs`, `to`, `when`, `payload`, `retry`, `loop`, and
exactly one of `tool`, `internal`, or `sub`. `options` maps `room`,
`ask_to`, an optional `budget` object, and an optional `trace` boolean
onto `Options`. `tools` lists the external tool names a step's `tool`
field may reference. `Load`'s full field-by-field mapping, including
the `retry` and `loop` policy shapes and the `when` enum values, lives
in `docs/plans/runconfig.md`.

## Invariants

- `Load` validates the whole document before it returns a
  `*Definition`; no partially built `Definition` escapes a rejected
  document.
- A step carries a child plan (`sub`) or a tool binding (`tool` or
  `internal`), never both; `buildStep` rejects the combination before
  it recurses into `sub`.
- `Definition.Bindings` lists one entry per bound step across the
  whole plan, including steps nested under `sub`, in the order
  `buildPlan` visits them.
- `Blocks.Set` and `Blocks.get` hold the same mutex, so concurrent
  `Set` calls and a concurrent `Runner` resolution never race.
- `Definition.Runner`'s adapter, built by the unexported `newStepTool`
  in `runconfig/steptool.go`, forwards exactly the optional
  `tools.Tool` capability interfaces (`tools.ProfiledTool`,
  `tools.ResultBudgetTool`, `tools.PrivilegedTool`, `tools.SchemaTool`)
  that the resolved inner tool implements. A caller-set `tools.Scope`
  approval threshold or a privileged tool reads the wrapped tool's true
  published capability, never a stripped default.
- A document naming two internal steps of the same `Kind` shares one
  `Blocks` entry: both steps run the same underlying tool. Distinct
  tools of one kind compose through the external `tools` array
  instead.

## Cross-references

- [agentrun.md](agentrun.md) — `Definition.Runner` builds and returns
  an `*agentrun.Runner`; `Definition.Options` is a plain
  `agentrun.Options` value the caller finishes wiring after `Load`.
- [flow.md](flow.md) and [machine.md](machine.md) — `Load` resolves
  the document's `plan` and `machine` sections through `flow.New` and
  `machine.New`, and returns their own rejections wrapped in
  `ErrBadDocument`.
- [subagent.md](subagent.md) — each `Kind` names one of `subagent`'s
  internal tool families; the caller builds the concrete tool through
  the matching subagent helper and registers it on `Blocks`.
- [tools.md](tools.md) — `Definition.External` is a `*tools.Registry`;
  `Runner` builds another `*tools.Registry` keyed by step ID from the
  resolved bindings.

## Usage

```go
def, err := runconfig.Load(documentBytes)
if err != nil {
    // malformed document, bad binding, or a rejected machine/plan shape
}

def.Options.Agent = myAgent
def.External.Add(myGrepTool)

blocks := runconfig.NewBlocks()
blocks.Set(runconfig.FlowKind, subagent.FlowTool("flow", subPlan, subMachine, bus))
def.Blocks = blocks

runner, err := def.Runner()
if err != nil {
    // nil Agent, an unresolved external tool, or an unresolved internal Kind
}
```
