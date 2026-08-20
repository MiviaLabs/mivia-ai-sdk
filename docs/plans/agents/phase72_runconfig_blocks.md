# Phase 72: runconfig blocks

Status: plan, ready for plan review. Ships as changes to the shipped
`runconfig` package. No new top-level package. Depends on
already-shipped packages `subagent`, `workspace`, `diff`, and
`contextbudget`, and on phase 71 (folded into `docs/plans/subagent.md`'s
"File tools addendum" section), which has shipped:
`subagent.OpenFileTools`, `subagent.FileTools`, and
the five file tool constructors all exist today with the
post-phase-71 signature. Phase 71 replaced the five file tool
constructors' `ws *workspace.Workspace` parameter with
`ft *subagent.FileTools`, built through `subagent.OpenFileTools`. This
phase's `Kind` bindings for the five file tools use that shipped
signature throughout; this phase's precondition on phase 71 is
satisfied.

## Why this phase exists

`runconfig` loads a JSON document into a validated `agentrun.Runner`.
Its `Kind` enum names eleven internal tool families a document may
bind a step to: flow, ledger, memory, room, scheduler, heartbeat,
discovery, trigger, channel, provider, and providerregistry.

Three tool families already exist and already implement `tools.Tool`,
but no `Kind` names them, so no JSON document can bind a step to one:

- The five `subagent` file tools: `WorkspaceReadTool`,
  `WorkspaceWriteTool`, `WorkspaceListTool`, `WorkspaceStatTool`, and
  `DiffTool`. `subagent`'s own plan (docs/plans/subagent.md) built
  these as tools a model or a workflow step can call; `runconfig` is
  the one loader that turns a JSON document into a step-bound tool
  set, and today it cannot reach any of the five.
- `subagent.AsTool`, which wraps a built `*agentrun.Runner` as a
  spawnable subagent tool. A document that wants one step to delegate
  to a nested runner has no way to name that binding today.

Separately, `runconfig/loader.go`'s `Definition.Options` maps only
`room` and `ask_to` from the JSON `options` section onto
`agentrun.Options`. Six more `agentrun.Options` fields exist —
`Hooks`, `Tracer`, `Budget`, `Ask`, `Scope`, and `Artifacts` — and a
caller today sets every one of them imperatively in Go after `Load`
returns. This phase closes one of those six gaps, `Budget`, and states
a concrete, checked reason for leaving the other five as Go-composed
values.

`Budget` is not a speculative addition. `runconfig`'s own shipped test
suite already builds one in Go and already proves `Runner` forwards
its validation failure (`runconfig/runconfig_test/runner_test.go`'s
"bad budget" case, and `docs/plans/runconfig.md`'s Tests section
naming it). The plumbing from `Options.Budget` through
`agentrun.New`'s `Budget.Validate` call already exists and is already
tested; the only missing piece is the JSON round trip, the same gap
`room` and `ask_to` already closed for their two fields.

State the true invariant `budget` gates, so this phase's tests match
what the code does, not what a reader might assume. `agent.Agent.Run`'s
own doc comment (`agent/run.go:80-93`) and `confirmStep`
(`agent/run.go:153-183`) fix the scope: `budget.Fits` checks the
cumulative byte total of every step's own declared `payload` field,
built before the bound tool runs, plus the 1-indexed step count. It
never sees a tool's result. `agentrun/wire.go`'s `chain` reads the
tool's actual output into the ack's result string after `Fits` has
already passed judgment on the step, so no budget check ever compares
against a tool's return value; this is a disclosed, shipped scope
limit from phase 32, not a gap this phase changes. This phase closes
the same, narrower gap `room` and `ask_to` already closed: an existing,
already-tested Go-composed field (`Options.Budget`, gating a step's own
declared `payload` size) gains a JSON round trip. Widening `Kind` to
reach `WorkspaceReadTool` and `DiffTool` in the same change is
unrelated to `budget`'s reason for inclusion; those two additions do
not change what bytes `budget.Fits` counts.

## Goal

Widen `runconfig.Kind` so a JSON document can bind a step to a
workspace file tool, a diff tool, or a nested subagent runner, using
the same `Blocks`/`Set`/`Runner`-resolution path every other `Kind`
already uses. Widen the JSON `options` section so a document can state
a context-budget cap as plain data, matching the existing `room` and
`ask_to` scalar fields. Leave every other `agentrun.Options` field
Go-composed, each with a stated reason.

