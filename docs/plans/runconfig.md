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

## Addendum: schema-decode and capability forwarding (phase 76)

Phase 76 closed the argument-decode gap phase 72 left open. `agentrun`'s
`chain` decodes a step's payload through `tools.SchemaTool.DecodeArguments`
before it calls the tool, when the resolved tool implements
`tools.SchemaTool`, instead of always passing the raw string. This
fixes argument decode for all five file/diff internal Kinds
(`WorkspaceReadKind`, `WorkspaceWriteKind`, `WorkspaceListKind`,
`WorkspaceStatKind`, `DiffKind`).

Decode success alone does not prove a Kind completes a real
`agentrun.Runner.Run`: `chain` also requires the tool's result,
`tools.Out.Value`, to be a `string`, or the step fails with
`ErrResultNotText`. `WorkspaceReadKind`, `WorkspaceWriteKind`, and
`DiffKind` return a string and are confirmed, by real end-to-end
tests, to complete through `Runner.Run`. `WorkspaceListKind` and
`WorkspaceStatKind` bind tools whose `Run` returns a struct
(`[]subagent.WorkspaceEntry` and `subagent.WorkspaceFileInfo`), not a
string; no test drives either through a real `Runner.Run`, and the
result-type mismatch means they are expected to fail with
`ErrResultNotText` until a follow-up phase resolves it.

`runconfig/runner.go`'s `stepTool` wrapper, the type
`Definition.Runner` puts around every resolved internal-Kind tool,
moved into `runconfig/steptool.go` and gained an unexported
`newStepTool` constructor. `newStepTool` composes exactly the optional
`tools.Tool` capability interfaces (`tools.SchemaTool`,
`tools.ProfiledTool`, `tools.ResultBudgetTool`, `tools.PrivilegedTool`)
the wrapped tool implements, so a caller-set `tools.Scope` approval
threshold or privilege check reads the wrapped tool's true published
capability, not a stripped default. No standalone phase 76 plan file
remains for this contract.

## Correctness fix: declaredTools admits a whitespace-only name

Status: shipped in commit f9b3ace.

### Fix goal

`declaredTools`'s doc comment (`runconfig/loader.go:153`) says it
"validates the tools array: no blank or duplicate name." The code
(`:157`) checks `n == ""` only. A tool name of `" "` (a single space)
is not empty, so it passes both the blank check and, if it appears
once, the duplicate check. A document with a whitespace-only tool
name loads, and any step naming that exact whitespace string as its
`tool` field also resolves, since the lookup at `:246` (`declared[w.Tool]`)
compares the same raw, untrimmed string on both sides. The result is
silent: no error at `Load` time, and no error at `Runner` time, for a
tool name that is almost certainly a padded config mistake.
`tools/registry.go`, `dispatch/options.go`, and `agentloop/options.go`
already reject the same shape by checking `strings.TrimSpace(name) ==
""`; this fix matches that sibling pattern.

### Fix scope

Inside:

- `declaredTools`, in `runconfig/loader.go`, changes its blank check
  from `if n == ""` to `if strings.TrimSpace(n) == ""`. The map key
  and the duplicate check stay on the raw `n`; only the blank test
  trims. A name with meaningful surrounding whitespace, such as
  `" grep"`, is unaffected by this fix and stays a separate scope
  question, not addressed here.
- `runconfig/loader.go` gains a `"strings"` import.
- `declaredTools`'s doc comment stays "no blank or duplicate name":
  the word "blank" already matches the corrected behavior once the
  check trims. No comment reword needed beyond confirming this.

Outside:

- The `declared[w.Tool]` lookup at `:246`. It stays a raw-string map
  lookup; a declared name with internal or leading/trailing
  whitespace besides an all-whitespace name is out of scope.
- Any change to how a step's `tool` field is matched against
  `declared`.

### Fix API

No exported symbol changes. `make api-update` must produce no diff
for `api/runconfig.txt`. No `policy/layers.json` change: `strings` is
standard library, not an internal package edge.

### Fix tests

In `runconfig/runconfig_test/reject_test.go`:

- `TestLoadRejectsWhitespaceOnlyToolName` — a document whose `tools`
  array holds `" "` (a single space). `Load` must return an error
  matching `ErrBadDocument`. Fails against today's code, which loads
  the document successfully.
