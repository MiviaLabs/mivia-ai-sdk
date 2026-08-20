# Convergence: mivia-agent onto mivia-ai-sdk

Status: program plan, revision six. Every decision is answered. The
plan is buildable.

Revision six corrects stage 3 and stage 6's scope. It narrows two
absorption targets, adds one reversed-direction item, and removes two
SDK-only packages that were never a convergence target. See "Boundary
correction (revision six)" below the resolved decisions.

This plan covers two repositories. The SDK is
`github.com/MiviaLabs/mivia-ai-sdk`. The consumer is
`github.com/MiviaLabs/mivia-agent`. The consumer imports zero SDK
packages today.

## Decision record

The user confirmed five decisions. They frame every stage below.

- Direction: the SDK absorbs the consumer's hardened implementations.
  The consumer never accepts a guarantee regression.
- Scope: full convergence, phased. Every duplicated concern resolves
  to one implementation.
- Parity bar: differential tests where the code is pure. Invariant and
  fault tests everywhere else.
- Order: structural cleanup lands before the first migration.
- User interface: `internal/ui` runs on mocks today. Its knobs are
  prepared for integration. The terminal interface inside
  `internal/cli` is deleted, not migrated.

## Resolved decisions

Three questions blocked work through revision four. The user answered
all three. Each answer is recorded in AGENTS.md.

### Third-party imports

Revisions two and three both read permission from the SDK's `go.mod`.
That was the wrong source. `go.mod` records what the module resolves.
AGENTS.md records what a package may import. Only AGENTS.md governs.

AGENTS.md permits four exceptions today: `a2aclient` and `a2aloopback`
for `a2a-go` and `grpc`, `mcp` for `modelcontextprotocol/go-sdk`,
`ledger` for `modernc.org/sqlite` behind the `ledger_sqlite` build tag
only, and `schema` for `santhosh-tekuri/jsonschema`.

Measured direct imports of every absorption target:

| Consumer package | Direct third-party | In SDK go.mod | Permitted by AGENTS.md |
| --- | --- | --- | --- |
| `ledger` | none | not applicable | yes |
| `provider` | none | not applicable | yes |
| `subagents` | none | not applicable | yes |
| `contextmgr` | none | not applicable | yes |
| `mcp` | modelcontextprotocol/go-sdk | yes | yes |
| `memory` | modernc.org/sqlite | yes | no; ledger-scoped, tag-gated |
| `skills` | go-toml, x/sys | x/sys indirect | no |
| `workflows` | go-toml, x/mod | x/mod yes | no |
| `hooks` | go-toml | no | not on converging path |

Decision one, as recorded in revision five: the SDK permits
`github.com/pelletier/go-toml/v2`, `golang.org/x/mod`, and
`golang.org/x/sys` as direct imports, scoped to named packages.

Revision six narrows this. "Boundary correction" above walks the
grep evidence: `x/mod`'s sole caller stays in the consumer's
`controller`-adjacent verifier and never converges; `go-toml`'s
callers are all format-parsing loaders that hand a parsed struct to
an SDK-shaped registry, so under the "SDK accepts parsed Go structs"
rule the SDK never needs it either. `x/sys` stays granted, rescoped to
`workspace/` rather than `skills/`, since its one caller is filesystem
confinement hardening, not skill matching.

Decision one, corrected: the SDK permits `golang.org/x/sys` as a
direct import, scoped to `workspace/` (or a workspace-adjacent leaf
decided at absorption time). `github.com/pelletier/go-toml/v2` and
`golang.org/x/mod` are not granted; no absorption target needs them.
Each absorption still gets its own plan review, per the existing rule.

The exception list is not one file. Three sites enforce it, and a
change to one alone creates drift. This corrects revision five's
mistaken site list: `scripts/check_gomod.py` does not exist, and
Semgrep holds no per-package scoped rule. The real sites are:

- AGENTS.md holds the prose rule.
- `policy/thirdparty.json` holds the actual per-package allowlist: a
  map from package name to its permitted modules and its build tag,
  if any (for example `"ledger": {"modules":
  ["modernc.org/sqlite"], "tag": "ledger_sqlite"}`).
- `scripts/check_thirdparty.py` is the one gate; its own docstring
  calls it "the one site that owns third-party truth." It runs seven
  checks: per-package imports against `policy/thirdparty.json`, a
  residual raw-text scan, policy shape, `go.mod` direct-require
  equality, the `go.sum` closure lock, `go mod tidy -diff` identity,
  and a replace/exclude/retract rejection.
- `semgrep/sdk-standards.yml` holds one rule,
  `sdk.go.stdlib-only-imports`: a blanket forbid on any third-party
  import path, with one flat `pattern-not-regex` exemption list
  covering every currently-allowed module. The exemption is
  module-level, not per-package; Semgrep cannot tell which SDK package
  is allowed which module. `policy/thirdparty.json` and
  `check_thirdparty.py` own that finer-grained rule alone.

So no exception lands ahead of its absorption. Landing a new
per-package exception needs a `policy/thirdparty.json` row, and, only
if the module is not yet in any row, a new `pattern-not-regex` line in
`semgrep/sdk-standards.yml`. Widening an already-exempted module (for
example `modernc.org/sqlite`) to a new package needs only the
`policy/thirdparty.json` row; Semgrep needs no change, since its
exemption already covers the module. Land the policy row, the Semgrep
line if needed, and the import in the same commit.

The `x/sys` item is not cosmetic. The `skills` hardening the SDK was
asked to absorb is the `x/sys` code, but "Boundary correction" above
found its real home is `workspace/`, not `skills/`. Landing it needs a
new `policy/thirdparty.json` row for `workspace` naming
`golang.org/x/sys`, and a new `pattern-not-regex` line in
`semgrep/sdk-standards.yml`, since no current exemption covers that
module yet. `internal/skills/resource_file_linux.go` implements
fail-closed resource rebinding with `unix.Dup`, `unix.Fstatat`,
`unix.Openat`, `unix.AT_SYMLINK_NOFOLLOW`, and `unix.O_NOFOLLOW`.
Re-implementing it on `syscall` or `os.Root` yields a different
guarantee. The decision record forbids that.

