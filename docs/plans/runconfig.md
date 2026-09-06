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

## Addendum: document-built internal tools

Status: planned, not shipped. This addendum lands the `runconfig` to
`subagent` wiring and closes `subagent`'s `policy/pending_wiring.json`
entry. It also replaces the caller-builds-`Blocks` step the `Kind` doc
comment in `runconfig/blocks.go` still describes.

### Addendum goal

The document's `internal` section builds a wireable `subagent` tool at
`Load` time. A step's `"internal"` binding then resolves with no
caller-built block. Each `Kind` resolves to its `subagent` constructor
through code, and a conformance test pins the link.

### Addendum scope

Inside:

- One new optional top-level document section, `internal`. Each key
  names one `Kind`. Each value is that `Kind`'s configuration object.
- One new file `runconfig/internal.go`. It holds the `wireInternal`
  config struct, the `builders` table, the `callerBuilt` set, and the
  `buildInternal` step `Load` calls.
- The `builders` table maps each wireable `Kind` to a builder function.
  Each builder body is exactly one `subagent` constructor call. This
  table is the mechanical link the doc comment's prose link replaces.
- One new sentinel, `ErrCallerBuilt`. `Load` returns it wrapped in
  `ErrBadDocument` when the document declares a caller-built `Kind`.
- Six wireable Kinds: `discovery`, `flow`, `heartbeat`, `ledger`,
  `memory`, `room`. Section "Addendum API" pins each one's config and
  constructor.
- Six caller-built Kinds: `astool`, `channel`, `provider`,
  `providerregistry`, `scheduler`, `trigger`. A document that declares
  one is rejected. The caller keeps setting these through `Blocks.Set`.

Outside:

- Any dependency-injection grammar beyond the six flat config objects.
  The config struct holds plain scalars only.
- Mailbox kinds. No `Kind` constant names `SendTool` or `InboxTool`
  today. They stay out until a caller names the need.
- A durable store for `ledger`. A document-built ledger runs over
  `ledger.NewMemStore`. A deployment needing durability sets its own
  `LedgerTool` through `Blocks.Set`.
- Any change to `subagent`. Its constructors keep their signatures.
  `runconfig` composes them through their public API.
- Any change to `Runner`, `Binding`, `Blocks`, or the step `"internal"`
  field. A document without an `internal` section behaves exactly as
  before.

Design rules:

- A `Kind` is wireable exactly when every constructor argument is a
  document scalar or a value the loaded `Definition` already holds.
  A Go function value or a live object makes the `Kind` caller-built.
- Each caller-built constructor takes one such value:
  `scheduler.Job`, `trigger.Condition` and `trigger.Action`,
  `channel.Notifier`, `provider.Completer`,
  `providerregistry.Registry` with `providerregistry.Retryable`, and
  `*agentrun.Runner`. A JSON document cannot encode any of them.
- `flow` binds the document's own `Definition.Plan` and
  `Definition.Machine`. `flow.Run` walks a plan structurally and never
  runs step tools, so a flow tool bound inside its own plan cannot
  recurse.
- The builder's `name` argument is the `Kind`'s own string value. Tool
  error messages then name the family, for example
  `heartbeat: subagent: bad command`.
- `buildInternal` walks the section keys in sorted order. Rejections
  stay deterministic when a document declares several bad keys.

Precedence rules:

- `Blocks.Set` after `Load` replaces a document-built tool. Last write
  wins. Assigning a whole new `Blocks` to `Definition.Blocks` also
  wins.
- A declared wireable `Kind` with no bound step is allowed. The built
  tool stays unused in `Blocks`.
- A step binding a wireable `Kind` with no declaration still needs
  `Blocks.Set`. `Runner` fails it with `ErrUnknownInternal`, exactly as
  today.

### Addendum API

The `internal` section, one heartbeat binding:

```json
{
  "machine": {"initial": "queued", "transitions": [
    {"from": "queued", "to": "done", "trigger": "run"}
  ]},
  "plan": {"steps": [
    {"id": "beat", "to": "done",
     "payload": "{\"op\":\"beat\",\"id\":\"w1\"}",
     "internal": "heartbeat"}
  ]},
  "internal": {"heartbeat": {"timeout": "30s"}},
  "tools": []
}
```

Per-Kind config and constructor:

- `discovery` — no fields. Calls
  `subagent.DiscoveryTool(name)`.
