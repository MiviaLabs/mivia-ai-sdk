# Plan: runconfig

Status: shipped in commit 9054704. The original contract lives in
docs/plans/agents/phase69_options_loader.md. The open work is the
correctness fix at the end of this file.

## Goal

One package loads a JSON document into a validated `agentrun` runner
and its tool set. A deployment defines a runner as data, without
recompiling.

## Scope

Inside:

- A `runconfig` package importing `agentrun`, `flow`, `machine`,
  `subagent`, and `tools`. `policy/layers.json` gains exactly that row.
- `Load(data []byte)` resolving one JSON document into a validated
  `Definition`. The loader never reimplements validation. It feeds
  `flow.New`, `machine.New`, and `agentrun.New`.
- `Definition` holds the resolved machine, plan, options, and tool
  set. Each is exported for inspection.
- `Runner()` builds a `*agentrun.Runner` from the loaded definition.
- Internal tool bindings by name. A step binds one `Kind` from the
  subagent families. The loader wires the named kind onto its step.
- Sentinels `ErrUnknownTool`, `ErrUnknownInternal`, and
  `ErrBadDocument`.

Outside:

- TOML parsing. A caller translates a TOML front end to this JSON form.
- Provider client construction. A provider entry names a registered
  completer. Registration stays code.
- Secrets. The loader never reads the environment. The caller supplies
  the agent identity.
- Mivia workflow semantics. The loader maps what `flow.Step` already
  expresses. Richer semantics stay app-side translations.
- `dispatch` endpoint configuration and HTTP wiring.
- Any `agent`, `identity`, or `events` value. `runconfig` cannot name
  those types. The caller composes them through `Definition.Options`.

## API

The document shape is one JSON object with four sections.

```json
{
  "machine": {
    "initial": "idle",
    "transitions": [
      {"from": "idle", "to": "done", "trigger": "next"}
    ]
  },
  "plan": {
    "panels": [["a", "b"]],
    "steps": [
      {
        "id": "a",
        "needs": [],
        "to": "done",
        "when": "on_finished",
        "payload": "hello",
        "tool": "grep"
      },
      {
        "id": "b",
        "needs": ["a"],
        "to": "done",
        "internal": "flow"
      }
    ]
  },
  "options": {
    "room": "platform-team",
    "ask_to": "human-1",
    "budget": {"max_bytes": 200000, "max_events": 500}
  },
  "tools": ["grep"]
}
```

- `machine` holds `initial` and `transitions`. Each transition row is
  `{from, to, trigger}`. Guards, on-entry, and on-exit are function
  code. They stay out of the document.
- `plan.steps` holds one object per step. The step `id` is required.
  Unknown JSON fields are ignored, matching `envelope.Decode`.
- `plan.panels` holds parallel waves. Each panel is an array of step
  IDs.
- `options` maps `room` to `Options.Room`, `ask_to` to `Options.AskTo`,
  and a present `budget` object (`max_bytes`, `max_events`) to
  `Options.Budget` as a `*contextbudget.Limits`. An absent `budget`
  leaves `Options.Budget` nil. `Load` performs no range check on
  `budget`'s two integers; `contextbudget.Limits.Validate`, called
  inside `agentrun.New` during `Runner`, rejects a negative value.
- `tools` lists the external tool names a step binding may reference.

Each step carries `tool` or `internal`, never both. A step carries
neither exactly when it runs inside a two-plus-member panel and needs
no tool. The `tool` field names an external tool. The `internal` field
names a subagent `Kind`.

Step field mapping:

- `needs` maps to `Step.Needs`. `when` maps to `Step.When`.
- `to` maps to `Step.To`. `payload` maps to `Step.Payload`.
- `retry` maps to a `flow.RetryPolicy`. `loop` maps to a
  `flow.LoopPolicy`.
- `sub` maps to a nested `flow.Definition`. The loader recurses. A
  step that sets `sub` together with `tool` or `internal` is
  `ErrBadDocument`. A step carries a child plan or a binding, never
  both.
- `panels` maps to `[]flow.Panel`. The loader passes them to `flow.New`.
- No `route` field. A `Route` is function code. Branch steps stay
  caller-composed.