The `hooks` row is inert. Its TOML use sits in `config.go` and
`validate.go`. Only the protocol codec converges, and
`internal/hooks/protocol.go` imports the standard library alone.

### The sqlite scope

Decision two: `modernc.org/sqlite` widens to named packages and keeps
its build tag. `ledger`, `memory`, and the usage store may import it,
each behind the tag. A build without the tag stays stdlib-only, so the
SDK stays embeddable.

Three absorptions needed this answer. The `memory` absorption carries a
sqlite backend. The usage-path absorption carries a durable event
store in `internal/storage`, which imports sqlite. The `ledger`
absorption keeps the tag rather than dropping it.

The tag stays. Dropping it would weaken a gate, which AGENTS.md
forbids. Widening the package scope while keeping the tag is a
recorded exception, not a weakened gate.

The same mechanism applies, corrected as above: `policy/thirdparty.json`
holds `ledger`'s row for `modernc.org/sqlite` under the
`ledger_sqlite` tag today. `semgrep/sdk-standards.yml`'s single
`stdlib-only-imports` rule already exempts `modernc.org/sqlite` at the
module level. Widening to `memory` and the usage store needs only a
new `policy/thirdparty.json` row per package, each under the same
tag; Semgrep needs no change, since its exemption already covers the
module.

### The scheduler capability gap

Decision three: the SDK adds scheduler enumeration, scheduler
cancellation, and a cancelled ledger state. It is a named stage 6
item. Stage 5b follows it. The ports layer converges fully.

`internal/uikit/ports/settings_automations.go` records the gap in its
own comments. It states that the SDK scheduler has no cancellation,
that `scheduler.Job` is a bare closure with only a string identifier,
and that `Scheduler` entries are unexported and unenumerable.
`api/scheduler.txt` confirms all three. It exports `Add`, `Remove`,
and `Run`, and `Job` is `func(ctx) error`.

So `ports.AutomationSettings` cannot be implemented on SDK types
today. Stage 5b below waits for the stage 6 scheduler item.

## Boundary correction (revision six)

Revision five's stage 6 list named absorption targets by shared
package name. Some named pairs share a name and nothing else. One SDK
package, `subagent/`'s file-editing tool set, and its sole dependency
`diff/`, were never a consumer duplicate; they are coding-agent
product surface that should not have entered the SDK. This section
corrects both errors with evidence, then edits the affected tables.

### `subagent/` and `diff/`: an SDK-only correction, no consumer wait

`subagent/` holds two concerns. `AsTool`, `SendTool`, `InboxTool`, and
eleven typed tool wrappers (`MemoryTool`, `ChannelTool`,
`ProviderTool`, `SchedulerTool`, `RoomTool`, `LedgerTool`, `FlowTool`,
`TriggerTool`, `HeartbeatTool`, `DiscoveryTool`,
`ProviderRegistryTool`) each expose one SDK block as a tool. That
concern belongs in the SDK. `subagent/filetoolset.go`,
`workspacereadtool.go`, `workspacewritetool.go`,
`workspacelisttool.go`, `workspacestattool.go`, and `difftool.go`
expose `FileTools`, `WorkspaceReadTool`, `WorkspaceWriteTool`,
`WorkspaceListTool`, `WorkspaceStatTool`, and `DiffTool`: a concrete
file-editing toolbox wrapping `workspace.Workspace` and `diff.Unified`.

Apply the AGENTS.md building-block test: would a support agent, a
research agent, or a data-pipeline agent need a bundled file-diff
tool? No. Only a coding agent edits source files and previews the
edit as a diff. This toolbox is `mivia-agent` product surface riding
inside the SDK.

`diff/` exists to serve exactly one production caller. A repo-wide
search for `sdk/diff"` finds three matches outside worktree copies:
`subagent/difftool.go`, `subagent/subagent_test/filetools_test.go`,
and `diff/diff_test/diff_test.go`. No other package imports it. Its
sole reason to exist is `subagent/difftool.go`.

Decision: `FileTools`, `WorkspaceReadTool`, `WorkspaceWriteTool`,
`WorkspaceListTool`, `WorkspaceStatTool`, `DiffTool`, and their
support files leave `subagent/`. `diff/` deletes as a package. The
eleven block wrappers plus `AsTool`, `SendTool`, and `InboxTool` stay.

This correction needs no consumer coordination and no rollback
ordering. The consumer imports zero SDK packages today, so no import
lands before a deletion; there is nothing to roll back to. The
consumer already owns a superset replacement: `internal/diff`
(`Compute`, `Result`, `Op`, `Kind`, `Stats`, `Options`,
`FormatUnifiedAt`, `TruncateUTF8`, a context deadline) backs
`internal/tools/write.go` and `internal/tools/edit.go`, the consumer's
real file-edit tools. No consumer-side construction is needed.

This change can land in `mivia-ai-sdk` alone, ahead of stage 0, in one
commit. No gate below checks these files, so skipping any leaves it
silently stale:

- Delete the six symbols and their files.
- Update `subagent/wire.go` and the tests that reference them.
- Run `make api-update` to drop them from `api/subagent.txt`; delete
  `api/diff.txt`.
- Remove `diff/`'s row and `subagent`'s `diff` entry from
  `policy/layers.json`.
- Delete `docs/packages/diff.md` and `docs/plans/diff.md`.
- Remove `docs/packages/diff.md`'s entry from `docs/README.md`'s
  package list.
- Remove `diff` from `docs/architecture.md`'s module list and its
  leaf-package sentence, and remove the `subagent --> diff` edge from
  the Mermaid package-map diagram.

None of `check_plan.py`, `check_docs.py`, or `check_orphan_packages.py`
enforce these four doc sites; treat the list above as mandatory
regardless. It is not a stage 6 absorption; it has no
"import first, deletion second" step, because no SDK-to-consumer
import exists to sequence against.

#### Addendum: `runconfig`'s `Kind` vocabulary shares the same defect