- `TestLoadRejectsWhitespaceOnlyToolNameWithStep` — the same document,
  with a step whose `tool` field is the same single-space string.
  `Load` must return an error matching `ErrBadDocument`. This is the
  case that shows the silent-resolve consequence: today this document
  loads and the step's binding resolves, with no error anywhere.

In `runconfig/runconfig_test/load_test.go`:

- One positive control: a document whose `tools` array holds a normal
  name (`"grep"`) still loads, and the resolved `Definition.Tools`
  still contains it. Proves the trim only affects the all-whitespace
  case.

### Fix verification

- `make verify` passes; `runconfig` holds the 85 coverage floor.
- `go test -race ./runconfig/...` passes.
- `python3 scripts/check_api.py` passes with no `api/` diff.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`, and
  `scripts/check_prose.py` pass. No `policy/layers.json` change; the
  `runconfig` row is unchanged.
- `python3 scripts/check_docs.py` passes over `declaredTools`'s
  unchanged doc comment.

## Addendum: options.trace flag; hooks stays a caller-set field

Status: planned, not shipped.

### Addendum goal

Let a document turn on run tracing without a code change. `agentrun.Options`
already carries `Tracer *trace.Tracer` and `Hooks *hooks.Registry`, but
`wireOptions` in `runconfig/loader.go` maps only `room`, `ask_to`, and
`budget`. This addendum adds one JSON field, `options.trace`, that
builds a `*trace.Tracer` through `Load`. It leaves `Hooks` out of the
JSON grammar and states why.

### Addendum scope

Inside:

- `wireOptions` gains one field: `Trace bool` with JSON tag `trace`.
- `Load` sets `def.Options.Tracer = trace.New()` when `doc.Options.Trace`
  is `true`. A `false` or absent `trace` key leaves `Options.Tracer` nil.
- `runconfig` imports `trace`. `policy/layers.json`'s `runconfig` row
  gains `"trace"`.
- `docs/architecture.md:32-34`'s prose list of `runconfig`'s imports
  gains `trace`. The mermaid diagram near `docs/architecture.md:131`
  gains one edge, `runconfig --> trace`. Make this update in the same
  commit as the `policy/layers.json` change.
- The document mapping table in this plan's API section gains one row
  for `trace`.

Outside:

- No `HooksKind` or any JSON path that builds a `*hooks.Registry`. See
  the design note below for why.
- No `TracerKind`. `runconfig.Kind` names a step's bound `tools.Tool`,
  resolved per step through `Blocks`. A `Tracer` is not a `tools.Tool`
  and is not bound to one step; it opens one span per run and one span
  per gated step's tool call, across the whole `Runner`. Fitting it
  into the per-step `Kind`/`Blocks` system would misrepresent its
  scope. `options.trace` matches its Options-level scope instead.
- No span-shape configuration (sampling, span naming, export sinks).
  `trace.New()` takes no arguments; a document has nothing else to
  express.

### Design note: why hooks stays out of the JSON grammar

`hooks.Registry.Add` takes a `Handler`, a Go function value
(`func(ctx context.Context, payload any) (bool, error)`). A JSON
document cannot encode a function body. An `options.hooks: true` flag
could still build an empty `hooks.New()` registry, matching the
`trace` shape, but an empty registry changes nothing: `Fire` returns
`nil` at once for a point with zero handlers, so `PointPreTool` never
vetoes and `PointPostTool` and `PointStop` never observe. A document
flag that silently produces a no-op registry is worse than no flag: it
reads as "hooks are on" while nothing fires. `trace.New()` differs
because a bare `*trace.Tracer` is already complete: `Start` builds and
retains a real span with no further setup. The registry is not
complete without a handler.

A caller that needs hooks already has a path with no runconfig change:
`Definition.Options` is a plain `agentrun.Options` value, so the caller
sets `def.Options.Hooks = hooks.New()` and calls `Add` in Go code after
`Load`, the same pattern `Definition.Options.Agent` already uses for
the caller-set agent. `runconfig` requires no new API for this; the
field is already exported and already settable.

### Addendum API

The document's `options` section gains one key:

```json
"options": {
  "room": "platform-team",
  "ask_to": "human-1",
  "budget": {"max_bytes": 200000, "max_events": 500},
  "trace": true
}
```

- `trace` maps to `Options.Tracer`. `true` builds one `*trace.Tracer`
  through `trace.New()`. `false` or an absent key leaves `Options.Tracer`
  nil.

No exported Go symbol changes. `wireOptions` is unexported; `Load`'s
signature, `Definition`'s fields, and every existing exported symbol
stay as locked. `make api-update` must produce no diff for
`api/runconfig.txt`.

### Addendum tests

In `runconfig/runconfig_test/load_test.go`:

- A document with `options.trace: true` loads. `Definition.Options.Tracer`
  is non-nil.
- A document with `options.trace: false` loads. `Definition.Options.Tracer`
  is nil.
- A document with no `trace` key loads. `Definition.Options.Tracer` is
  nil. This is the existing default-value case; add it as an explicit
  case so a future change to `wireOptions`'s zero value is caught.

In `runconfig/runconfig_test/load_integration_test.go`:

- Extend the golden document with `options.trace: true`. Build the
  `Runner` and run it to completion. Assert `Definition.Options.Tracer.Spans()`
  is non-empty after the run, proving the wired `*trace.Tracer` reaches
  the live `agentrun.Runner` and records the run's spans, not just a
  value sitting on `Definition.Options`.

No new test file. `Hooks` gains no test in this addendum; it is
unchanged code, proven by the existing `agentrun` and `hooks` suites.

### Addendum verification

- `policy/layers.json`'s `runconfig` row gains `"trace"`.
- `docs/architecture.md:32-34`'s prose list and the mermaid diagram
  near line 131 both name the new `runconfig --> trace` edge, in the
  same commit as the `policy/layers.json` change.
- `python3 scripts/check_deps.py` passes with the new edge declared.
- `make verify` passes; `runconfig` and the module total hold the 85
  coverage floor.
- `go test -race ./runconfig/...` passes.
- `python3 scripts/check_api.py` passes with no `api/runconfig.txt` diff.
- `python3 scripts/check_plan.py`, `scripts/check_prose.py`, and
  `scripts/check_docs.py` pass.

## Addendum: WorkspaceListKind and WorkspaceStatKind fix (phase 77)

Status: shipped. Phase 76's addendum above expected `WorkspaceListKind`
and `WorkspaceStatKind` to fail `Runner.Run` with `ErrResultNotText`,
because their bound tools returned a typed struct, not a string, in
`tools.Out.Value`. Phase 77 changed `subagent.WorkspaceListTool.Run`
and `subagent.WorkspaceStatTool.Run` to return a JSON-encoded string
instead, matching every other `subagent` tool. All six `Kind`
constants `runconfig` publishes are now confirmed end to end through a
real `Runner.Run`, by
`TestRunnerResolvesWorkspaceListReal` and
`TestRunnerResolvesWorkspaceStatReal` in
`runconfig/runconfig_test/workspace_list_stat_test.go`. No standalone
phase 77 plan file remains for this contract.

## Addendum: five file-toolbox Kinds removed

Status: shipped. See `docs/plans/agents/convergence.md`'s "Boundary
correction" section. `subagent`'s file-editing toolbox (`FileTools`,
`WorkspaceReadTool`, `WorkspaceWriteTool`, `WorkspaceListTool`,
`WorkspaceStatTool`, `DiffTool`) and the `diff` package left the SDK as
coding-agent product surface, not a generic building block. The five
`Kind` constants that routed to that toolbox — `WorkspaceReadKind`,
`WorkspaceWriteKind`, `WorkspaceListKind`, `WorkspaceStatKind`, and
`DiffKind` — left with it. `runconfig`'s
remaining twelve `Kind` constants (`FlowKind`, `LedgerKind`,
`MemoryKind`, `RoomKind`, `SchedulerKind`, `HeartbeatKind`,
`DiscoveryKind`, `TriggerKind`, `ChannelKind`, `ProviderKind`,
`ProviderRegistryKind`, `AsToolKind`) are unaffected. `runconfig`'s
production code never imported `subagent`'s file tools directly; only
`runconfig_test`'s fixtures did, and those fixtures now use a minimal
fake `tools.Tool` per removed `Kind` to prove `Blocks.Set`/dispatch
alone.