Enum string mapping:

- `when` values: `on_succeeded`, `on_finished`, `on_failed`. They map
  to `AdmissionOnSucceeded`, `AdmissionOnFinished`, and
  `AdmissionOnFailed`. An unknown value is `ErrBadDocument`.
- `retry` fields `max_attempts`, `base_delay`, and `max_delay` map to
  the policy fields. The document never expresses a retryable
  predicate, jitter, or sleep func.
- `loop` field `max` maps to `LoopPolicy.Max`. A loop guard is function
  code. The document never expresses one.

`machine` maps onto `machine.New`. `plan` maps onto `flow.New`. The
loader returns `ErrBadDocument` when a typed constructor rejects the
resolved form. It never re-runs the validation logic itself.

Exported surface, landing in `api/runconfig.txt` via `make api-update`:

```go
func Load(data []byte) (*Definition, error)

type Definition struct {
    Plan     *flow.Definition     // resolved by Load
    Machine  *machine.Definition  // resolved by Load
    Options  agentrun.Options     // Agent caller-set; Tools filled by Runner
    Tools    []string             // the document's external tool names
    Bindings []Binding            // one per step, in plan order
    Blocks   *Blocks              // caller-set internal tool sources
    External *tools.Registry      // caller-set external tools by name
}

func (d *Definition) Runner() (*agentrun.Runner, error)

type Binding struct {
    Step     string // the step ID the tool binds
    Tool     string // settable only for an external binding
    Kind     Kind   // settable only for an internal binding
    Internal bool   // true for an internal kind
}

type Kind string

const (
    FlowKind             Kind = "flow"
    LedgerKind           Kind = "ledger"
    MemoryKind           Kind = "memory"
    RoomKind             Kind = "room"
    SchedulerKind        Kind = "scheduler"
    HeartbeatKind        Kind = "heartbeat"
    DiscoveryKind        Kind = "discovery"
    TriggerKind          Kind = "trigger"
    ChannelKind          Kind = "channel"
    ProviderKind         Kind = "provider"
    ProviderRegistryKind Kind = "providerregistry"
    WorkspaceReadKind    Kind = "workspaceread"
    WorkspaceWriteKind   Kind = "workspacewrite"
    WorkspaceListKind    Kind = "workspacelist"
    WorkspaceStatKind    Kind = "workspacestat"
    DiffKind             Kind = "diff"
    AsToolKind           Kind = "astool"
)

func NewBlocks() *Blocks
func (b *Blocks) Set(kind Kind, t tools.Tool)

var ErrUnknownTool, ErrUnknownInternal, ErrBadDocument error
```

`Definition.Options` carries the agent source. `runconfig` cannot name
`*agent.Agent`. The caller sets `Options.Agent` after `Load`. `Runner`
returns `agentrun.ErrNoAgent` when `Options.Agent` is nil.

`Blocks` holds one `tools.Tool` per internal `Kind`. The caller Builds
each through the matching `subagent` helper. The `Runner` resolves a
step's internal binding against `Blocks`. It returns `ErrUnknownInternal`
when the requested `Kind` is absent.

`Definition.External` holds the external tools by name. The caller
registers them before `Runner`. The `Runner` resolves a step's external
binding against `External`. It returns `ErrUnknownTool` when the name is
absent.

`Runner` builds one `tools.Registry`. It adds one adapter per bound
step. Each adapter is keyed by the step's ID. The adapter delegates to
the resolved `Kind` tool or external tool. It then sets `Options.Machine`
and `Options.Tools`. It passes `agentrun.New` the caller's `Options`.

A document with two internal tools of one `Kind` shares one `Blocks`
entry. Two such steps run the same tool. Distinct tools of one kind
compose through the external set instead.

`Load` returns `ErrBadDocument` for these shapes:

- Malformed JSON, or a non-object root.
- A step with both `tool` and `internal`, or an empty step `id`.
- An external `tool` name absent from the `tools` array.
- A blank or duplicate name in the `tools` array.
- An `internal` kind that no `Kind` constant names.
- An unknown `when` value.
- Any rejection from `machine.New`, `flow.New`, or a nested `flow.New`.