The "no consumer coordination" claim above checked `mivia-agent`
imports and `diff"` imports. It missed an in-repo consumer:
`runconfig/blocks.go` locks `WorkspaceReadKind`, `WorkspaceWriteKind`,
`WorkspaceListKind`, `WorkspaceStatKind`, and `DiffKind` as exported
`Kind` constants, each existing solely to route a config document's
tool binding to the file toolbox this correction deletes.
`runconfig_test` builds fixture tools against the real
`subagent.WorkspaceReadTool`/`WorkspaceWriteTool`/`WorkspaceListTool`/
`WorkspaceStatTool`/`DiffTool` to exercise dispatch for exactly these
five kinds.

The same building-block test applies one level further: a `Kind`
naming a coding-agent-specific tool that no longer exists in the SDK
is the same product-surface leak this correction is written to close.
Decision: delete `WorkspaceReadKind`, `WorkspaceWriteKind`,
`WorkspaceListKind`, `WorkspaceStatKind`, and `DiffKind` from
`runconfig.Blocks` and its `internalKinds` set, in the same commit as
the `subagent`/`diff` deletion above. Rewrite `runconfig_test`'s
fixtures for those five kinds against a minimal fake `tools.Tool` that
proves `Blocks.Set`/dispatch, since `runconfig`'s production code
(`blocks.go`, `loader.go`, `runner.go`, `steptool.go`) never imports
`subagent` and does not need a real workspace or diff tool to prove
its own dispatch logic. `runconfig`'s other ten `Kind`s are unaffected
and keep their existing fixtures. `make api-update` picks up the
`runconfig` surface change in the same pass as `subagent`'s.

This is still a pure SDK-internal correction. `runconfig` itself has
no consumer either — its `pending_wiring.json` entry names "external
application code (outside this module)" as the eventual caller, and
none exists yet.

### `events/`: the shared name hides two different concerns

SDK `events.Event` is `{Name Name, Data string}`, a generic
string-payload reaction bus. Inside this module, `agent`, `flow`,
`ledger`, `dispatch`, `agentrun`, `agentloop`, `scheduler`, `machine`,
and `heartbeat` all import it; it is core internal wiring, not an
unused leaf.

The consumer's `internal/events` defines `Kind`, a string enum with
over thirty constants (`KindAssistant`, `KindWorkflowRunStarted`,
`KindCacheUsage`, `KindTokenUsage`, `KindPrefixReset`, and more), plus
`Delivery`, `MetricsAdapter`, `CacheUsageEvent`, `TokenUsageEvent`,
and `CompactionEvent`, each with its own `Validate`. `internal/agent`,
`internal/hub`, `internal/chat`, eleven-plus files under
`internal/cli`, and `internal/workflows/localengine` all import it.
This is a typed, UI-facing and workflow-facing vocabulary for
rendering one turn, not a generic bus.

Decision: `events/` and `internal/events` never converge. `events/`
stays the generic bus. `internal/events` stays the consumer's typed
projection of agent, workflow, and provider activity onto UI event
kinds.

`agentloop` needs no new typed surface for this projection.
`agentloop.Options` already exposes `Audit` (one `AuditRecord` per
completed `Completer` turn and per tool call) and a positive
`HeartbeatInterval` paired with a non-nil `Bus` (iteration, tool-call,
and heartbeat `events.Name` progress events). A consumer-side
projection can build every `internal/events.Kind` the union defines
from bus events plus `AuditRecord`: cache and token usage ride on
`AuditRecord`'s embedded `provider.Response` fields; workflow kinds
ride on `flow`'s own `events.Name` constants once the flow absorption
below lands.

### `workspace/`: confinement converges, the namespace registry never does

SDK `workspace/` confines file access with `os.Root`
(`workspace/confine.go`, `read.go`, `write.go`, `list.go`, `stat.go`):
`Open`, `OpenWith`, `ReadFile`, `ReadFileLimit`, `WriteFile`, `List`,
`Stat`, plus a `secretpath` deny matcher. Every access opens through
one `os.Root` descriptor, confined once, for the life of the
`Workspace`.

The consumer's `internal/workspace` holds two concerns in one
package. `root.go` defines `Root`: `Open`, `OpenFullDisk`, `Resolve`,
`evalExistingPrefix`, `isUnder`. `Resolve` cleans a path, evaluates
symlinks, and string-prefix-checks it against the root, then returns
a plain path string; the caller opens that path itself, later, with
its own call. This is a resolve-then-open pattern, not `workspace/`'s
open-once-confine-forever pattern, and it carries a TOCTOU window
between `Resolve` and the caller's own open. `internal/workspace.Root`
backs `internal/tools`' real file-edit tools directly:
`write.go`, `edit.go`, `delete.go`, `read.go`, `list_dir.go`, and over
twenty more callers.

`namespace.go`, in the same package, defines `Namespace = ".mivia"`,
`SkillsDir`, `SessionsDir`, `WorktreesDir`, `ContextStorePath`, and
`MemoryDBPath`: mivia's own on-disk layout convention. It has no
confinement content.

This is the one item in the program where the decision-record
direction runs in reverse. The decision record says the SDK absorbs
the consumer's hardened implementation; here the SDK's `os.Root`-based
confinement already carries the stronger guarantee than the
consumer's resolve-then-open check. Decision: `internal/workspace`'s
`Root` converges onto SDK `workspace/`, direction consumer-to-SDK.
`namespace.go` never converges; it is mivia's own path convention, and
no second SDK consumer needs it. See the new stage 4 item below.

### Third-party import exceptions narrow

Grep evidence in the consumer, current as of this revision:

- `golang.org/x/mod`'s only caller is
  `internal/workflows/verifier/sandbox_modules.go`, a Go-module
  verification sandbox. It sits with `controller`'s product-specific
  gate logic, which never converges regardless of how the
  `flow`/`workflows` seam question above resolves. The SDK gains no
  caller for `x/mod`.