- `flow` — no fields. Calls
  `subagent.FlowTool(name, d.Plan, d.Machine, nil)`. A bus stays
  caller-side; a caller wanting bus events uses `Blocks.Set`.
- `heartbeat` — field `timeout`, a duration string parsed by
  `time.ParseDuration`. Calls `heartbeat.New(timeout)`, then
  `subagent.HeartbeatTool(name, monitor)`.
- `ledger` — fields `actor`, a non-blank string, and `lease`, a
  positive duration string. Calls
  `ledger.New(ledger.NewMemStore(), nil)`, then
  `subagent.LedgerTool(name, l, ledger.Actor(actor), lease)`.
- `memory` — field `max_bytes`, a positive integer. Calls
  `memory.New(maxBytes)`, then `subagent.MemoryTool(name, store)`.
- `room` — fields `id`, `founder`, `actor`, three non-blank strings.
  Calls `room.New(id, founder)`, then
  `subagent.RoomTool(name, r, actor)`.

The `wireInternal` struct holds every field above with `json` tags
`timeout`, `max_bytes`, `id`, `founder`, `actor`, and `lease`. Each
builder reads only its own fields. Unknown fields are ignored, matching
the step grammar's rule. A duplicate section key follows
`encoding/json` map semantics: the last value wins. Rejecting a
duplicate key stays out of scope: map decoding cannot see the earlier
key.

Rejections, each wrapped in `ErrBadDocument`:

- A section key no `Kind` constant names. The message says
  `unknown internal` and names the key.
- A caller-built key. The error also wraps `ErrCallerBuilt` through
  `fmt.Errorf` with two `%w` verbs. The message names the key.
- A duration field that fails `time.ParseDuration`, for `heartbeat` or
  `ledger`. The message names the kind and the field.
- A blank `actor`, for `room` or `ledger`, or a `lease` at or below
  zero. The loader checks these three itself; no typed constructor sees
  them.
- Any rejection from `heartbeat.New`, `memory.New`, or `room.New`. The
  loader forwards the constructor's own sentinel wrapped in
  `ErrBadDocument`, matching the `machine.New` and `flow.New` rule.

Exported surface delta, exactly one new symbol:

```go
// ErrCallerBuilt names an internal Kind a document cannot build.
// Load wraps it inside ErrBadDocument. Test with errors.Is.
var ErrCallerBuilt = errors.New("runconfig: internal kind stays caller-built")
```

- `make api-update` adds exactly one row, `var ErrCallerBuilt`, to
  `api/runconfig.txt`, sorted between `var ErrBadDocument` and
  `var ErrUnknownInternal`. Commit the lock diff in the same change.
- `Load`'s doc comment gains the two new rejection cases in its
  enumerated list.
- The `Kind` and `Blocks` doc comments in `runconfig/blocks.go` stop
  describing the caller-builds step for wireable Kinds. They state the
  split: the document builds six Kinds, the caller sets the rest.

Import policy:

- The `runconfig` row in `policy/layers.json` gains `heartbeat`,
  `ledger`, `memory`, and `room`. The `subagent` edge already exists.
  This plan edit landed with the addendum.

### Addendum tests

The tests land first and fail. The implementation follows. New test
names sit in backticks until they ship.

New file `runconfig/runconfig_test/internal_test.go`, package
`runconfig_test`:

- `TestLoadBuildsInternalTools` — table-driven over the six wireable
  Kinds. Each row loads a document with a valid `internal` declaration
  and one step binding that Kind, sets only `Options.Agent`, and
  asserts `Runner` builds. No `Blocks.Set` call appears anywhere. One
  extra row pins the unknown-field rule: a `heartbeat` config carrying
  a stray `max_bytes` key still loads, and `Runner` still builds.
- `TestPartialInternalResolution` — two rows against a partially
  filled `Blocks`. Row one: the document declares `heartbeat`, one
  step binds `memory`, and no `Blocks.Set` runs. `Runner` returns
  `ErrUnknownInternal` naming `memory`, proving a document-built entry
  never masks an undeclared sibling Kind. Row two: the document
  declares `heartbeat` and `memory`; the caller `Blocks.Set` a failing
  stub over `memory` only. The machine runs `queued` to `mid` to
  `done`; step `beat` binds `heartbeat` with `to` `mid`; step `mem`
  needs `beat`, binds `memory`, with `to` `done`. Assert the run's
  error carries the stub's sentinel and names step `mem`, and the
  reached status is `done`. Flow fires the step's transition before
  the confirm-time tool call, so the failed step's `to` is already
  reached. The heartbeat step answered from the
  document-built tool; the memory step ran the caller's stub. Each
  binding resolves from its own source.
