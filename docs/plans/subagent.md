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
  plan and reports the final status; `ProviderTool` runs one turn;
  `ProviderRegistryTool` routes one turn over `providerregistry`'s
  named fallback order; `TriggerTool` fires a named trigger;
  `ChannelTool` asks a human through a Notifier.
- Spawn tracing: `ToolOptions.Tracer` opens one `subagent.spawn` span
  per spawn through a caller-supplied `trace.Tracer`. A runner wired
  with the same tracer instance nests its run's spans under the
  spawn span.
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
var ErrMaxDepth, ErrBadCommand, ErrMailboxFull, ErrInvalidCapacity

func FlowTool(name string, plan *flow.Definition, m *machine.Definition, bus *events.Bus) tools.Tool
func LedgerTool(name string, l *ledger.Ledger, actor ledger.Actor, lease time.Duration) tools.Tool
func MemoryTool(name string, s *memory.Store) tools.Tool
func RoomTool(name string, r *room.Room, actor string) tools.Tool
func SchedulerTool(name string, s *scheduler.Scheduler, job scheduler.Job) tools.Tool
func HeartbeatTool(name string, m *heartbeat.Monitor) tools.Tool
func DiscoveryTool(name string) tools.Tool
func ProviderTool(name string, c provider.Completer) tools.Tool
func ProviderRegistryTool(name string, reg *providerregistry.Registry, order []string, retryable providerregistry.Retryable) tools.Tool
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
"provider", "providerregistry", "room", "scheduler", "taskrun",
"tools", "trace", "trigger"]`
edges. The package imports each block only through its public API.

## Tests

Tests live in `subagent/subagent_test/`, one external package:

- `astool_test.go` — status and artifact results, failure
  propagation, and repeated spawns on fresh threads.
- `providerregistrytool_test.go` — fallback routing, the
  all-failed sentinel, usage composition, and the spawn-span
  nesting under the caller's span.
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

## Metamorphic test suite

New file `subagent/subagent_test/metamorphic_test.go`, package
`subagent_test`, using the existing `signedMessage` and `badMessage`
helpers from `mailbox_test.go`. Each case is a property pair: apply a
transformation to a valid input, assert the stated outcome.
Table-driven; one `TestMetamorphic*` function per property.

- `TestMetamorphicMailboxFullRejectsInInsertionOrder` — property: a
  mailbox at capacity rejects new sends in insertion order. Table
  varying capacity (one, two, three) and the count of overflow
  attempts. For each case: build a `Mailbox` at the given capacity,
  `Deliver` exactly `capacity` distinct signed messages in order,
  assert every call returns `nil`. `Deliver` one more message, assert
  `errors.Is(err, subagent.ErrMailboxFull)`. Call `Take`, assert the
  drained slice equals the delivered messages, same payloads, same
  order. Confirmed true against `mailbox.go`: `Deliver` appends only
  when `len(m.msgs) < m.cap`, and `Take` returns `m.msgs` unmodified
  otherwise.
- `TestMetamorphicMailboxDeliveredMessageNeverDropped` — property: a
  delivered message is never silently dropped. Table varying
  capacity (two, eight, thirty-two). For each case: build a `Mailbox`
  at the given capacity, deliver exactly `capacity` distinct signed
  messages concurrently from `capacity` goroutines gated by a start
  channel, so every `Deliver` call lands inside the capacity bound.
  Assert every goroutine's `Deliver` call returns `nil`. Call `Take`
  once and assert the drained set of payloads, compared as a set, not
  an order, equals the set of sent payloads, with no loss and no
  duplication. Confirmed true against `mailbox.go`: `Deliver` and
  `Take` both hold `m.mu` for their full body, so no interleaving
  drops an appended message.

## Verification

- `make verify` passes; subagent and the module total hold the 85
  floor.
- `go test -race ./subagent/...` passes.
- `make api-update` lands `api/subagent.txt` in the same change.
- The metamorphic suite is test-only: no exported symbol changes, so
  `make api-update` must produce no diff for `api/subagent.txt` in
  that change. `go test -race ./subagent/...` covers the new file.
- The e2e system scenarios in `docs/plans/e2e.md` drive the package
  end to end.
- `python3 scripts/check_prose.py` and `check_labels.py` pass.

## File tools addendum

Status: shipped. This addendum wires the already-shipped
leaf packages `workspace` and `diff` into `subagent` as tools, so a
model driving a subagent through `agentloop` can read, list, stat,
and write files inside a caller-bound sandbox, and preview a bounded
diff before a write. `envfile` gets no tool; see its section below.

### Goal

Give a subagent's tool registry four file-confined operations
(read, write, list, stat) plus one preview operation (diff), each
bound to a `*workspace.Workspace` the deployment supplies once, at
wiring time. A model never chooses the sandbox root; it chooses only
a path inside the root the caller already picked.

### Scope

Inside:

- Five new tools, one file each, following the existing `*tool.go`
  pattern (`Name`, `Run`) plus `tools.SchemaTool`
  (`ParameterSchema`, `DecodeArguments`), so each tool is offerable
  through `agentloop.Definitions` without a caller-written adapter.
  `agentloop.Definitions` skips any tool with no published schema
  (see `AGENTS.md`'s `tools/` entry), so a schema-less file tool
  would be silently unreachable from a loop; these five tools close
  that gap for file access the way `spool.SpoolTool`'s `schemaCap`
  closes it for a spooled result.
- `WorkspaceReadTool(name string, ws *workspace.Workspace, maxResultBytes int) tools.Tool`.
  Reads one file at a caller-model-supplied path, relative to `ws`'s
  bound root. `maxResultBytes`, when positive, publishes
  `tools.ResultBudgetTool`, so `agentloop`'s `render` truncates an
  oversized read instead of flooding the model's context; a
  non-positive value publishes no budget. `ws.ReadFile` today carries
  no size bound of its own (see `workspace/read.go`'s doc comment),
  so this tool-level cap is the only bound in the current shipped
  `workspace` surface (`api/workspace.txt`). Not privileged.
  `ExecutionProfile.Class` is `tools.ExecutionClassRead`.
- `WorkspaceWriteTool(name string, ws *workspace.Workspace) tools.Tool`.
  Writes one file at a caller-model-supplied path plus
  caller-model-supplied content. Creates the file with a fixed mode,
  `0o600`; no `os.FileMode` argument ever reaches the model, matching
  the reasoning `docs/plans/workspace.md` already states for
  dropping `WriteFile`'s `perm` parameter on a model-reachable path.
  The one truly dangerous operation this addendum adds: it mutates
  the filesystem inside `ws`'s root. Implements `tools.PrivilegedTool`
  returning `true`, so `tools.Scope.Allowed` denies it unless a
  caller's `ScopeOptions.Allowlist` names it explicitly; see
  "Privilege model" below. `ExecutionProfile.Class` is
  `tools.ExecutionClassWrite`.
- `WorkspaceListTool(name string, ws *workspace.Workspace) tools.Tool`.
  Lists one directory, relative to `ws`'s bound root; a blank path
  lists the root itself. Result is a JSON-renderable
  `[]WorkspaceEntry`, one `{name, is_dir}` pair per entry, sorted the
  way `ws.List`'s underlying `os.ReadDir` already sorts. Not
  privileged. `ExecutionProfile.Class` is `tools.ExecutionClassRead`.
- `WorkspaceStatTool(name string, ws *workspace.Workspace) tools.Tool`.
  Stats one path, relative to `ws`'s bound root. Result is one
  `WorkspaceFileInfo` (`name`, `size`, `is_dir`, `mod_time`). Not
  privileged. `ExecutionProfile.Class` is `tools.ExecutionClassRead`.
- `DiffTool(name string, ws *workspace.Workspace, maxLines int) tools.Tool`.
  Previews a write: diffs the on-disk content at a
  caller-model-supplied path, read through `ws.ReadFile`, against
  caller-model-supplied proposed content, through `diff.Unified`. A
  path that does not yet exist (`ws.ReadFile` returns a wrapped
  `fs.ErrNotExist`) diffs against empty content, so a new-file
  proposal renders the same way `diff -u /dev/null` would. Any other
  `ws.ReadFile` error, including `workspace.ErrEscape`, propagates
  unchanged. `maxLines` passes straight to `diff.Unified`; a diff
  over the bound returns `diff.ErrTooLarge` as the tool's own error,
  which `agentloop`'s `ErrorPolicyReport` path renders back to the
  model as a `ToolErrorPrefix`-marked message instead of silently
  truncating a diff, since a truncated diff can misstate a change.
  Not privileged: it reads two pieces of content and writes nothing.
  `ExecutionProfile.Class` is `tools.ExecutionClassRead`.
- One shared sentinel, `ErrBadArguments`, for a `DecodeArguments`
  call that cannot parse its raw bytes into the tool's typed
  argument struct, or a `Run` call whose `tools.InOut.Value` is not
  that struct. This is a separate family from the existing
  `ErrBadCommand`: `ErrBadCommand` covers the JSON-command,
  raw-string-payload tools (`RoomTool`, `HeartbeatTool`, and the
  rest); the five tools here take a typed, schema-validated argument
  struct instead of a command string, so they get their own sentinel
  rather than overloading `ErrBadCommand`'s meaning.
- Argument and result types, each a plain struct with `json` tags,
  one per tool: `WorkspaceReadArgs{Path string}`,
  `WorkspaceWriteArgs{Path, Content string}`,
  `WorkspaceListArgs{Path string}`, `WorkspaceStatArgs{Path string}`,
  `DiffArgs{Path, Content string}`, `WorkspaceEntry{Name string;
  IsDir bool}`, `WorkspaceFileInfo{Name string; Size int64; IsDir
  bool; ModTime time.Time}`.

Outside:

- `envfile.Load` as a tool. A parsed dotenv map is close to a
  credential-exfiltration primitive: most dotenv files hold API keys
  or tokens, and a model asking to read the parsed map gets no
  legitimate benefit `envfile` does not already give the caller's own
  process at startup. `envfile` stays a caller-side helper, wired
  before a subagent runs, never exposed as a model-reachable tool.
  This is a deliberate no, not an oversight; add a tool only if a
  caller names a real, reviewed use case.
- Diffing two arbitrary paths inside one workspace, or diffing across
  two different workspaces. `DiffTool` covers the "preview this
  write" case, the one a `WorkspaceWriteTool` caller actually needs.
  Add a two-path or two-workspace variant only when a caller names
  the need.
- Spooling an oversized read or diff result automatically.
  `spool.SpoolTool` already wraps any `tools.Tool` generically,
  including every tool this addendum adds, since `spool` imports only
  `tools`. Forcing every file tool through `spool.SpoolTool` by
  default would need a new `subagent` -> `spool` import edge for a
  concern `spool` already covers at the composition site. A caller
  that wants spooling wraps `spool.SpoolTool(name, maxBytes, store,
  subagent.WorkspaceReadTool(...))` itself, the same way it already
  composes `SpoolTool` over any other oversized-result tool.
- Any bound on `WorkspaceWriteTool`'s content size beyond what
  `agentloop`'s own `schema.MaxPayloadBytes` (64 KiB) already caps
  when the tool runs through `agentloop`'s `call.Arguments` validation
  gate. A direct `tools.Registry.Run` caller, outside `agentloop`,
  gets no such cap; that caller supplies its own bound the way it
  already must for every other tool argument.
- Tracking the in-flight `docs/plans/workspace.md` migration to
  `os.Root`, `Close`, `Options`, `ReadFileLimit`, and `ErrTooLarge`.
  This addendum targets the shipped surface locked in
  `api/workspace.txt` today: `Open`, `Root`, `ReadFile`, `WriteFile`
  (with `perm`), `List`, `Stat`, `ErrEscape`. When that migration
  lands, `WorkspaceWriteTool`'s hardcoded `0o600` argument to
  `WriteFile` drops along with the parameter, and
  `WorkspaceReadTool`'s own `maxResultBytes` cap stays as a
  second, tool-level bound layered under any workspace-level one.
  Both are follow-up edits to this addendum's code, not a blocker on
  it.

### Who supplies the Workspace

`ws *workspace.Workspace` is a constructor argument, never a
per-call, model-supplied value. The caller assembling a subagent's
`tools.Registry` calls `workspace.Open(root)` once, with a root path
it chose (a task's scratch directory, a repository checkout, a
sandbox mount), and passes the resulting `*Workspace` into each of
these five constructors. A model never sees `root`, never sees an
absolute path outside it, and never picks which root to bind: it
supplies only the relative-path argument each `Run` call reads out
of its decoded argument struct. `ws.resolve` (unexported, called by
every `Workspace` method) still rejects a model-supplied absolute
path or a `..`-traversal that would resolve outside `root`, so
confinement holds even if a caller's schema or decode step lets an
unexpected path string through; the tool-level typing is a second
layer, not the only one.

### Privilege model

`tools.PrivilegedTool` and `tools.Scope` already carry the risk
model this addendum needs; nothing new is invented.

- Four of the five tools (`WorkspaceReadTool`, `WorkspaceListTool`,
  `WorkspaceStatTool`, `DiffTool`) implement no `Privileged` method
  at all, so `tools.IsPrivileged` reports `false` for them, and
  `tools.Scope.Allowed` treats them like any other non-privileged
  tool: allowed by default, or allowed whenever a non-empty
  `Allowlist` names them.
- `WorkspaceWriteTool` implements `Privileged() bool` returning
  `true`. Per `tools.Scope.Allowed`'s documented rule, a privileged
  tool is denied unless its registered name appears in
  `ScopeOptions.Allowlist`; a caller opts a subagent into write access
  by naming the write tool's registration name there, the same
  mechanism that already gates any other privileged tool in this
  module.
- A caller that additionally wants synchronous human approval before
  a write runs composes the existing `ScopeOptions.Approve` and
  `ScopeOptions.ApprovalThreshold` fields, unchanged by this
  addendum: setting `ApprovalThreshold: tools.ExecutionClassWrite`
  routes every `tools.ExecutionClassWrite`-and-above call, including
  `WorkspaceWriteTool`'s, through `Approve` before `RunScoped` runs
  it. `WorkspaceWriteTool` needs no code of its own for this; it only
  needs to publish `ExecutionProfile.Class = ExecutionClassWrite`
  through `tools.ProfiledTool`, which it does.
- No file tool implements `tools.ProfiledTool` and
  `tools.PrivilegedTool` both flagging the same risk in two places;
  `Privileged` marks the allowlist gate, `ExecutionProfile.Class`
  marks the approval-threshold rank. `WorkspaceWriteTool` carries
  both, matching `tools.execution_profile.go`'s own documented
  distinction between the two mechanisms.
- Residual risk: unprivileged whole-root read access is real, and
  this addendum does not close it. `WorkspaceReadTool`,
  `WorkspaceListTool`, `WorkspaceStatTool`, and `DiffTool` each read
  any path under the bound root with no allowlist gate, the same way
  `envfile.Load` reads a dotenv file the Scope section above refuses
  to expose as a tool. `workspace` carries no secret-path denial of
  its own today; that is `docs/plans/workspace.md`'s still-unbuilt
  change two, `Options.Deny *secretpath.Matcher`. A root bound to "a
  repository checkout," the same example this addendum's "Who
  supplies the Workspace" section names, routinely holds `.env`,
  `credentials.json`, or a private key. An unprivileged
  `WorkspaceReadTool` over that root is the same exfiltration
  primitive the envfile section refuses to build, reached through a
  different door. Mitigation, until `workspace` change two ships:
  the caller binds these four tools only to a disposable or
  already-reviewed root that holds no credential-bearing path, the
  same discipline it would need for any other unprivileged
  file-reading tool. A caller that cannot make that guarantee waits
  for `workspace`'s secret-path denial, or privileges these four
  tools itself by wrapping them behind its own `Privileged() bool`
  the way `WorkspaceWriteTool` does, before offering them to a model.

### API

```go
func WorkspaceReadTool(name string, ws *workspace.Workspace, maxResultBytes int) tools.Tool
func WorkspaceWriteTool(name string, ws *workspace.Workspace) tools.Tool
func WorkspaceListTool(name string, ws *workspace.Workspace) tools.Tool
func WorkspaceStatTool(name string, ws *workspace.Workspace) tools.Tool
func DiffTool(name string, ws *workspace.Workspace, maxLines int) tools.Tool