- `github.com/pelletier/go-toml/v2`'s callers are nine files under
  `internal/config`, plus `internal/hooks/config.go`,
  `internal/workflows/delivery/prtitle.go`,
  `internal/workflows/definition/decode.go`, and
  `internal/skills/resources.go`. `internal/skills/resources.go`
  decodes a `resourceManifest` into a `[]ResourceDescriptor` and calls
  no registry method itself; that decoded list only reaches the
  registry indirectly, as one field (`Definition.Resources`) of the
  `Definition` that `internal/skills/loader.go:74` assembles and
  passes to `Registry.Register` (`internal/skills/skills.go:49`,
  named `Register`, not `Add`). `loader.go` itself imports no
  `go-toml`. `internal/workflows/delivery/prtitle.go` decodes
  `pr-title.toml` into a `PRTitlePolicy` struct and calls its own
  `Validate`, with no SDK call at all. Most `internal/config` callers
  build a consumer-internal `Resolved` config and never cross into an
  SDK call. That most callers never reach the SDK, and that the one
  that does reach a registry does so only indirectly through a struct
  field, is a stronger argument against an SDK `go-toml` dependency
  than a direct "decodes, then registers" claim would be: the SDK's
  own `skills.Registry.Add` already takes a parsed `skills.Skill`, not
  TOML text, so `skills/` parses nothing itself either way.
- `golang.org/x/sys`'s caller is
  `internal/skills/resource_file_linux.go`'s unexported
  `openDeclaredResourceFile`, called only from
  `internal/skills/resources.go`. This is filesystem-confinement
  hardening (`unix.Openat`, `unix.AT_SYMLINK_NOFOLLOW`), not
  skill-matching logic. It belongs with the workspace confinement item
  above, not with the `skills` absorption.

Decision: drop `golang.org/x/mod` and `github.com/pelletier/go-toml/v2`
from the program's dependency-exception list. Neither remaining
absorption target needs them under the corrected boundary. Keep
`golang.org/x/sys` as a granted exception, rescoped to `workspace/`
(decide the exact package at absorption time), not `skills/`.
`skills/` absorbs with no third-party import.

This edits the "Third-party imports" table and prose below, and, at
absorption time, `policy/thirdparty.json`. No absorption code lands in
this revision; this revision only corrects the plan.

### `flow` versus `internal/workflows`: the seam is not clean

`internal/workflows/localengine` (19 out-edges, per the evidence base)
holds `Engine.Start`, `admitInvocation`, `launch`, `Wait`, plus
`engine_admission.go`, `fence.go`, `resume_admission.go`, and
`engine_resume.go` / `engine_resume_worktree.go`: an admission ladder,
a fenced lease, and resume-from-log. On name and file list alone this
matches `flow`'s own admission, lease, and resume concerns.

Reading the code past the file names shows the two packages are not
separable today. `Engine` (`localengine/engine.go`) holds
`ctrl *controller.LinearController` as a core field, and
`Engine.launch` calls
`controller.RunWithCancelReconciliationRetry(runCtx, ctrl.Run)`
directly. `engine_resume.go`, named above as a converging file, builds
and returns a `*controller.LinearController` through
`controller.NewLinearController`, `controller.Admission`, and
`controller.StepRuntime`. Twenty-seven files in `localengine` import
`controller`; zero files in `controller` import `localengine`. The
admission and resume logic this plan wants to converge is written
directly against `controller`'s concrete types, not behind an
abstraction `flow` could absorb in isolation.

One seam already exists, and it is not the one this plan needs.
`controller.AgentStepRunner` (`agent_step.go`) is an interface one
step's execution goes through; `controller.CoordinatorRunner` is its
production implementation. That interface abstracts what one step
does, not the run-level admission, lease, and resume machinery
`LinearController` owns. `flow` absorbing that machinery means
absorbing something `controller`-shaped, not admission and resume in
isolation.

Decision: the split line is undetermined. Revision five's working
hypothesis, and revision six's first attempt at naming the seam, both
assumed a separability the code does not have. Before any `flow`
absorption plan is written, a design spike must answer one question:
can `localengine`'s admission, lease, and resume logic be rewritten
against an abstracted step-runner interface, so `flow` absorbs that
logic without absorbing `controller`'s step-execution semantics? Until
that spike answers yes with a named interface, stage 6 keeps `flow`
and `internal/workflows` as one open item with no committed boundary,
not two.

### Proposed standing rules

Three rules, drafted here for the orchestrator to apply to AGENTS.md's
Building blocks section. The planner may not edit AGENTS.md; this text
is a proposal, not yet in force.

- "Test a candidate package against a non-coding agent: a support
  agent, a research agent, a data-pipeline agent. If that agent has no
  plausible use for the package, the package belongs in the consumer,
  not the SDK. This test is symmetric: a package that claims a
  plausible non-coding-agent caller must name one. Name an in-tree
  caller, or a `policy/pending_wiring.json` entry with a real
  `target`, the way `subagent` itself already names `runconfig` as
  its target. A bare claim of plausibility, with no named caller and
  no `pending_wiring.json` entry, does not pass the test either way."
- "The SDK accepts parsed Go structs. An application parses its own
  file formats before it calls into the SDK."
- "Treat a consumer's port or interface layer as the specification of
  what the SDK must provide. A capability no port needs, and no
  plausible second consumer needs, stays in the consumer."

This closes a gap this revision's own diff/ evidence exposed. The
eleven retained `subagent` block-wrapper tools (`RoomTool`,
`SchedulerTool`, `DiscoveryTool`, `TriggerTool`, `HeartbeatTool`,
`ProviderRegistryTool`, and the rest) have zero non-test callers
anywhere in this repo today, the same grep result `diff/` had before
this plan removed it. The difference is `policy/pending_wiring.json`:
`subagent` already carries a named `target` (`runconfig`) and a
`reason`, so its orphan status is declared and tracked, not asserted.
The standing rule above requires that same declaration from every
package it keeps on the "plausible non-coding-agent caller" side, not
grep evidence alone on the "goes" side and assertion alone on the
"stays" side.

## Module wiring

- During a stage, the consumer uses a `replace` directive against a
  local SDK checkout.
- At every stage boundary, the SDK cuts a tag. The consumer pins that
  tag and drops the `replace` directive.
- Rule: no stage closes with a `replace` directive committed. A gate
  on the consumer's `go.mod` checks this.
- The SDK remote is private, per AGENTS.md. Set
  `GOPRIVATE=github.com/MiviaLabs/*` in the consumer only — local dev
  and every Go-running consumer CI job, not just the one most jobs
  share. This repo's own CI needs no `GOPRIVATE`: this repo imports no
  other MiviaLabs module today, so setting it here is dead config with
  no caller. Re-add it if that changes. This is a stage 1 prerequisite
  in the consumer. A tag pin fails without it there.