- `TestLoadRejectsCallerBuiltInternal` — table-driven over the six
  caller-built Kinds. Each row's document declares the Kind in the
  `internal` section. Assert the error matches both
  `errors.Is(err, ErrBadDocument)` and
  `errors.Is(err, ErrCallerBuilt)`, and names the key.
- `TestLoadRejectsBadInternalConfig` — table-driven: an unknown key, a
  bad `heartbeat` duration, a zero `heartbeat` timeout, a zero
  `memory` `max_bytes`, a blank `room` `id`, a blank `room` `founder`,
  a blank `room` `actor`, a blank `ledger` `actor`, a bad `ledger`
  lease string, and a zero `ledger` lease. Each row asserts
  `ErrBadDocument` and the naming fragment.
- `TestInternalToolsRunThroughRunner` — table-driven over the five
  command Kinds (`discovery`, `heartbeat`, `ledger`, `memory`,
  `room`). Each row's step payload carries one valid JSON command.
  Each run completes with the final status `done`. This proves the
  wired tool answers with a string result through a real runner.
- `TestInternalFlowToolRunsOwnPlan` — one flow row. The document's own
  machine and plan walk to a final status, and the run completes.
- `TestCallerSetOverridesDocumentKind` — a document builds `memory`;
  the caller then sets a failing stub through `Blocks.Set`. The run
  fails with the stub's error, proving the caller's tool ran.

New file `runconfig/internal_conformance_test.go`, package
`runconfig`:

- `TestKindBuildersPinConstructors` — the conformance pin. One table
  with all twelve Kinds, each marked wireable or caller-built, each
  wireable row carrying a reference builder that calls the `subagent`
  constructor by hand. Assert four things: the table's kind set equals
  the `kinds` map's key set; the `builders` table's key set equals the
  table's wireable set; the `callerBuilt` set equals the table's
  caller-built set; and for each wireable Kind, `fmt.Sprintf("%T", ...)`
  of the document-built tool equals that of the reference call. A
  rewired constructor or an undecided new `Kind` fails this test.
- Behavior parity rides the same table. For each wireable Kind, run
  the document-built tool and the reference tool on one valid command
  input, and assert equal outputs.

Extend `runconfig/runconfig_test/load_fuzz_test.go` seeds with one
document carrying an `internal` section.

No existing test changes. `TestRunnerResolvesNewKindsStub` and
`TestGoldenDocumentRuns` keep passing unchanged. They prove the
caller-built path still works when a document declares nothing.

### Addendum verification

- `make verify` passes. `runconfig` and `subagent` and the module
  total hold the 85 coverage floor.
- `go test -race ./runconfig/...` passes.
- `make api-update` lands the `api/runconfig.txt` diff in the same
  change as the code.
- The builder removes `subagent`'s entry from
  `policy/pending_wiring.json` in the same change. Once `runconfig`
  imports `subagent`, `check_orphan_packages.py` reads the entry as
  stale and fails until it is removed.
- `docs/architecture.md` updates in the same change: the prose import
  list near line 32 and the mermaid edges both gain `runconfig -->
  heartbeat`, `runconfig --> ledger`, `runconfig --> memory`, and
  `runconfig --> room`.
- `docs/packages/runconfig.md` updates in the same change: the
  `internal` section, `ErrCallerBuilt`, and the wireable split. The
  Usage snippet near lines 160 to 172 builds a fresh `NewBlocks()` and
  assigns it over `def.Blocks`; that pattern silently discards every
  document-built tool once this change lands. Rewrite the snippet to
  call `def.Blocks.Set(...)` after `Load` instead of replacing
  `def.Blocks`.
- `runconfig/doc.go` updates in the same change: the package doc's
  enumeration of what the document names gains the `internal` section,
  beside the machine rows, the plan steps, the options, and the
  external tool set.
- `python3 scripts/check_plan.py`, `scripts/check_deps.py`,
  `scripts/check_prose.py`, `scripts/check_labels.py`, and
  `scripts/check_orphan_packages.py` pass.