## Scope

Inside:

- Six new `Kind` constants: `WorkspaceReadKind`, `WorkspaceWriteKind`,
  `WorkspaceListKind`, `WorkspaceStatKind`, `DiffKind`, and
  `AsToolKind`. Each is a case in the existing `kinds` validation
  map in `runconfig/blocks.go`. `runconfig` gains no new import for
  this: `Blocks.Set(kind Kind, t tools.Tool)` already accepts any
  `tools.Tool` the caller constructs, and a `WorkspaceReadTool`,
  `DiffTool`, or `AsTool` result already satisfies `tools.Tool`. The
  caller builds the concrete tool in Go — opening a `*subagent.FileTools`
  root through `subagent.OpenFileTools` (phase 71's enforcing
  constructor, mandatory `Deny`), or building a nested
  `*agentrun.Runner` — and registers it under the matching `Kind`
  before calling `Runner`. This is the exact
  shape `LedgerKind`, `RoomKind`, and every other existing `Kind`
  already use: `runconfig` never constructs the underlying block
  itself.
- One new field in the JSON `options` section: `budget`, an object
  with `max_bytes` and `max_events`, mapping onto
  `agentrun.Options.Budget` as a `*contextbudget.Limits`. Present only
  when the JSON document sets it; absent leaves `Options.Budget` nil,
  matching the field's own "Optional" contract in
  `agentrun/options.go`.
- `policy/layers.json` gains `contextbudget` on the `runconfig` row.
  `contextbudget` is a leaf package (`policy/layers.json`'s own
  `contextbudget` row is empty), so the edge adds no transitive
  import.
- `Load`'s doc comment, and this file's API section, name every added
  case.

Outside, each with a reason:

- **`Hooks *hooks.Registry`.** A `hooks.Handler` is a Go closure
  (`func(ctx, ...) (bool, error)` reading `hooks/registry.go`'s
  `Handler` type). No JSON value can name a closure. A document field
  that named a handler by string would need a caller-side registry of
  named closures the loader looks up, which is a second configuration
  system this phase does not add. `Hooks` stays Go-composed.
- **`Tracer *trace.Tracer`.** `trace.New()` takes no arguments and
  returns a `*Tracer` with no fields a caller sets. A JSON toggle for
  it would replace one line of Go
  (`opts.Tracer = trace.New()`) with one JSON field carrying no actual
  configuration. That is a knob with no data behind it, the shape
  `AGENTS.md`'s Building Blocks section calls out as cost without a
  caller. `Tracer` stays Go-composed.
- **`Ask channel.Notifier`.** `channel.Notifier` is a func type
  (`func(ctx, Question) (Answer, error)`), a live transport, not data.
  `AskTo`, the string naming who answers, already maps from JSON. The
  transport itself cannot. `Ask` stays Go-composed; a document may
  still declare `ask_to` and expect the caller to set `Ask` before
  `Runner`.
- **`Scope *tools.Scope`.** `tools.ScopeOptions` mixes two shapes:
  `Allowlist` and `ExtraDenylist`, both plain `[]string`, and
  `Approve`, a `func(ctx, ToolCall) (bool, error)` approval gate. A
  document could express the first two, but a `Scope` built from JSON
  with no `Approve` would silently carry no approval gate for a
  privileged tool call, a security-relevant default the loader must
  never choose on a caller's behalf. Encoding only the safe half of
  `ScopeOptions` invites a caller to assume the whole gate is JSON-
  declared, when only the narrowing half is. `Scope` stays
  Go-composed, whole.
- **`Artifacts *Artifacts`.** `agentrun.Artifacts`'s zero value works
  directly (its methods nil-check `a == nil` and lazily init their
  maps): construction is `&agentrun.Artifacts{}`, one line, with no
  field a caller sets before use. The same "no data behind the knob"
  reason as `Tracer` applies. `Artifacts` stays Go-composed.
- No new JSON top-level section and no new document format. The four
  existing sections (`machine`, `plan`, `options`, `tools`) keep their
  shape; `budget` is one more field inside `options`, following the
  pattern `room` and `ask_to` already set.
- No TOML, YAML, or any non-JSON front end. `Load` still takes
  `[]byte`.
- `Guard`, `Route`, and any other function-typed `flow` or `machine`
  field. Already out of scope per the shipped `docs/plans/runconfig.md`;
  restated here because this phase's Kind widening does not change it.