- Rollback rule: an SDK import and the matching consumer deletion land
  in separate commits. The import lands first. Reverting a regression
  is then one commit, not a merge.

## Every new SDK package

Two stages create SDK packages whose only caller is the other module.
A cross-module caller is invisible to the SDK's own gates. Both
packages fail on the commit that creates them unless declared.

Each new SDK package declares, in the same commit:

- A row in `policy/layers.json`, or `check_deps.py` blocks its
  imports.
- An entry in `policy/pending_wiring.json` with `reason`, `target`,
  and `permanent`. The consumer module is the named caller.
  `permanent` is `true`, because the entry cannot clear while the
  consumer is a separate module.
- A plan at the mirrored path, the 85 percent coverage floor, and the
  `api/` lock.

## Evidence base

Measured, not estimated. Worktree copies excluded. Every number below
was verified twice.

- Consumer: 86 packages, 319 internal import edges, 33 Python gates.
- Consumer: 170,206 source lines, 337,272 test lines, 1,438 test
  files.
- Fuzz functions, unique names, same unit on both sides: consumer 57,
  SDK 21.
- `internal/cli`: 53,103 non-test lines across 266 files. It is one
  flat package. `find internal/cli -type d` returns one directory. All
  266 files share one package scope and one set of unexported
  identifiers.
- `internal/cli` imports 48 of 86 packages. Its in-degree is one;
  `cmd/mivia` is the single importer.
- Terminal interface inside `internal/cli`: 47 files, 13,045 non-test
  lines. This is the code that gets deleted.
- `internal/ui` and `internal/uikit` together: 14,731 non-test lines.
  This is the code that stays.
- `internal/workflows`: 16 subpackages. `localengine` has 19
  out-edges and `controller` has 18.
- `internal/uikit/ports`: 675 non-test lines holding 18 interfaces.
  Two implementations exist: `demoharness` and `replay`. Both are
  mocks. No production implementation exists.
- `internal/workflows/testdata/invalid`: 20 cases.

## The parity oracle

Revision one named `internal/memory/search_parity_test.go` as the
template. That was wrong. It runs two backends against one hard-coded
expectation. It never compares one backend's output to the other's.

Two oracle shapes apply, chosen by package purity.

For a pure package, use a generated golden fixture:

- In the change that introduces the swap, capture the pre-swap
  implementation's output into a fixture.
- Assert the new implementation is byte-equal to that fixture.
- The fixture is generated, never hand-written.

For an impure package, byte equality is unreachable. Use invariant,
metamorphic, and fault-ordinal assertions. Assert the same properties
against both implementations.

Every stage 4 item is tagged pure or impure. Only a pure item may
claim the byte-equal criterion.

## Stage 0 — structural preparation

No SDK import lands in this stage.

- Add an import-policy gate to the consumer. Mirror the SDK's
  `policy/layers.json` and `scripts/check_deps.py`. The consumer has
  33 Python gates and no import gate. That absence is why
  `internal/cli` reached 48 out-edges.
- Give the policy a committed integer edge cap that continuous
  integration enforces. Today's count is 319. That is the pre-stage
  baseline, not the cap. Stage 0 both adds edges by splitting
  `internal/cli` and removes them by collapsing `internal/workflows`.
  Measure after the restructure lands, then seed the cap.
- Give the policy an explicit deny-list of inverted edges. An inverted
  edge runs from a persistence package into a domain package. Two
  exist: `storage` into `contextmgr`, and `storage` into
  `contextstate`.
- Cut those two edges. The `storage` to `contextmgr` edge exists
  because `storage.NewUsageWriter` returns `contextmgr.UsageWriter`,
  and `RecordUsageEvent` takes `contextmgr.UsageRecord`. Cutting it
  relocates the usage-accounting types. That is the same concern the
  usage swap touches in stage 4. Resolve both as one decision.
- Inventory the unexported identifiers the 47 terminal files share
  with the other 219 files, before splitting. One flat package of 266
  files has no compiler-enforced seam. This inventory is the largest
  single task in the stage.
- Split `internal/cli`. `cmd/mivia` already exists and stays thin. Add
  a composition root holding construction only, and a command-handler
  package. The composition root becomes the single site that knows SDK
  from internal. That single site is what makes each later swap small.
- Collapse the `internal/workflows` subpackage count. Sixteen
  subpackages hold one logical engine.

Exit criteria, all mechanically checkable:

- The import gate is green and a cap integer is committed.
- The inverted-edge deny-list reports zero violations.
- The command-handler package's out-degree is below 10. The
  composition root is exempt and is the only exempt package.
- `go list ./internal/workflows/...` returns eight packages or fewer.
- `make verify` is green in the consumer.

## Stage 1 — parity infrastructure

Build the oracle before moving any code.

- Set `GOPRIVATE` in the consumer, local dev and every Go-running CI
  job. Not in this repo; see the "Module wiring" section's corrected
  bullet above.
- Define a recorded provider fixture format. Neither repository
  records provider traffic. Write one scripted-response format. Load
  it from a consumer fake and from an SDK fake. Record real traffic
  through the consumer's existing `Completer` seam.
- Redact the corpus before any fixture lands. Recorded traffic carries
  keys, model identifiers, and user content. The consumer owns
  `internal/redact`. Run every fixture through it, and through a
  committed secret-scan gate.
- Donate the fault-ordinal primitive upward. Revision one proposed
  importing the SDK's `e2e.FaultStore`. That was wrong twice.
  `e2e.FaultStore` wraps a concrete `ledger.Store` and cannot serve as
  a generic ordinal counter. Separately, `policy/pending_wiring.json`
  declares `e2e` caller-scoped and permanent. Instead, generalize the
  consumer's `faultinject.Gate` into a new leaf SDK package. It has
  zero third-party imports, so nothing blocks it. Follow the "every
  new SDK package" rule above.
- Add clock and identifier seams, scoped to packages that read the
  clock. Those are consumer `storage`, `workflows`, `agent`, and
  `coordinator`, plus SDK `subagent`. Copy the pattern from SDK
  `ledger/store.go`. Do not add a clock seam to a pure package.
  `contextmgr` and `contextplan` read no clock and need none.