## Addendum: one step tool wrapper

### Coupling and landing order

`newStepTool` builds sixteen wrapper variants. Each variant exposes
exactly the optional interfaces `inner` implements. Four direct
interface assertions consume the wrapper:

- `agentrun/wire.go` asserts `tools.SchemaTool` before decode.
- `tools/registry_timeout.go` asserts `tools.ProfiledTool` for the
  timeout backstop.
- `tools/scope.go` reads privilege through `tools.IsPrivileged`, a
  helper.
- `agentloop/wire.go` reads the budget through `tools.ResultBudgetOf`,
  a helper.

No runconfig-built tool reaches `agentloop`'s own `tools.SchemaTool`
assertion. No in-tree code routes a runconfig registry into an
`agentloop.Loop`.

This change and the `agentrun` change of the same name land as one
change. The wrapper collapse alone stays behavior-identical. The
`agentrun` probe swap alone stays behavior-identical. One change keeps
one review surface. Within the change, the `agentrun` edit lands
first.

### New shape

`steptool.go` keeps one struct, `stepTool`, and deletes the four
capability structs and the sixteen variants. `newStepTool` returns
`&stepTool{step, inner}`. The struct gains five forwarding methods:

- `ExecutionProfile` returns `tools.ExecutionProfileOf(inner)`.
- `MaxResultBytes` returns the count from `tools.ResultBudgetOf(inner)`.
- `Privileged` returns `tools.IsPrivileged(inner)`.
- `ParameterSchema` returns the schema from `tools.SchemaOf(inner)`.
- `DecodeArguments` forwards to `inner` when it implements
  `tools.SchemaTool`. Otherwise it returns the raw bytes unchanged as
  an `InOut` string value.

The wrapper now declares all four optional interfaces for every inner
tool. The methods degrade to inner's published defaults. Three of the
four consumers read values, not presence, so behavior holds:

- `effectiveRunTimeout` treats a zero declared `Timeout` like an
  absent profile. The forwarded zero profile matches the old absent
  interface.
- The scope and budget paths read helpers that forward the same
  defaults.
- The `agentrun` chain now always calls `DecodeArguments`. For a
  schema-less tool the identity fallback reproduces the old plain
  payload pass-through byte for byte. A decode failure still fails the
  step only when `inner` truly decodes.

### Exported surface

No exported symbol of `runconfig` or `agentrun` changes. All wrapper
types stay unexported. `make api-update` produces no `api/` diff. No
`policy/layers.json` edge changes.

### Tests

Rewrite `runconfig/steptool_internal_test.go`:

- Keep `TestNewStepToolForwardsAllCapabilities` unchanged.
- Keep `TestNewStepToolForwardsPerSubset` unchanged.
- Delete `TestNewStepToolInterfaceParity`. It pins exact interface
  parity, which this change removes by design.
- Delete `TestNewStepToolForwardsNoCapabilities`. It pins interface
  absence, which this change removes by design. Report the deletion
  like the parity test's.
- Add `TestNewStepToolDeclaresAllCapsAlways`. Table-driven over the
  sixteen capability subsets. Each row builds `inner` with the subset,
  wraps it, and asserts the new shape: the wrapper satisfies all four
  interfaces; each forwarded value equals `inner`'s through the
  `tools.*Of` helpers; a schema-less row yields a nil schema and an
  identity decode. This test fails against the sixteen-variant shape,
  so it proves the refactor.

`scripts/check_test_tampering.py` will flag both deleted tests.
The deletions are mandated by this addendum. The builder reports the
mandate in the change notes and does not weaken the gate.

### Composition with the document-built internal tools addendum

The earlier addendum wires `runconfig` to `subagent` and edits
`runconfig/runner.go` builders. This addendum edits
`runconfig/steptool.go` and `runconfig/steptool_internal_test.go`.
The `newStepTool` call site in `runner.go` keeps its signature. The
file sets overlap only in `runner.go` context lines. This addendum
lands first; the internal-tools addendum rebases on it.

### Verification

- `make verify` passes. Coverage floors for `runconfig`, `agentrun`,
  and `tools` hold at 85 or better.
- No `api/` diff and no `policy/` diff is expected. Any diff stops the
  change for review.
- `python3 scripts/check_plan.py`, `scripts/check_prose.py`, and
  `scripts/check_labels.py` pass.
