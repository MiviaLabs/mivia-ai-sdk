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
  `Blocks *Blocks` (the internal tool sources), and
  `External *tools.Registry` (caller-set external tools by name).
  `Load` fills `Blocks` for the document's `internal` section and
  leaves the remaining Kinds to the caller. `Load` fills every field
  except `External`, which the caller sets before calling `Runner`.
- `Binding` — ties one step to one tool source: `Step` (the bound step
  ID), `Tool` (the external tool name, set only when `Internal` is
  false), `Kind` (the internal family, set only when `Internal` is
  true), and `Internal` (separates the two cases).
- `Blocks` — holds one `tools.Tool` per internal `Kind`. `Load`
  fills the six wireable Kinds from the document's `internal`
  section. The caller sets the caller-built Kinds, or overrides a
  wireable one, through `Set`. Last write wins. Safe for concurrent
  use. Created only through `NewBlocks`.
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

The document builds six of the twelve at `Load` through its
`internal` section: `discovery`, `flow`, `heartbeat`, `ledger`,
`memory`, and `room`. The other six stay caller-built because their
constructors take live values no JSON document encodes: `astool` a
runner, `channel` a `Notifier`, `provider` a `Completer`,
`providerregistry` a registry, `scheduler` a job, and `trigger` a
registry of conditions. A document declaring a caller-built Kind
fails `Load` with `ErrBadDocument` wrapping `ErrCallerBuilt`.

## Functions and methods

- `Load(data)` — resolves one JSON document into a `*Definition`.
  Rejects malformed JSON, a non-object root, a step with both `tool`
  and `internal`, a step that sets `sub` beside `tool` or `internal`,
  a step with no `tool`, `internal`, or `sub` binding outside a
  two-or-more-member panel, an empty step ID, an undeclared external
  tool, a blank or duplicate tool name, an unknown internal kind, and
  an unknown `when` value, each wrapped in `ErrBadDocument`. Also
  rejects an `internal` section key no `Kind` constant names, a key
  naming a caller-built `Kind` (wrapping `ErrCallerBuilt`), and an
  invalid internal config value. Also wraps in `ErrBadDocument` any
  rejection from `machine.New`, `flow.New`, or an internal builder. A
  present `options.budget` maps onto `Options.Budget` as a
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
  bindings, a step with `sub` beside a binding, a step with no `tool`,
  `internal`, or `sub` binding outside a two-or-more-member panel, an
  empty step ID, an undeclared or duplicate or blank tool name, an
  unknown internal kind, an unknown `when` value, a bad retry duration
  string, a rejected `internal` section key or config, or a
  constructor rejection from `machine.New`, `flow.New`, or an internal
  builder. `Runner` also wraps it when `tools.Registry.Add` rejects a
  resolved step adapter, for example on a duplicate step ID.
- `ErrCallerBuilt` ("runconfig: internal kind stays caller-built") —
  `Load` returns it, wrapped inside `ErrBadDocument` with two `%w`
  verbs, when the document's `internal` section names one of the six
  caller-built Kinds. Test with `errors.Is` against both sentinels.
- `ErrUnknownInternal` ("runconfig: unknown internal tool") —
  `Runner` returns it, through `resolve` in `runconfig/runner.go`, when
  a binding names a `Kind` absent from `Blocks`.
- `ErrUnknownTool` ("runconfig: unknown tool") — `Runner` returns it,
  through `resolve` in `runconfig/runner.go`, when a binding names an
  external tool absent from `External`.

## Document shape

One JSON document holds five top-level sections: `machine`, `plan`,
`options`, `tools`, and `internal`. `machine` holds `initial` and a
`transitions`
array of `{from, to, trigger}` rows. `plan` holds a `steps` array and a
`panels` array of step-ID arrays. Each step in `plan.steps` sets `id`,
and optionally `needs`, `to`, `when`, `payload`, `retry`, `loop`, and
at most one of `tool`, `internal`, or `sub`. A step carries none of
the three exactly when it is a member of a panel with two or more
members. `options` maps `room`,
`ask_to`, an optional `budget` object, and an optional `trace` boolean
onto `Options`. `tools` lists the external tool names a step's `tool`
field may reference. `internal` maps a wireable `Kind` name to that
Kind's config object, so `Load` builds the tool before `Runner`. Each
config holds plain scalars: `timeout` for `heartbeat`; `max_bytes` for
`memory`; `id`, `founder`, and `actor` for `room`; `actor` and `lease`
for `ledger`. `discovery` and `flow` take no fields, and flow binds
the document's own plan and machine. Unknown config fields are
ignored. A duplicate section key follows `encoding/json` map
semantics, so the last value wins. `Load`'s full field-by-field
mapping, including
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
- `Blocks.Set` after `Load` overrides a document-built tool for that
  `Kind` only. Last write wins. Partial overlap keeps both sources
  serving their own steps.
- The wireable/caller-built partition is complete and pinned by
  `TestKindBuildersPinConstructors` in
  `runconfig/internal_conformance_test.go`. A new `Kind` without a
  decision fails that test.

## Cross-references

- [agentrun.md](agentrun.md) — `Definition.Runner` builds and returns
  an `*agentrun.Runner`; `Definition.Options` is a plain
  `agentrun.Options` value the caller finishes wiring after `Load`.
- [flow.md](flow.md) and [machine.md](machine.md) — `Load` resolves
  the document's `plan` and `machine` sections through `flow.New` and
  `machine.New`, and returns their own rejections wrapped in
  `ErrBadDocument`.
- [subagent.md](subagent.md) — each `Kind` names one of `subagent`'s
  internal tool families. `Load` builds the six wireable Kinds
  through `runconfig/internal.go`. The caller builds the rest
  through the matching subagent helper and registers them on
  `Blocks`.
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

// The document's internal section already built flow. Set only what
// stays caller-built, or override one Kind. Do not assign a fresh
// NewBlocks() over def.Blocks; that discards every document-built
// tool.
def.Blocks.Set(runconfig.SchedulerKind, subagent.SchedulerTool("sched", sched, job))

runner, err := def.Runner()
if err != nil {
    // nil Agent, an unresolved external tool, or an unresolved internal Kind
}
```