- Create a shared corpus both modules read. Adopt the SDK's `valid_`
  and `invalid_` vector prefix convention. Seed it from the consumer's
  `internal/workflows/testdata`, which holds one valid and 20 invalid
  cases. Convert those cases to JSON at seeding time. That keeps the
  corpus readable without a TOML parser, so stage 1 needs no
  dependency exception of its own.

Exit criteria:

- The fault-ordinal package passes every SDK gate, including the
  orphan gate through its declared wiring entry.
- The consumer imports it, and continuous integration is green in both
  repositories with no `replace` directive.
- A recorded provider corpus exists, both sides load it, and every
  fixture passes redaction and secret scanning.
- Every clock-reading package named above has an injectable clock.

## Stage 2 — additive adoptions

Revision one listed six packages here, revision two listed three, and
revision three listed two. All were wrong. The consumer already owns
`internal/providerregistry`, `internal/remainder`, `internal/jschema`,
and a full token-accounting path spanning `provider/usage.go`,
`agent/emit.go`, `contextmgr/usage.go`, and `storage/usage_events.go`.

`contextbudget` also fails the additive test. `internal/contextmgr`
already owns budget accounting through `Plan`, `PercentFloor`,
`planner_retention.go`, and `planner_elision.go`. In the SDK,
`contextbudget` is an input to the planner, reachable only through
`agent`, `agentloop`, `agentrun`, and `runconfig`. Adopting a second
budget representation two stages before the first one leaves is the
duplication this program removes. It moves to stage 4.

Stage 2 adopts one package: `trace`. The consumer has no tracer, no
span type, and no exporter.

Exit criteria: `trace` is wired through the composition root, and the
consumer builds against a tagged SDK version with no `replace`
directive.

## Stage 3 — absorptions that unblock the leaf swaps

Every stage 4 swap deletes a consumer package. The decision record
forbids a guarantee regression. So the SDK counterpart must be a
superset before the deletion lands.

Revisions two and three both hand-listed the exports to absorb, and
both lists were incomplete. Revision three's `schema` list named three
exports; the consumer exports nine, and six have production callers.
Its `spool` list omitted `CapWithSpoolRef`, which
`internal/agent/loop_scheduler.go` calls. Hand-listing does not work.

So the export set is derived mechanically, never by hand:

- Write a script that diffs the consumer package's exported symbols
  against the SDK counterpart's `api/` lock.
- Cross-reference each missing symbol against a non-test caller
  search.
- Every symbol with a non-test caller must exist in the SDK before the
  stage 4 deletion lands.
- The script's output is committed with the absorption.

Six absorptions are needed. Each is its own SDK plan with its own plan
review.

- `skills`. Absorb version-aware selection. No new dependency: the
  SDK registry already takes parsed `Skill` structs through `Add`, so
  the TOML loader stays a consumer-owned file. It decodes TOML,
  assembles a `Definition`, and calls the consumer's own
  `internal/skills.Registry.Register` today; an absorption maps that
  call onto SDK `skills.Registry.Add` without moving any TOML parsing
  into the SDK. Fail-closed resource rebinding
  (`resource_file_linux.go`) does not absorb here; it moves with the
  `workspace` confinement item in stage 4, since it is filesystem
  hardening, not skill matching.
- `contextplan`. Absorb the pure-function contract and the
  output-reserve rule. Callers pre-subtract the reserve. A naive port
  violates that rule. No new dependency.
- `mcp`. Absorb server-text sanitization and fleet-level error
  containment. No decision needed.
- `schema`. Absorb envelope extraction and the corrective-message
  surface. The consumer's `internal/jschema` exports `ExtractEnvelope`,
  `ExtractOutputCandidate`, `EnvelopeAppendixBody`, `PromptAppendix`,
  `ModelSchemaContract`, `FormatCorrective`,
  `FormatCorrectiveWithSchema`, `StripOneCodeFence`, and
  `MaxCorrectiveBytes`. The SDK's `api/schema.txt` has `Compile`,
  `Validate`, and `Corrective`. One item needs a design decision, not
  a bullet: `FormatCorrectiveWithSchema` takes a redactor argument,
  and the SDK's `Corrective(err error) string` takes none. Adding a
  redactor seam changes a locked surface. Note the blast radius:
  `internal/cli`, `internal/skills`, `internal/subagents`, and three
  `internal/workflows` subpackages all call `jschema`.
- `spool`. Absorb durable grant persistence and the truncation notice.
  The consumer exports `SpoolGrantStore`, `NotFoundReporter`,
  `TruncationNotice`, `Fit`, `CapWithSpool`, and `CapWithSpoolRef`.
  The SDK's `ContentStore` is a two-method put-and-get with no
  context, no durable grant seam, and no not-found reporter.
- Provider descriptor catalog. The consumer's
  `internal/providerregistry` is a static catalog of `Descriptor`,
  `Lookup`, and `Names`, with name canonicalization. The SDK's
  `providerregistry` is a runtime failover router. The two share a
  name and nothing else. Folding a catalog into a router merges two
  concerns in one package. Give the catalog its own SDK package name,
  and follow the "every new SDK package" rule.

Exit criteria, per absorption:

- The mechanical export diff is committed and reports zero missing
  symbols that have non-test callers.
- A merged plan, the 85 percent coverage floor, the `api/` lock, and
  the structure gate, all in the same commit.
- A conformance vector generated from the consumer implementation,
  committed to the SDK in the same change. Shape gates prove nothing
  about behavior. Without this vector a mismatch surfaces one stage
  too late, and the rollback is an SDK release rather than a consumer
  commit.

## Stage 4 — leaf swaps

Order by coupling, lowest first. Each swap deletes the internal
package in the same change. Each is tagged by oracle type.

- `contextmgr` onto `contextplan`, plus `contextbudget`. Pure. Neither
  side uses wall clock, randomness, or goroutines. Best candidate.
- `skills`. Pure. The consumer's registry is unsynchronized where the
  SDK's is locked. The swap fixes that consumer defect.
- `internal/jschema` onto SDK `schema`. Pure.
- The `hooks` protocol codec. Pure. Subprocess execution does not
  converge.