The tool, the duplicates, and the malformed shape are document errors.
Object resolution happens in `Runner`, not in `Load`.

## Tests

Tests live in `runconfig/runconfig_test/`, one external package. The
following files exist.

- `load_test.go` — table-driven over the document grammar. Every step
  field, panel, machine row, and option round-trips into the built
  types.
- `reject_test.go` — table-driven over every `ErrBadDocument` case:
  malformed JSON, both bindings, blank `id`, undeclared tool, unknown
  kind, unknown `when`, duplicate tool, `sub` with `tool`, `sub` with
  `internal`, and constructor rejections.
- `runner_test.go` — `Runner` returns `ErrUnknownTool` for a missing
  external tool and `ErrUnknownInternal` for a missing `Kind`. A nil
  `Options.Agent` yields `agentrun.ErrNoAgent`. A bad budget forwards
  its wrapped error.
- `load_integration_test.go` — one golden document loads. A concrete
  agent runs to completion. The test supplies the identity, the card,
  and a no-op `AckWait` resolver.

The golden document uses one external tool and one `flow` internal
tool. The subagent wiring proves a document naming a subagent trace
builds a runnable resolver.

The integration test file ports phase 69's e2e requirement. It lives in
`runconfig/runconfig_test/`. It matches the `agentrun` integration test
layout in `PHASES.md`.

File names bound the concern, not the phase. No test file carries the
word `phase`.

## Verification

- `policy/layers.json` grants runconfig the
  `["agentrun", "contextbudget", "flow", "machine", "subagent", "tools"]`
  row.
- `make verify` passes; runconfig and the module total hold the 85
  floor.
- `go test -race ./runconfig/...` passes.
- `make api-update` lands `api/runconfig.txt` in the same change. It
  locks the surface above.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass. The gates inspect Go package directories. No code ships here,
  so both pass with the plan and the row alone.

## Correctness fix: `sub` beside a binding

`buildStep` in `runconfig/loader.go` returns from its `Sub` branch
before the `Tool` and `Internal` branches run. A step that sets both
`sub` and `tool` loads, and the binding is dropped without a word. An
undeclared tool name passes too. That contradicts `Load`'s own doc
comment at `runconfig/loader.go:95-100`, which promises to reject an
undeclared external tool. The same comment says "a step with both
bindings". `sub` is a child plan, not a binding, so the comment does
not cover this case today.

The fix, in `buildStep`, beside the existing tool-plus-internal check:

- Reject a step that sets `Sub` together with `Tool` or `Internal`.
- Return `ErrBadDocument` wrapped with the step id, in the message
  form the neighbouring check already uses.
- Place the new check with the other early field checks, before the
  `Sub` recursion runs.

Scope of the fix:

- No exported symbol changes. `api/runconfig.txt` stays as locked.
- No import edge changes. The `runconfig` row in `policy/layers.json`
  stays `["agentrun", "flow", "machine", "subagent", "tools"]`.
- `Load`'s doc comment gains the new case in its enumerated rejection
  list, in the same change. Name it as a step that sets `sub` beside
  `tool` or `internal`.

Tests, in `runconfig/runconfig_test/reject_test.go`:

- A document whose step sets `sub` and `tool`, with the tool declared.
  `Load` must return an error matching `ErrBadDocument`, and the
  message must name the step id.
- A document whose step sets `sub` and an undeclared `tool`. Same
  assertion. This case is the one that passed before the fix, so it
  kills the mutation that deletes the new check.
- A document whose step sets `sub` and a valid `internal` kind. Same
  assertion.
- One positive control: a step with `sub` alone still loads, and its
  child bindings still reach `Definition.Bindings`.

Verification:

- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  `python3 scripts/check_prose.py`, and `python3 scripts/check_api.py`
  pass.
- `make verify` passes. `runconfig` holds the 85 coverage floor.
- `go test -race ./runconfig/...` passes.
- `python3 scripts/check_docs.py` passes over the reworded `Load`
  comment.
- `docs/plans/agentloop.md` and the `policy/layers.json` row adding
  `schema` to `agentloop` stay out of this commit. They belong to the
  concurrent `agentloop` change and need their own plan review.
