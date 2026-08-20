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
- `subagent` holds a mutation-kill floor of 94, in
  `scripts/mutation_denylist/subagent.json`. Run `make mutation-gate`
  to check it.

## File tools addendum

Status: superseded by the corrected shape below.
`docs/plans/agents/phase71_filetools.md` records why: the shipped
five constructors took a bare `*workspace.Workspace`, so nothing
stopped a caller from wiring an unrestricted workspace straight into
a model-facing tool. This addendum now states the corrected,
enforcing contract. `envfile` still gets no tool; see its section
below.

### Goal

Give a subagent's tool registry four file-confined operations
(read, write, list, stat) plus one preview operation (diff), each
bound to one `*FileTools` set the deployment opens once, at wiring
time, from a root path and a mandatory secret-path deny list. A
model never chooses the sandbox root and never reaches a
secret-designated path; it chooses only a path inside the root the
caller already picked, and only among the paths the caller's deny
list did not refuse.

### Scope

Inside:

- Five tools, one file each, following the existing `*tool.go`
  pattern (`Name`, `Run`) plus `tools.SchemaTool`
  (`ParameterSchema`, `DecodeArguments`), so each tool is offerable
  through `agentloop.Definitions` without a caller-written adapter.
  `agentloop.Definitions` skips any tool with no published schema
  (see `AGENTS.md`'s `tools/` entry), so a schema-less file tool
  would be silently unreachable from a loop; these five tools close
  that gap for file access the way `spool.SpoolTool`'s `schemaCap`
  closes it for a spooled result.
- `FileToolOptions{Root string; Deny *secretpath.Matcher; MaxReadBytes int64}`
  and `(FileToolOptions) Validate() error`, in a new file
  `subagent/filetoolset.go`. `Validate` rejects a blank `Root` and a
  nil `Deny`. `Deny` is mandatory here even though
  `workspace.Options.Deny` is optional: `workspace` stays a general
  filesystem primitive with no policy opinion, and `subagent`'s file
  tools are the one place in this module that hands filesystem
  access to a model, so they hold their own, stricter requirement. A
  caller that truly wants no secret-path denial passes
  `secretpath.NewMatcher(nil)`, a non-nil matcher that matches
  nothing; see "Who supplies the FileTools" for the symlink
  consequence that choice still carries.
- `FileTools`, an opaque handle holding one opened
  `*workspace.Workspace`, and `OpenFileTools(opts FileToolOptions) (*FileTools, error)`,
  which validates `opts`, then opens the workspace through
  `workspace.OpenWith(workspace.Options{Root: opts.Root, MaxReadBytes: opts.MaxReadBytes, Deny: opts.Deny})`.
  `(*FileTools) Close() error` closes that workspace; see "Who
  supplies the FileTools" for the ownership rule.
- `ErrDenyRequired`, the sentinel `FileToolOptions.Validate` wraps
  and returns when `Deny` is nil.
- `WorkspaceReadTool(name string, ft *FileTools, maxResultBytes int) tools.Tool`.
  Reads one file at a caller-model-supplied path, relative to `ft`'s
  bound root. `maxResultBytes`, when positive, publishes
  `tools.ResultBudgetTool`, so `agentloop`'s `render` truncates an
  oversized read instead of flooding the model's context; a
  non-positive value publishes no budget. The bound workspace's own
  read limit is the second bound, and it applies after the read
  allocates. A path `ft`'s `Deny` refuses returns
  `workspace.ErrSecretPath` unchanged; see "Error mapping" below.
  Not privileged. `ExecutionProfile.Class` is
  `tools.ExecutionClassRead`.
- `WorkspaceWriteTool(name string, ft *FileTools) tools.Tool`.
  Writes one file at a caller-model-supplied path plus
  caller-model-supplied content. `workspace.WriteFile` creates the
  file with a fixed `0o600` mode; no `os.FileMode` argument reaches
  the model, matching the reasoning `docs/plans/workspace.md` states
  for dropping `WriteFile`'s `perm` parameter on a model-reachable
  path.
  The one truly dangerous operation this addendum adds: it mutates
  the filesystem inside `ft`'s root. Implements `tools.PrivilegedTool`
  returning `true`, so `tools.Scope.Allowed` denies it unless a
  caller's `ScopeOptions.Allowlist` names it explicitly; see
  "Privilege model" below. `ExecutionProfile.Class` is
  `tools.ExecutionClassWrite`.
- `WorkspaceListTool(name string, ft *FileTools) tools.Tool`.
  Lists one directory, relative to `ft`'s bound root; a blank path
  lists the root itself. Result is a JSON-encoded string of
  `[]WorkspaceEntry`, one `{name, is_dir}` pair per entry, sorted the
  way `ws.List`'s underlying `os.ReadDir` already sorts; see the
  "Gap fix: workspace list and stat results as a string" addendum
  below for the shipped JSON-string result shape. Not privileged.
  `ExecutionProfile.Class` is `tools.ExecutionClassRead`.
- `WorkspaceStatTool(name string, ft *FileTools) tools.Tool`.
  Stats one path, relative to `ft`'s bound root. Result is a
  JSON-encoded string of one `WorkspaceFileInfo` (`name`, `size`,
  `is_dir`, `mod_time`); see the same addendum below. Not privileged.
  `ExecutionProfile.Class` is `tools.ExecutionClassRead`.
- `DiffTool(name string, ft *FileTools, maxLines int) tools.Tool`.
  Previews a write: diffs the on-disk content at a
  caller-model-supplied path, read through `ft`'s bound workspace,
  against caller-model-supplied proposed content, through
  `diff.Unified`. A path that does not yet exist (the read returns a
  wrapped `fs.ErrNotExist`) diffs against empty content, so a
  new-file proposal renders the same way `diff -u /dev/null` would.
  Any other read error, including `workspace.ErrEscape` and
  `workspace.ErrSecretPath`, propagates unchanged. `maxLines` passes
  straight to `diff.Unified`; a diff over the bound returns
  `diff.ErrTooLarge` as the tool's own error, which `agentloop`'s
  `ErrorPolicyReport` path renders back to the model as a
  `ToolErrorPrefix`-marked message instead of silently truncating a
  diff, since a truncated diff can misstate a change. Not privileged:
  it reads two pieces of content and writes nothing.
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

### Error mapping

`workspace.ErrSecretPath` and `workspace.ErrEscape` propagate to the
model unchanged, wrapped only by the tool's own argument-path
context, not re-mapped to a third, generic refusal. The two
sentinels already carry distinct text ("path is a secret path" and
"symlink component" against "path escapes root"), so a model reading
`agentloop`'s `ToolErrorPrefix`-marked message already learns which
rule it hit, without a new mapping layer. Neither error leaks
anything the model did not already supply: both echo back only the
path argument the model itself sent in the failed call, never file
content and never another path. `DiffTool`'s doc comment already
states this "propagates unchanged" rule for `ErrEscape`; this
addendum extends the same rule to `ErrSecretPath` and applies it to
all five tools, not `DiffTool` alone.

Distinguishable sentinels open a separate, smaller leak: a model that
probes many candidate paths learns, from which sentinel comes back,
which of its guesses are secret-designated on this deployment.
`ErrSecretPath` marks a denied name, `fs.ErrNotExist` marks an absent
one, success marks a permitted one. This is an existence-plus-
classification oracle, not a content leak; the model learns at most
one bit per guess, and never a byte of file content. This addendum
accepts that trade-off, for the same ergonomic reason the
"propagates unchanged" rule above accepts the traversal-side channel:
a model that cannot tell "denied" from "absent" cannot tell a hard
sandbox boundary from a typo, and keeps probing instead of moving on.

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
  that wants spooling builds a `*spool.Spool` with `spool.NewSpool`
  and wraps `spool.SpoolTool(name, maxBytes, sp,
  subagent.WorkspaceReadTool(...))` itself, the same way it already
  composes `SpoolTool` over any other oversized-result tool.
- Any bound on `WorkspaceWriteTool`'s content size beyond what
  `agentloop`'s own `schema.MaxPayloadBytes` (64 KiB) already caps
  when the tool runs through `agentloop`'s `call.Arguments` validation
  gate. A direct `tools.Registry.Run` caller, outside `agentloop`,
  gets no such cap; that caller supplies its own bound the way it
  already must for every other tool argument.
- A narrower symlink policy than `workspace`'s own all-or-nothing
  walk. A non-nil `Deny`, mandatory here, refuses every symlink
  component unconditionally; see "Who supplies the FileTools." Adding
  an option to narrow that walk is `workspace`'s change to make, not
  this addendum's; it stays outside until a caller names the need.
- A backward-compatible shim for the five removed
  `*workspace.Workspace`-taking constructors. AGENTS.md forbids a
  compatibility shim for removed code; a caller updates its call
  sites to build a `*FileTools` through `OpenFileTools` instead. See
  "Breaking change and migration" below.

### Who supplies the FileTools

`ft *FileTools` is a constructor argument, never a per-call,
model-supplied value, matching the shipped design's "who supplies the
Workspace" rule for the same reason: a model must never choose the
sandbox root. What changes is who opens the workspace underneath.

The caller assembling a subagent's `tools.Registry` calls
`subagent.OpenFileTools(subagent.FileToolOptions{Root: root, Deny:
deny})` once, naming a root path it chose (a task's scratch
directory, a repository checkout, a sandbox mount) and a
`*secretpath.Matcher` naming the paths that root must never expose
to a model. `OpenFileTools` is the one place these five tools accept
a workspace from; none of the five constructors takes a bare
`*workspace.Workspace` anymore, so a caller cannot route around the
`Deny` requirement by opening its own, unchecked `workspace.Workspace`
and handing it in. `OpenFileTools` returns `subagent.ErrDenyRequired`
on a nil `Deny`, checked in `FileToolOptions.Validate` before any
filesystem call.

A model never sees `root`, never sees an absolute path outside it,
and never picks which root to bind or which paths `Deny` refuses: it
supplies only the relative-path argument each `Run` call reads out
of its decoded argument struct. The bound workspace's own path
resolution still rejects a model-supplied absolute path or a
`..`-traversal that would resolve outside `root`, so confinement
holds even if a caller's schema or decode step lets an unexpected
path string through; the tool-level typing is a second layer, not
the only one.

A mandatory `Deny` has one consequence every caller must accept
deliberately: it turns on `workspace`'s symlink walk unconditionally,
even for a `Deny` built from an empty pattern list. No model-driven
file tool built through `OpenFileTools` can read through any symlink
component, ever. Vendored dependency trees, `node_modules` layouts,
and dotfile trees that use symlinks all refuse under any of the five
tools. A caller with a symlink-free root, or one that accepts the
refusal, uses `OpenFileTools` directly; a caller that cannot accept
the refusal does not offer these five tools to a model.

`(*FileTools) Close()` closes the workspace `OpenFileTools` opened.
The caller that calls `OpenFileTools` owns the matching `Close`, the
same way a `workspace.Open` caller already owns `Close` today; a
`defer ft.Close()` right after a successful `OpenFileTools` call is
the expected shape. `Close` releases the `os.Root` file descriptor
`OpenFileTools` opened; skipping it leaks that descriptor for the
life of the process, exactly as skipping `workspace.Workspace.Close`
does today. No component inside `subagent` calls `Close` on the
caller's behalf, because no component inside `subagent` outlives the
caller's own `*FileTools` value; ownership stays with whoever opened
it, matching this package's `Mailbox` precedent, where `NewMailbox`'s
caller also owns the value everything else in the package only
borrows.

`FileTools` needs no mutex for concurrent `Run` calls against one
shared value. `os.Root`'s documented contract states its methods are
safe for concurrent use from multiple goroutines, and `FileTools`
adds no further mutable state over the `*workspace.Workspace` it
wraps. This matters in practice: a `flow` panel runs its member steps
concurrently in goroutines, so two file-tool steps in one panel that
share one `FileTools` rely on this guarantee holding.

### Breaking change and migration

`WorkspaceReadTool`, `WorkspaceWriteTool`, `WorkspaceListTool`,
`WorkspaceStatTool`, and `DiffTool` each change their second
parameter from `ws *workspace.Workspace` to `ft *FileTools`. Any
existing caller of the shipped signatures fails to compile until it
replaces its own `workspace.Open` or `workspace.OpenWith` call with
`subagent.OpenFileTools`, and supplies a `Deny`. This includes
`subagent/subagent_test/filetools_test.go`'s `openWorkspace` helper,
rewritten to `openFileTools`, opening through `OpenFileTools` with a
matcher built from an explicit, test-local deny list rather than
`secretpath.NewMatcher(nil)`, so the new secret-path tests below
exercise a real refusal.

No compatibility shim ships for the old signatures. AGENTS.md forbids
one for removed code, and a shim here would keep the exact hole this
addendum closes open for any caller that keeps using it.

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
- Residual risk, narrowed but not closed: unprivileged read access to
  every non-denied path under the bound root is still real.
  `WorkspaceReadTool`, `WorkspaceListTool`, `WorkspaceStatTool`, and
  `DiffTool` each read any path under the bound root that `Deny` does
  not name, with no allowlist gate. `Deny` is now mandatory, so a
  root bound to "a repository checkout" no longer exposes `.env` or
  `credentials.json` by default the way the shipped design did,
  provided the caller's `Deny` patterns actually name those paths.
  The residual risk is caller error: a `Deny` that omits a
  credential-bearing path, or a `Deny` built from
  `secretpath.NewMatcher(nil)` to opt out of the walk while accepting
  the symlink refusal, still exposes that path to the model. This
  addendum does not validate a caller's pattern list for
  completeness; `secretpath.Matcher` cannot know what a deployment
  considers secret. Mitigation: the caller reviews its root for
  credential-bearing paths before naming its `Deny` patterns, the
  same discipline `docs/plans/secretpath.md` already asks of a
  `Matcher` builder for any other consumer.

### API

```go
type FileToolOptions struct {
	Root         string
	Deny         *secretpath.Matcher
	MaxReadBytes int64
}
func (o FileToolOptions) Validate() error

type FileTools struct{ /* unexported */ }
func OpenFileTools(opts FileToolOptions) (*FileTools, error)
func (f *FileTools) Close() error

var ErrDenyRequired error

func WorkspaceReadTool(name string, ft *FileTools, maxResultBytes int) tools.Tool
func WorkspaceWriteTool(name string, ft *FileTools) tools.Tool
func WorkspaceListTool(name string, ft *FileTools) tools.Tool
func WorkspaceStatTool(name string, ft *FileTools) tools.Tool
func DiffTool(name string, ft *FileTools, maxLines int) tools.Tool

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

`FileToolOptions.Deny` names `*secretpath.Matcher`, so `subagent`
needs a direct import of `secretpath`, not only the transitive
`workspace` -> `secretpath` edge. Every unexported struct behind the
five tool constructors (for example `workspaceReadTool`,
`workspaceWriteTool`, matching this package's existing per-tool
naming: `roomTool`, `heartbeatTool`) implements `tools.Tool` (`Name`,
`Run`), `tools.SchemaTool` (`ParameterSchema`, `DecodeArguments`),
and `tools.ProfiledTool` (`ExecutionProfile`); `WorkspaceWriteTool`'s
struct additionally implements `tools.PrivilegedTool` (`Privileged`).
Every `ParameterSchema` is a flat JSON Schema object,
`additionalProperties: false`, with only `string`-typed properties,
well under `schema.MaxSchemaBytes` and `schema.MaxSchemaDepth`, so
`agentloop.New`'s `schema.Compile` call never fails
`ErrInvalidSchema` for any of the five.

`policy/layers.json` grants `subagent` the additional edges `diff`,
`secretpath`, and `workspace`, added to the existing row
alphabetically. No other edge changes; `envfile` gets no row addition
to `subagent` since it gets no tool.

### Tests

Two files, package `subagent_test`, split to hold the 500-line file
cap. The shipped `filetools_test.go` already sits at 481 lines before
this addendum's changes, and the new `FileTools`-level cases below do
not fit inside it alongside the existing tool-behavior tests. The
`FileTools`-level cases move to a new file,
`subagent/subagent_test/filetoolset_test.go`; the tool-behavior cases
stay in the rewritten `subagent/subagent_test/filetools_test.go`.

Both files share the `openFileTools(t, deny []string) *FileTools`
helper, which replaces the old `openWorkspace` helper. It opens
through `subagent.OpenFileTools` with a `secretpath.NewMatcher(deny)`
matcher and registers `t.Cleanup` on `Close`; every existing `build`
func's parameter changes from `ws *workspace.Workspace` to
`ft *subagent.FileTools` to match the new constructor signatures. The
helper is declared once, in `filetools_test.go`, since both test
files share one `subagent_test` package.

In `subagent/subagent_test/filetoolset_test.go`:

- `TestOpenFileToolsValidatesOptions`: table-driven,
  red-green. A blank `Root` returns an error. A nil `Deny` returns an
  error matching `errors.Is(err, subagent.ErrDenyRequired)`. A blank
  `Root` and a non-nil `Deny` together still return the blank-root
  error, pinning `Validate`'s check order. A valid `Options` returns a
  non-nil `*FileTools` and a nil error; `t.Cleanup`s its `Close`. This
  is the direct pin for the gap this addendum closes: the shipped
  718d79b constructors let a caller build the five tools from a raw
  `*workspace.Workspace` opened with no `Deny` at all; the nil-`Deny`
  row is red against that shape and green once `OpenFileTools` and
  its mandatory `Deny` land.
- `TestFileToolsCloseIsIdempotent`: opens a `*FileTools`, calls
  `Close` twice, asserts both calls return `nil`, matching
  `workspace.Workspace.Close`'s own idempotent contract.
- `TestFileToolsDeniesSecretPath`: table-driven over all five tools,
  using `openFileTools(t, []string{"secret.env"})`. Each row calls
  the tool against `secret.env` and asserts the returned error
  matches `errors.Is(err, workspace.ErrSecretPath)`. A second
  sub-table repeats the same five rows against a permitted path in
  the same directory and asserts no `ErrSecretPath`, pinning that the
  matcher denies by name, not by directory membership alone. This
  test is not the pin for this addendum's gap: denial-when-a-pattern-
  matches is a `workspace`-level property, shipped and tested in
  commit f2f6028, before this addendum existed. It confirms the tool
  layer forwards that already-shipped denial without swallowing it, a
  real property this addendum must preserve, distinct from the gap
  `TestOpenFileToolsValidatesOptions` pins above.
- `TestOpenFileToolsSymlinkRefused`: builds a `*FileTools` over a
  root holding a symlink to a permitted file outside the deny list,
  through `openFileTools(t, nil)`. `WorkspaceReadTool` over the
  symlink's path returns an error matching
  `errors.Is(err, workspace.ErrSecretPath)`, pinning the "mandatory
  `Deny` refuses every symlink component unconditionally" rule "Who
  supplies the FileTools" states, even for an empty pattern list.
- `TestFileToolsConcurrentReadsSafeUnderClose`: race-covered. Several
  goroutines call `WorkspaceReadTool.Run` concurrently against one
  shared `*FileTools` while it stays open, joined before `Close` runs
  once. Every call returns its expected result or error, and
  `go test -race` reports no race, pinning the "no mutex needed" claim
  in "Close ownership."

In `subagent/subagent_test/filetools_test.go`:

- `TestWorkspaceReadTool`: `openFileTools(t, nil)` over
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
  both rejected-input shapes "Who supplies the FileTools" names. One
  `..`-traversal case per tool (five rows), plus one absolute-path
  case shared across the five (a sixth row, or one absolute-path
  sub-case per tool if that reads clearer as a table). Every row
  asserts the tool's returned error matches
  `errors.Is(err, workspace.ErrEscape)`. `WorkspaceReadTool` alone is
  not the pin: each tool wraps the resolve error through its own
  argument-struct path, and this table proves none of the four other
  wrappers swallows or reshapes `ErrEscape` on the way out, closing
  the gap the "second layer, not the only one" framing in "Who
  supplies the FileTools" claims but does not, by itself, test.
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
- `make api-update` lands the `api/subagent.txt` diff: the five tool
  constructors' changed signatures, `FileToolOptions`, `FileTools`,
  `OpenFileTools`, `(*FileTools) Close`, and `ErrDenyRequired`, in the
  same change as the code. The removal of the five old
  `ws *workspace.Workspace` signatures shows in the diff as a
  deletion, not a hidden overload.
- `python3 scripts/check_deps.py` passes against the new `diff`,
  `secretpath`, and `workspace` edges on the `subagent` row.
- `python3 scripts/check_plan.py`, `check_prose.py`, and
  `check_labels.py` pass against this addendum.
- `docs/packages/subagent.md` and `AGENTS.md`'s `subagent/` entry
  gain `FileToolOptions`, `FileTools`, `OpenFileTools`,
  `ErrDenyRequired`, and the five tools' new signatures in the same
  change as the code, following `docs/plans/TEMPLATE.md`'s
  API-surface discipline.
- `docs/plans/secrets.md`'s "Phase 71 owns the tool surface" section
  is corrected in the same change to match this addendum: the
  `envfile.LoadBytes` term is marked dropped, not met, with the
  reasoning "Envfile" in `docs/plans/agents/phase71_filetools.md`
  states.

### Gap fix: workspace list and stat results as a string

Status: shipped. `WorkspaceListTool.Run` and `WorkspaceStatTool.Run`
returned `[]WorkspaceEntry` and `WorkspaceFileInfo` directly in
`tools.Out.Value`, not a string. Every other `subagent` tool returns a
string. `agentrun`'s `chain` requires a string result, so
`runconfig.WorkspaceListKind` and `runconfig.WorkspaceStatKind` failed
every real `Runner.Run` with `ErrResultNotText`; see
`docs/plans/runconfig.md`'s phase 76 addendum. The fix JSON-encodes
each result into `Out.Value` as a string, matching every other tool's
convention. No standalone phase 77 plan file remains for this
contract.

### Gap fix: export the mailbox-capacity sentinel

Status: shipped. `ErrInvalidCapacity` is declared in
`subagent/mailbox.go`, wrapped by `NewMailbox`, locked in
`api/subagent.txt`, and covered by
`TestNewMailboxRejectsBadCapacity` in
`subagent/subagent_test/mailbox_test.go`.

### Gap fix: Deliver rejects an unsigned message

Status: shipped. `Deliver` calls `VerifySignature` and wraps the
failure in `ErrUnverified`, which `api/subagent.txt` locks.
`TestDeliverRejectsUnsignedMessage`,
`TestDeliverRejectsTamperedSignature`, and
`TestDeliverKeepsSignedMessage` in
`subagent/subagent_test/mailbox_test.go` cover it. The text below
records the gap the fix closed.

The `Mailbox` doc
(`subagent/mailbox.go:26`) says "Mailbox holds signed messages".
`Deliver` (`subagent/mailbox.go:43`) calls `msg.Validate()` only.
`Message.validateSignature` (`envelope/message.go:186`) returns nil
when `Signer` and `Signature` are both empty. So an unsigned message
enters the mailbox. `InboxTool.Run` (`subagent/mailbox.go:127`) then
hands the payload to a model with no provenance. AGENTS.md requires a
`Validate`-enforced rule behind every documented rule, so enforce the
doc.

#### Callers checked

Enforcement breaks no in-repo caller. Every call site delivers a
signed message today.

- `sendTool.Run` (`subagent/mailbox.go:101`) delivers a message it
  signed with its `identity.Identity`.
- `e2e/e2e_test/subagent_messaging_test.go:148` delivers a
  human-signed message.
- `subagent/subagent_test/mailbox_test.go` and
  `subagent/subagent_test/metamorphic_test.go` deliver
  `signedMessage` values. The one unsigned case
  (`mailbox_test.go:21`) already asserts an error.

#### Fix

`Deliver` calls `msg.Validate()`, then `msg.VerifySignature()`.
`VerifySignature` (`envelope/sign.go:35`) rejects an empty `Signer`
or `Signature` and checks the ed25519 signature over the canonical
bytes. The check needs no roster: the message carries its own public
key. Trust policy, meaning which signers to accept, stays with the
caller, as `envelope` documents. Run both checks before the lock and
before the capacity check, so the existing `ErrMailboxFull` order is
unchanged.

#### API change

This gap fix adds one exported symbol. `ErrUnverified` reports a
`Deliver` whose message fails signature verification: no signature,
or a signature that does not match.

`envelope` exports no sentinel for this case: `VerifySignature`
returns a bare `errors.New` (`envelope/sign.go:37`). So the new
symbol is required and duplicates nothing.

Pin the text and the wrapping. `ErrMailboxFull` and
`ErrInvalidCapacity` (`subagent/mailbox.go:20,24`) each carry the
`subagent: ` prefix in their own message. `ErrUnverified` follows that
house style, and `Deliver` wraps without re-prefixing.

```go
// ErrUnverified reports a Deliver whose message fails envelope
// signature verification. Test with errors.Is.
var ErrUnverified = errors.New("subagent: mailbox rejects an unverified message")

// inside Deliver:
return fmt.Errorf("%w: %v", ErrUnverified, err)
```

A caller matches the sentinel with `errors.Is` and still reads the
`envelope` detail. Run `make api-update` and commit the
`api/subagent.txt` diff in the same change.

## File tools removed

Status: shipped. See `docs/plans/agents/convergence.md`'s "Boundary
correction" section. The "File tools addendum" above added
`FileTools`, `OpenFileTools`, `WorkspaceReadTool`, `WorkspaceWriteTool`,
`WorkspaceListTool`, `WorkspaceStatTool`, `DiffTool`, their argument
structs, and `ErrDenyRequired` to `subagent`. This toolbox is a
concrete file-editing product surface: only a coding agent reads,
writes, lists, stats, and diffs source files. No other agent shape
this SDK targets — support, research, data-pipeline — needs it. The
correction removes these symbols, their support files, and the `diff`
package they depended on. `subagent`'s current scope is the "Scope"
section above: the eleven block-wrapper tools, `AsTool`, `SendTool`,
and `InboxTool`. `runconfig`'s matching `Kind` constants
(`WorkspaceReadKind`, `WorkspaceWriteKind`, `WorkspaceListKind`,
`WorkspaceStatKind`, `DiffKind`) are removed in the same change; see
`docs/plans/runconfig.md`'s matching addendum.

Update the `Mailbox` and `Deliver` comments to name the enforced
rule. Update the mailbox entries in `docs/packages/subagent.md` in
the same change.

#### Tests

Add the cases to `subagent/subagent_test/mailbox_test.go`. The first
two fail against today's code.

- `TestDeliverRejectsUnsignedMessage` — build a message that passes
  `Validate` with an empty `Signer` and an empty `Signature`. Assert
  `Deliver` returns an error matching `ErrUnverified`. Assert `Take`
  returns no message. Today `Deliver` returns nil and `Take` returns
  one message, so the case fails now.
- `TestDeliverRejectsTamperedSignature` — sign a message, then change
  its `Payload`. Assert `Deliver` returns an error matching
  `ErrUnverified`. Today the hex format stays valid, `Validate`
  passes, and the mailbox accepts the tampered message, so the case
  fails now.
- `TestDeliverKeepsSignedMessage` — assert the signed path still
  succeeds and `Take` returns the message. This case passes today. It
  guards against an over-strict check.

Both red cases assert the sentinel and the empty drain. So a mutation
that drops the `VerifySignature` call is killed, and the mutation
floor of 94 holds.

#### Verification for this gap fix

Land this gap fix as its own commit, separate from the `flow` retry
gap fix in `docs/plans/flow.md`. The two fixes share no file and no
package. One commit per fix keeps a revert granular.

Run `make verify`, `go test -race ./subagent/...`, and
`go test ./e2e/...`. `policy/layers.json` is unchanged: the
`subagent` row already lists `envelope`. Hold `subagent` coverage at
or above 85 and the mutation floor at 94. No conformance vector
changes: `envelope` message semantics are untouched.