- `internal/remainder` onto SDK `spool`. Impure; durable grants.
- The `memory` search layer. Impure; sqlite ordering ties. Its sqlite
  backend sits behind the build tag, per decision two.
- `internal/providerregistry` onto the new SDK catalog package.
  Impure.
- `internal/workspace`'s `Root` onto SDK `workspace`. Impure; real
  filesystem calls. Direction runs consumer-to-SDK, the reverse of
  every other item on this list, because SDK `workspace/`'s
  `os.Root`-based confinement is already the harder guarantee (see
  "Boundary correction" above). Wire `internal/tools`' twenty-plus
  callers onto `workspace.Workspace`, then delete `root.go` and the
  `longpath*.go` files. `namespace.go` stays in `internal/workspace`,
  unchanged; it never converges. `resource_file_linux.go`'s
  fail-closed reopen absorbs into SDK `workspace/` in the same change,
  carrying the rescoped `x/sys` exception (see decision one, above).
- The usage path onto SDK `usage`. Impure; durable event store. Its
  sqlite backend sits behind the build tag, per decision two. Resolve
  it together with the stage 0 inverted-edge cut.
- `mcp`. Impure; network.

Exit criteria: every listed internal package is deleted. Each pure
item has a committed golden fixture. Each impure item has committed
invariant and fault-ordinal tests.

## Stage 5a — build the ports implementations that the SDK backs today

Revision two treated the missing production `ports` implementation as
a defect. It is not. `internal/ui` runs on mocks with knobs prepared
for integration. No production implementation was ever built, because
the intended production implementation is the one built on the SDK.
`ports.go` states it directly: the SDK owns the model-completion loop.

So this is greenfield construction, not a swap.

First task, before any construction: produce a table mapping each of
the 18 `ports` interfaces to the SDK package that backs it. An
interface with no backing package is a stage 6 dependency, not a stage
5a task. That table is committed.

Build the implementations with SDK backing today: `Conversation`,
`TurnHandle`, `SubagentThreads`, `Approver`, `CommandRunner`,
`SessionStore`, and the read-only settings views.

### The control

Revision three proposed driving `demoharness` and `replay` through
golden fixtures and requiring the SDK-backed implementation to match.
That control cannot reach the path, and the item is corrected here.

`demoharness` is a fake whose observable values are hard-coded.
`conversation.go` fixes input tokens at 40, output tokens at 90, cost
at four thousandths of a dollar, and turn identifiers at `demo-N`. A
real backend draws those from a provider and an agent loop. It can
never produce `demo-1` with exactly 40 input tokens. `demoharness` is
not a specification and must not be used as one.

Two controls that do reach:

- A conformance suite written against the `ports` contract, not
  against any implementation's output. For each of the 18 interfaces,
  assert the invariants the interface promises. Examples: a channel
  closes exactly once, `Cancel` is idempotent, `Pending` delivers
  before `Resolve` unblocks, and handle identifiers are unique. Run
  the identical suite against `demoharness`, `replay`, and the
  SDK-backed implementation. Three implementations passing one suite
  is a real oracle.
- A recorded-corpus regression lock. Drive the SDK-backed
  implementation from a stage 1 provider fixture. Assert the resulting
  event stream is byte-equal to a golden generated from that same
  recording. That golden derives from the real backend, so it locks
  regressions rather than specifying behavior.

`internal/cli/settings_ports_drift_test.go` is not a third control. It
is a reflection-based field-coverage guard that never constructs an
implementation. It detects a new `internal/config` field, not a wrong
implementation. Keep it green through the stage 0 split and the stage
7 deletion, and do not count it as evidence of correctness.

### Scope statement

Four packages import `ports`: `ui/component/approval`,
`ui/component/topbar`, `ui/screen/conversation`, and
`ui/screen/settings`. Saying `internal/ui` is untouched contradicts
that. The accurate statement is narrower.

- The 18 exported `ports` signatures do not change. Only the set of
  implementations grows by one.

### Exit criteria

- The interface-to-package backing table is committed.
- The conformance suite passes against all three implementations.
- The recorded-corpus regression lock passes.
- The diff of the exported `ports` signatures is empty.
- The feature inventory described under stage 7 is committed.

## Stage 5b — automation settings

`ports.AutomationSettings` needs scheduler enumeration, scheduler
cancellation, and a cancelled ledger state. None exists in the SDK
today.

Decision three schedules that work as a named stage 6 item. Stage 5b
runs after it and builds `AutomationSettings` on SDK types. The ports
layer then converges fully, with no consumer-backed interface left.

Exit criteria: `AutomationSettings` passes the same stage 5a
conformance suite, and no `ports` interface remains backed by a
consumer-owned store.

## Stage 6 — donate upward

Each item is its own project with its own SDK plan and plan review.
The consumer's implementation is generalized and lands in the SDK
first. Only then does the consumer delete its copy.

Every item carries the same four gates.

- The 85 percent coverage floor met, with the consumer's tests
  migrated in the same commit.
- The `api/` lock updated in the same commit.
- The structure gate met. Files stay at or below 500 lines and
  functions at or below 80. The absorbed bodies are the largest in the
  program. Split during absorption, not after.
- Import and deletion in separate commits, import first.

Order by independence:

- `ledger`. The SDK absorbs the run, task, and attempt hierarchy. It
  absorbs per-run fencing combined with per-task version compare-and-
  set. It absorbs event sourcing with projection rebuild, and the run
  retry policy. Its sqlite backend keeps the `ledger_sqlite` tag, per
  decision two. The tag is not dropped.
- `subagent`. The SDK absorbs the bounded worker pool, dependency
  readiness, blocked-status propagation, and panic recovery. This
  absorption does not touch `subagent`'s file-editing toolbox; that
  toolbox and `diff/` already left the SDK in the boundary correction
  above, ahead of this stage and with no consumer dependency.
- `provider`. The SDK absorbs the retry transport. That transport
  honors `Retry-After` and classifies non-retryable responses by body,
  not by status alone. Decide per adapter whether the seven concrete
  adapters move or stay.