- `Approve` and `ApprovalThreshold` are not partially JSON-encoded
  either, for the reason given under `Scope` above.

## API

`runconfig/blocks.go` gains six `Kind` constants and their `kinds` map
entries:

```go
const (
    WorkspaceReadKind  Kind = "workspaceread"
    WorkspaceWriteKind Kind = "workspacewrite"
    WorkspaceListKind  Kind = "workspacelist"
    WorkspaceStatKind  Kind = "workspacestat"
    DiffKind           Kind = "diff"
    AsToolKind         Kind = "astool"
)
```

The string values follow the existing derivation rule: every current
`Kind` string is its matching `subagent` constructor's name with the
trailing `Tool` dropped, lowercased — `FlowTool` gives `"flow"`,
`DiffTool` gives `"diff"`, `WorkspaceReadTool` gives
`"workspaceread"`. `AsTool` is the one constructor the literal rule
serves badly: dropping `Tool` from `AsTool` leaves `"as"`, a two-letter
JSON value that names nothing a document author would recognize.
`AsToolKind`'s string stays close to the constructor's own name,
`"astool"`, instead of the two-letter literal derivation. No other
`Kind` needs the same exception, since `AsTool` is the only
constructor in `subagent` whose stem is not already a recognizable
word once `Tool` is dropped.

A document names any of the six the same way it names `"ledger"` or
`"room"` today, on a step's `internal` field:

```json
{
  "id": "read-notes",
  "needs": [],
  "to": "done",
  "when": "on_finished",
  "internal": "workspaceread"
}
```

The caller resolves it exactly as every other `Kind`:

```go
deny, err := secretpath.NewMatcher([]string{"*.env", "*.pem"})
if err != nil {
    return err
}
ft, err := subagent.OpenFileTools(subagent.FileToolOptions{
    Root: "/srv/agent-root",
    Deny: deny,
})
if err != nil {
    return err
}
blocks := runconfig.NewBlocks()
blocks.Set(runconfig.WorkspaceReadKind, subagent.WorkspaceReadTool("read-notes", ft, 65536))
def.Blocks = blocks
```

`runconfig/loader.go` gains one field on `wireOptions` and one nested
type:

```go
type wireOptions struct {
    Room   string       `json:"room"`
    AskTo  string       `json:"ask_to"`
    Budget *wireBudget  `json:"budget"`
}

type wireBudget struct {
    MaxBytes  int `json:"max_bytes"`
    MaxEvents int `json:"max_events"`
}
```

`Load` maps a present `Options.Budget` onto
`agentrun.Options.Budget = &contextbudget.Limits{MaxBytes: ..., MaxEvents: ...}`.
A nil `wireOptions.Budget`, or a nil `wireOptions` itself, leaves
`Definition.Options.Budget` nil. `Load` performs no range check on the
two integers; `contextbudget.Limits.Validate`, called inside
`agentrun.New` during `Definition.Runner`, rejects a negative value
the same way it already rejects one set through Go.

Full JSON document example, widened from `docs/plans/runconfig.md`'s
example:

```json
{
  "machine": {
    "initial": "idle",
    "transitions": [{"from": "idle", "to": "done", "trigger": "next"}]
  },
  "plan": {
    "panels": [],
    "steps": [
      {
        "id": "read-notes",
        "needs": [],
        "to": "done",
        "when": "on_finished",
        "internal": "workspaceread"
      }
    ]
  },
  "options": {
    "room": "platform-team",
    "ask_to": "human-1",
    "budget": {"max_bytes": 200000, "max_events": 500}
  },
  "tools": []
}
```

Exported surface, landing in `api/runconfig.txt` via `make api-update`:

```go
const WorkspaceReadKind Kind = "workspaceread"
const WorkspaceWriteKind Kind = "workspacewrite"
const WorkspaceListKind Kind = "workspacelist"
const WorkspaceStatKind Kind = "workspacestat"
const DiffKind Kind = "diff"
const AsToolKind Kind = "astool"
```

No other exported symbol changes. `Load`, `Definition`, `Binding`,
`Blocks`, `NewBlocks`, `Runner`, and the three sentinels keep their
current signatures; `wireOptions` and `wireBudget` are unexported.

`Load`'s doc comment enumerates rejection conditions today; it has no
options-field mapping list to extend, for `room` and `ask_to` either.
This phase adds one clause to that comment naming the `budget`
mapping, in the same change as the code.

## Tests