var ErrBadArguments error

type WorkspaceReadArgs struct {
	Path string `json:"path"`
}
type WorkspaceWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type WorkspaceListArgs struct {
	Path string `json:"path"`
}
type WorkspaceStatArgs struct {
	Path string `json:"path"`
}
type DiffArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type WorkspaceEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}
type WorkspaceFileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}
```

Every unexported struct behind these constructors (for example
`workspaceReadTool`, `workspaceWriteTool`, matching this package's
existing per-tool naming: `roomTool`, `heartbeatTool`) implements
`tools.Tool` (`Name`, `Run`), `tools.SchemaTool` (`ParameterSchema`,
`DecodeArguments`), and `tools.ProfiledTool` (`ExecutionProfile`);
`WorkspaceWriteTool`'s struct additionally implements
`tools.PrivilegedTool` (`Privileged`). Every `ParameterSchema` is a
flat JSON Schema object, `additionalProperties: false`, with only
`string`-typed properties, well under `schema.MaxSchemaBytes` and
`schema.MaxSchemaDepth`, so `agentloop.New`'s `schema.Compile` call
never fails `ErrInvalidSchema` for any of the five.

`policy/layers.json` grants `subagent` the additional edges `diff`
and `workspace`, added to the existing row alphabetically. No other
edge changes; `envfile` gets no row addition to `subagent` since it
gets no tool.

### Tests

New file `subagent/subagent_test/filetools_test.go`, package
`subagent_test`:

- `TestWorkspaceReadTool`: a real `workspace.Workspace` over
  `t.TempDir()`. Reads an existing file back exactly. A missing file
  returns an error. A `maxResultBytes` smaller than the file's size
  still returns the full content from `Run` (the cap is a published
  `MaxResultBytes`, enforced by `agentloop`'s `render`, not by `Run`
  itself); assert `tools.ResultBudgetOf` on the returned `tools.Tool`
  reports the configured value and `true`. A second sub-case builds
  the tool with `maxResultBytes: 0` and asserts `tools.ResultBudgetOf`
  returns `false`, pinning the "a non-positive value publishes no
  budget" rule this addendum's Scope section states.
- `TestWorkspaceWriteTool`: writes a new file and an existing file,
  in both cases reading the result back with `os.ReadFile` and
  asserting content and mode `0o600`. Asserts `tools.IsPrivileged`
  reports `true` on the returned tool.
- `TestWorkspaceListTool` and `TestWorkspaceStatTool`: table-driven
  over a directory tree fixture with a file and a subdirectory,
  asserting the decoded `WorkspaceEntry`/`WorkspaceFileInfo` fields.
- `TestDiffTool`: an existing file diffed against changed content
  renders a unified diff matching `diff.Unified`'s own output for the
  same inputs. A not-yet-existing path diffs against empty content.
  A diff over `maxLines` returns an error matching
  `errors.Is(err, diff.ErrTooLarge)`.
- `TestFileToolsEscape`: table-driven over all five tools, covering
  both rejected-input shapes "Who supplies the Workspace" names. One
  `..`-traversal case per tool (five rows), plus one absolute-path
  case shared across the five (a sixth row, or one absolute-path
  sub-case per tool if that reads clearer as a table). Every row
  asserts the tool's returned error matches
  `errors.Is(err, workspace.ErrEscape)`. `WorkspaceReadTool` alone is
  not the pin: each tool wraps `ws.resolve`'s error through its own
  argument-struct path, and this table proves none of the four other
  wrappers swallows or reshapes `ErrEscape` on the way out, closing
  the gap the "second layer, not the only one" framing in "Who
  supplies the Workspace" claims but does not, by itself, test.
- `TestFileToolsBadArguments`: table-driven, two cases per tool.
  Case one: `DecodeArguments` on malformed JSON returns an error
  matching `errors.Is(err, subagent.ErrBadArguments)`. Case two:
  `Run` called directly with a `tools.InOut{Value: "wrong-type"}`,
  bypassing `DecodeArguments` the way a direct `tools.Registry.Run`
  or `RunScoped` caller can, returns an error matching
  `errors.Is(err, subagent.ErrBadArguments)` rather than panicking on
  a failed type assertion.
- `TestFileToolsSchemaCompile`: table-driven over all five tools,
  asserts `schema.Compile(t.(tools.SchemaTool).ParameterSchema())`
  succeeds. This is the direct pin for the `agentloop.New`
  `ErrInvalidSchema` concern this addendum's Scope section states.
- `TestFileToolsThroughAgentloop`: builds a `tools.Registry` holding
  all five tools plus a stub `provider.Completer` that requests one
  `WorkspaceWriteTool` call inside a scope allowlisting both the
  write tool and `DiffTool`, then one more turn requesting `DiffTool`
  over the written file, and asserts
  both round trips through `agentloop.Run` succeed. This is the
  integration pin for "these tools compose cleanly with the
  agentloop schema-validation gate" the addendum's Goal states.
- `TestFileToolsThroughAgentloopScopeDeniesWrite`: the same wiring,
  minus the write tool's name in `ScopeOptions.Allowlist`; the
  requested write call fails with `tools.ErrScopeDenied`, surfaced
  through `agentloop`'s reported tool error.

### Verification

- `make verify` passes; `subagent` and the module total hold the 85
  floor.
- `go test -race ./subagent/... ./agentloop/...` passes.
- `make api-update` lands the `api/subagent.txt` diff for the five
  new tools, the new sentinel, and the new argument/result types, in
  the same change as the code.
- `python3 scripts/check_deps.py` passes against the new `diff` and
  `workspace` edges on the `subagent` row.
- `python3 scripts/check_plan.py`, `check_prose.py`, and
  `check_labels.py` pass against this addendum.
- `docs/packages/subagent.md` and `AGENTS.md`'s `subagent/` entry
  gain the five new tools and `ErrBadArguments` in the same change
  as the code, following `docs/plans/TEMPLATE.md`'s API-surface
  discipline.

### Gap fix: export the mailbox-capacity sentinel

Status: shipped. `ErrInvalidCapacity` is declared in
`subagent/mailbox.go`, wrapped by `NewMailbox`, locked in
`api/subagent.txt`, and covered by
`TestNewMailboxRejectsBadCapacity` in
`subagent/subagent_test/mailbox_test.go`.