- Scheduler capability. Add scheduler enumeration and cancellation,
  and a cancelled ledger state. Today `api/scheduler.txt` exports
  `Add`, `Remove`, and `Run` only, and `Job` is a bare closure with a
  string identifier. Stage 5b follows this item.
- `flow` and `workflows`. The largest item, and the one with no
  committed boundary yet. `internal/workflows/controller`'s prompt
  construction, approval and diff gates, human escalation, panel-
  output synthesis, and git worktree stacking are permanently
  consumer-only; that part is settled. Whether `flow` can absorb
  `internal/workflows/localengine`'s admission compiler, leases, and
  resume-from-log is not settled: `localengine.Engine` holds a
  concrete `*controller.LinearController` and calls
  `controller.RunWithCancelReconciliationRetry` directly, so the
  admission and resume logic is not separable from `controller` today
  (see "Boundary correction" above). This item's own plan must open
  with a design spike answering whether an abstracted step-runner
  interface can free `localengine`'s admission and resume logic from
  `controller`'s concrete types. No absorption plan may assume the
  answer is yes. No new dependency either way: `x/mod` and `go-toml`
  are not granted, per the narrowed decision one.

Verification here is invariant-based. Use the shared fault-ordinal
package and the race tests.

## Stage 7 — delete the terminal interface

The terminal interface inside `internal/cli` is deleted, not ported.
That is 47 files and 13,045 non-test lines. `internal/ui` and
`internal/uikit` stay and become the only user interface.

Revision three asserted that `internal/ui` must reach feature parity
first, and put that assertion in the risk register with no mechanism,
no artifact, and no owner. This is the program's largest unguarded
regression: 13,045 lines deleted against an unmeasured replacement.
The gate is defined here.

The artifact is a committed feature inventory, produced in stage 5a:

- Enumerate every user-reachable command, screen, and keybinding in
  the 47 terminal files.
- Map each one to its `internal/ui` counterpart, or mark it `dropped`
  with a one-line reason.

Exit criteria:

- The feature inventory has zero unmapped and unmarked entries.
- No file under `internal/cli` imports a terminal rendering library.
- The command-handler package's out-degree is still below 10.
- `settings_ports_drift_test.go` is still green.
- The edge cap was lowered at every stage boundary, and no stage
  closed without lowering it.

## Concerns that never converge

- Hook execution. The SDK runs in-process closures. The consumer runs
  declared subprocesses with argument vectors and timeouts. Subprocess
  execution is a harness concern. Only the protocol codec converges.
- Message routing. The SDK's `envelope` signs and chains messages. The
  consumer's `agentmsg` routes ask and steer traffic inside one run.
  The router is coupled to the coordinator run model.
- File-editing tools. `diff/` and `subagent`'s file-editing toolbox
  (`FileTools`, `WorkspaceReadTool`, `WorkspaceWriteTool`,
  `WorkspaceListTool`, `WorkspaceStatTool`, `DiffTool`) delete from
  the SDK; they were coding-agent product surface, not a duplicated
  concern. The consumer's `internal/diff` is a full diff engine
  backing `internal/tools/write.go` and `edit.go`; it stays
  consumer-only and gains no SDK counterpart.
- Typed UI events. SDK `events/` is a generic string-payload bus. The
  consumer's `internal/events` is a typed union (`Kind`, `Delivery`,
  `MetricsAdapter`, `CacheUsageEvent`, `TokenUsageEvent`,
  `CompactionEvent`) that projects agent, workflow, and provider
  activity for a terminal UI. Build the projection from SDK
  `events.Name` plus `agentloop.Options.Audit`'s `AuditRecord`; it
  needs no new SDK surface.
- The workspace namespace registry. `internal/workspace/namespace.go`
  (`Namespace`, `SkillsDir`, `SessionsDir`, `WorktreesDir`,
  `ContextStorePath`, `MemoryDBPath`) encodes mivia's own on-disk
  layout. No second SDK consumer needs it; only `internal/workspace`'s
  confinement type (`Root`) converges, onto SDK `workspace/`.
- `internal/workflows/controller`'s prompt construction, approval and
  diff gates, human escalation, panel-output synthesis, and git
  worktree stacking (`linear_prompt.go`, `linear_gates_approval.go`,
  `linear_diff_gate.go`, `panel_synthesis.go`, `stacking.go`). This is
  the coding agent's step-execution semantics, not a generic
  step-graph concern. This never converges. Whether `localengine`'s
  admission, lease, and resume logic can converge separately from it
  is undetermined; see the `flow`/`workflows` seam note in stage 6.
- `internal/workflows/verifier`'s Go-module sandbox
  (`sandbox_modules.go`), and its `golang.org/x/mod` dependency. It
  verifies Go module changes for a coding agent; no other agent shape
  plausibly needs it.

## Verification limits

Four concerns can never be differentially tested. Verify them by
invariant, by metamorphic test, and by fault ordinal.

- Provider retry and streaming.
- Ledger leases and fenced takeover.
- Workflow engine scheduling.
- Subagent concurrency.

## Documented limits to revisit

Revision one presented four items as newly found SDK defects. Review
showed all four are documented. They are limits worth revisiting, each
needing its own plan.

- `flow/checkpoint.go` documents that `FailureFrom` returns false
  after resume, and that a route-exclusion abort degrades into a plain
  skip.
- `flow/resume.go` documents the absent topology check.
- `flow/wave.go` documents the shared transition row as a deliberate
  pre-spawn resolution. It starts one goroutine per declared panel
  member, bounded by the static definition. It is not unbounded.
- `subagent/runall.go` documents flat fan-out and no sibling
  cancellation. It offers no dependency-ordering option, unlike the
  consumer's `subagents`. Revision three called it undocumented. That
  was wrong.

## Risk register

- The consumer is a working system with 337,272 test lines. Every
  stage must keep it shippable. The separate-commit rollback rule is
  the mechanism.
- A concurrent development session commits to the SDK. Coordinate
  before any large absorption lands.
- Stage 6 is the bulk of the effort. Do not start it before stage 1
  proves the oracle works.
- The edge cap can stall. Lower it at every stage boundary, or the
  baseline becomes permanent.
- Splitting one flat package of 266 files has no compiler-enforced
  seam. Budget stage 0 accordingly.