Tests live in `runconfig/runconfig_test/`, the existing external test
package. This phase adds cases to the existing files and adds no new
file, since no new test kind is needed beyond what `load_test.go`,
`reject_test.go`, `runner_test.go`, and `load_integration_test.go`
already cover.

- `load_test.go`: a table case per new `Kind` constant, asserting a
  step naming `"workspaceread"`, `"workspacewrite"`,
  `"workspacelist"`, `"workspacestat"`, `"diff"`, or `"astool"` on
  `internal` produces a `Binding` with `Internal: true` and the
  matching `Kind`. One case for `options.budget` present, asserting
  `Definition.Options.Budget` equals the parsed `*contextbudget.Limits`.
  One case for `options.budget` absent, asserting `Definition.Options.Budget`
  is nil, pinning the "absent stays nil" rule the API section states.
- `reject_test.go`: no new rejection case is needed for the six `Kind`
  strings, since an unknown `internal` value already returns
  `ErrBadDocument` through the existing `kinds` map check; a table
  case confirms a near-miss string, for example `"workspace"` without
  a suffix, still rejects. A negative `max_bytes` or `max_events` in
  `budget` is not a `Load`-time rejection per the API section; a case
  asserts `Load` accepts it and the rejection surfaces later, from
  `runner_test.go`. No new case covers a step setting `sub` beside one
  of the six new `internal` values: `buildStep`'s existing
  `sub`-beside-`tool`-or-`internal` check runs before the `kinds` map
  lookup, so it already rejects every `internal` string, new or old,
  the same way. The existing `sub` rejection cases in `reject_test.go`
  cover this without a new row.
- `runner_test.go`: a table case per new `Kind`, each building a
  minimal `tools.Tool` stub, setting it on `Blocks` under the matching
  `Kind`, and asserting `Runner` resolves the bound step to that tool.
  One case sets a negative `budget.max_events` and asserts `Runner`
  returns the wrapped `contextbudget` validation error, proving the
  deferred-validation claim in the API section. One case builds a
  `*subagent.FileTools` over `t.TempDir()` through
  `subagent.OpenFileTools` with a non-nil `Deny`, wraps it with
  `subagent.WorkspaceReadTool`, sets it on `Blocks` under
  `WorkspaceReadKind`, and asserts a `Runner`-built registry runs it
  and reads the seeded file back, proving the `Kind` composes with the
  real `subagent` and `workspace` types, not only a stub. One further
  case builds a real, minimal `*agentrun.Runner` (a machine with one
  transition, a plan with one step, no tools) and wraps it with
  `subagent.AsTool`, sets it on `Blocks` under `AsToolKind`, and
  asserts a `Runner`-built registry runs the nested runner to
  completion. `AsToolKind` needs its own real-constructor case,
  separate from the `WorkspaceReadKind` one, because it resolves a
  different concrete type (`*agentrun.Runner` plus
  `subagent.ToolOptions`, not `*subagent.FileTools`) and is otherwise
  covered only by the generic stub case like `WorkspaceWriteKind`,
  `WorkspaceListKind`, `WorkspaceStatKind`, and `DiffKind` stay.
- `load_integration_test.go`: widen the golden document to add one
  step bound to `WorkspaceReadKind` over a `t.TempDir()`-backed
  `*subagent.FileTools`, alongside the existing `flow` internal step
  and the external tool. The concrete agent run reads a seeded file
  through the loaded step and the test asserts the read content
  reaches `Definition.Options.Artifacts` (or the resolver stub the
  existing integration test already uses) unchanged. This is the
  end-to-end pin for "a document naming a workspace tool builds a
  runnable resolver," matching the file's existing role for the
  `flow` internal case.
- `load_integration_test.go` (or `runner_test.go`, builder's choice of
  file, same fixture either way): one new case proves the real
  invariant the "Why this phase exists" section states, not a tool's
  output size. The case loads a document whose one step declares a
  JSON `payload` field sized larger than a JSON-declared
  `options.budget.max_bytes`, with the step's `tool` field naming
  `stubTool`, reusing `runner_test.go`'s existing `stubTool` fixture
  (echoes its input, costs nothing) registered on `Definition.External`
  via `d.External.Add`, the same pattern `loadForRunner` already uses
  in `runner_test.go`. The test loads the `Definition` and calls its
  `Runner()` method, asserting it succeeds. It then calls the
  resulting `Runner.Run` and asserts the run reports the budget
  rejection, confirming
  `agentrun/wire.go`'s `Runner.Run` passes `r.budget` into
  `r.agent.Run` on every call, so the JSON-declared cap enforces during
  the run against the step's own declared `payload`, not only during
  `New` against a static `Limits` value. Because `confirmStep`'s
  `budget.Fits` check runs before the bound tool executes
  (`agent/run.go:153-183`), the rejection proves the cap fires on the
  step's declared-payload size alone, independent of `stubTool`'s
  behavior. A second, separate case in the same test pairs naturally
  with the first: it combines a `WorkspaceReadKind` binding with a
  `budget` well above the bound file's size, and asserts the run
  succeeds, proving the `Kind` addition and the `budget` addition
  compose without the tool's output size affecting the outcome either
  way. Neither case claims `budget` gates a tool's return value;
  `agent/run.go:80-93`'s doc comment and `confirmStep`
  (`agent/run.go:153-183`) fix that scope, and this phase does not
  change it. The existing "bad budget" case in `runner_test.go` stays;
  it pins the separate, already-covered static-negative-`Limits`
  rejection path through `Validate`, not the
  payload-size-exceeded-at-run-time path this new case adds.

## Verification

- `policy/layers.json` grants `runconfig` the added `contextbudget`
  row entry, alongside its existing `["agentrun", "flow", "machine",
  "subagent", "tools"]` imports.
- `make verify` passes; `runconfig` and the module total hold the 85
  floor.
- `go test -race ./runconfig/...` passes.
- `make api-update` lands the `api/runconfig.txt` diff for the six
  `Kind` constants, in the same change as the code.
- `python3 scripts/check_prose.py` and `check_labels.py` pass over
  this plan file and the updated `runconfig` doc comments.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass. `check_deps.py` confirms `runconfig`'s `.go` files import
  only `agentrun`, `contextbudget`, `flow`, `machine`, and `tools` —
  `subagent` stays in the policy row for the caller-facing doc
  comments' cross-reference but gains no actual import, matching its
  unused-but-declared state before this phase.
- `docs/plans/runconfig.md` gains this phase's `Kind` and `budget`
  additions in its own API section, in the same change that ships the
  code, following `docs/plans/TEMPLATE.md`'s API-surface discipline
  the way phase 71's addendum folded into `docs/plans/subagent.md`.
- `AGENTS.md` carries no `runconfig/` entry today; `grep -n
  "runconfig" AGENTS.md` returns nothing. The Layout list in AGENTS.md
  orders packages by architectural layer, not alphabetically. This
  phase's builder adds one bullet immediately after the `subagent/`
  bullet (`AGENTS.md:19`) and before the `ledger/` bullet, since
  `runconfig` composes `agentrun.Options` at the same layer `agentrun`
  and `subagent` occupy. The new bullet follows the list's existing
  one-line-per-package format: `- \`runconfig/\` — JSON-document
  loader binding a step graph to agentrun.Options.` The builder does
  not add a separate import-list sentence; the existing bullets in
  AGENTS.md's Layout list name only the package's concern, not its
  import edges, so the new `runconfig/` bullet follows that same
  one-clause shape.

## Addendum: schema-decode fix for the five file/diff Kinds (phase 76)

This phase shipped `agentrun/wire.go`'s `chain` always passing a raw
string payload to a resolved tool, with no check for
`tools.SchemaTool`. The five file/diff internal Kinds this plan added
(`WorkspaceReadKind`, `WorkspaceWriteKind`, `WorkspaceListKind`,
`WorkspaceStatKind`, `DiffKind`) each require a typed args struct only
their own `DecodeArguments` produces, so a step bound to one of them
failed unconditionally through a real `Runner.Run`, a gap this plan's
own tests worked around instead of closing. Phase 76 closed the
decode half of the gap: `chain` now decodes a `tools.SchemaTool`
tool's payload before it calls the tool. Decode success does not by
itself complete the run: `chain` also requires the tool's result to
be a string. `WorkspaceReadKind`, `WorkspaceWriteKind`, and
`DiffKind` return a string and are confirmed end to end.
`WorkspaceListKind` and `WorkspaceStatKind` bind tools whose `Run`
returns a struct, not a string, so they are expected to still fail a
real `Runner.Run`, with `agentrun.ErrResultNotText`, pending a
follow-up fix. This plan's own design and prose above stay unchanged;
see `docs/plans/agents/phase76_agentrun_schema_tool_decode.md` for the
fix.
