# Convergence: mivia-agent-side task list

Companion to `convergence.md` (revision six). This file holds every
item the convergence program requires in
`github.com/MiviaLabs/mivia-agent`, so `convergence.md` stays a plan
this repo's gates can check, and so SDK-side work is not blocked
waiting on consumer-side work that has not started.

Nothing here has landed. Each item cites its source section in
`convergence.md`.

## Ready to start (no design spike blocking it)

### Adopt SDK `workspace.Workspace` in place of `internal/workspace.Root`

Source: `convergence.md`, "`workspace/`: confinement converges, the
namespace registry never does."

`internal/workspace.Root` resolves a path, evaluates symlinks, and
string-prefix-checks it against the root, then returns a plain path
string; the caller opens that path itself, later. This is a
resolve-then-open pattern with a TOCTOU window between `Resolve` and
the caller's own open. SDK `workspace.Workspace` opens one `os.Root`
descriptor once and confines every access through it for the life of
the value. The SDK side already carries the stronger guarantee; this
is the one item in the program where the consumer adopts the SDK
package rather than the SDK absorbing the consumer's code.

- `internal/workspace` has 60 non-test importers in the consumer;
  measured and plan-reviewed. Of those, 21 touch confinement symbols
  (`Root`, `Open`, `OpenFullDisk`, `LongPath`, `SameExistingPath`); the
  remaining 39 use only `namespace.go`'s path-convention helpers and
  are out of scope for this item. The 21 split into four groups:
  Group A (20 files under `internal/tools/`: `write.go`, `edit.go`,
  `delete.go`, `read.go`, `list_dir.go`, and 15 more — the real
  file-edit/read/delete/list/search/run tool surface, the highest-risk
  part of the swap), Group B (11 files of CLI wiring that construct a
  `*Root` and thread it into Group A, led by `chat_workspace.go`),
  Group C (`worktree_marker.go`, `internal/storage/worktree_instances.go`,
  `internal/vcs/worktree.go` — `namespace.go`/`LongPath` convention
  only, unrelated to confinement, permanently out of scope), Group D
  (`internal/workflows/workspace/workspace.go` — `Open` plus
  `SameExistingPath` for git-worktree identity comparison, no raw
  filesystem mutation).
- `namespace.go` (`Namespace = ".mivia"`, `SkillsDir`, `SessionsDir`,
  `WorktreesDir`, `ContextStorePath`, `MemoryDBPath`) never converges.
  It stays in `internal/workspace` as mivia's own on-disk layout
  convention, composed with the SDK's `Workspace` rather than folded
  into it. `SameExistingPath` (a free function, `os.Stat` plus
  `os.SameFile`, zero dependency on `*Root`/`Workspace`) needs no SDK
  counterpart either — the consumer keeps it unchanged regardless of
  what else converges.
- **Group D is confirmed landable now**, verified by a full read (252
  lines): `Open` plus `SameExistingPath` only, no raw filesystem
  mutation. This is the consumer's first SDK import, via a `go.mod`
  `replace` directive pointing at the local SDK checkout.
- **Groups A and B are blocked** on SDK-side gaps, all confirmed by
  reading the SDK's `workspace/` package against the actual consumer
  call sites (not assumed from names). Adopting SDK `Workspace` for
  these groups is impossible today without a guarantee regression,
  which the decision record forbids:
  - **No confined delete.** The consumer's `delete.go` resolves a path
    through `*Root` once, then calls plain `os.Lstat`/`os.Remove`
    directly on the returned string — the SDK's `Workspace` has no
    delete primitive at all, fd-mediated or otherwise. This is one
    gap (a confined delete/unlink method closing the check-then-use
    window), not two — an earlier pass over-split it into a missing
    `Lstat` and a missing `Remove`; the consumer never calls Root's
    `Lstat` because Root has none.
  - **No streaming/partial file handle.** `read.go`'s windowed read
    scans a file with `bufio.Scanner`; `write.go`/`edit.go`
    stream-write. SDK `ReadFile`/`ReadFileLimit`/`WriteFile` are
    whole-buffer only.
  - **No mode-preserving/custom-mode write.** SDK `WriteFile`
    hardcodes `0o600`/`0o700`. The consumer's `write.go`/`edit.go`
    create with `0o644`/`0o755` and explicitly preserve an existing
    file's mode on rewrite — pinned by `TestSearchReplacePreservesFileMode`
    and `TestSearchReplacePreservesRestrictiveFileMode`, both asserting
    genuinely user-visible behavior (an executable hook script keeps
    its bit; a `0600` file doesn't get silently widened). A naive swap
    fails both.
  - **No FIFO-safe non-blocking open.** The consumer's
    `open_regular_unix.go` opens non-blocking then `fstat`s, closing a
    window where a path becomes a FIFO between `Stat` and `Open` and
    blocks the tool worker. SDK's `Open`/`WriteFile`/`Stat` call paths
    have no equivalent guard. Frame this gap honestly when it's
    absorbed: the consumer's `Root` is pure lexical confinement
    (`filepath.EvalSymlinks` plus a string-prefix check, no file
    descriptor); SDK `Workspace` is backed by a real `os.OpenRoot`
    (Go 1.24+, confirmed in use), which structurally closes the
    symlink-swap TOCTOU class the consumer's code cannot close today.
    Adopting SDK `Workspace` is a net security improvement on the
    confinement axis regardless of this FIFO gap remaining open — the
    FIFO gap is a separate, still-open axis, not evidence the SDK
    package is less safe than what it replaces.
  - **No confined-path-resolve-without-open, for subprocess handoff.**
    `run.go` and `search.go` resolve a confined subdirectory as a
    subprocess's working directory or argument; `Workspace.Root()`
    only covers the no-subpath case. There is no exported method to
    get a confinement-checked absolute path for an arbitrary
    caller-named subpath, for handoff to something outside
    `Workspace`'s own I/O.
  Each of these five gaps needs its own SDK plan and plan review
  before Groups A/B can proceed. Do not build a workaround in the
  consumer that routes around SDK `Workspace`'s API to fake coverage.
- Parity oracle: impure (filesystem I/O). Use invariant and
  fault-ordinal tests per `convergence.md`'s "The parity oracle"
  section, not byte-equal fixtures. Before writing them, read the
  consumer's existing `internal/workspace` test files and enumerate
  every invariant they already assert (path-outside-root rejection,
  symlink-escape rejection, `Rel`/`LexicalRel` round-trip, `Unrestricted`
  mode behavior) — every one needs a matching assertion against the
  new code path, or the swap silently loses proven coverage.

### Set `GOPRIVATE`

Source: `convergence.md`, Stage 1, first bullet. **Correction, plan
reviewed:** that bullet's "both repositories and both
continuous-integration pipelines" is overbroad on the SDK-CI half.

`GOPRIVATE=github.com/MiviaLabs/*` (the glob — the org has several
other MiviaLabs repos beyond these two, and `go env` only consults the
pattern for modules actually imported, so the glob costs nothing
extra) needs to land in the consumer only, not in this repo's CI:

- **Consumer local dev**: `go env -w GOPRIVATE=github.com/MiviaLabs/*`,
  documented as a one-time setup step.
- **Consumer CI**: every Go-running job needs it, not just the one
  composite action most jobs share — the consumer's CI has a handful
  of jobs (macOS, Windows, a cross-compile job) that call `setup-go`
  directly and skip the shared composite action. Each needs its own
  copy of the step, or the CI job list needs restructuring so all
  Go-running jobs route through one shared setup step.
- **This repo (`mivia-ai-sdk`)**: do not add it. This repo's `go.mod`
  imports no other MiviaLabs module today, and the convergence
  direction is one-way (consumer imports SDK, never the reverse), so
  setting it here would be dead config with no caller — the same
  "no speculative generality" rule this repo applies to code. Re-add
  it the moment this repo's own `go.mod` takes a dependency on another
  private MiviaLabs module; that is a conditional trigger, not a
  permanent omission.

**`GOPRIVATE` alone does not authenticate fetch** — it only tells `go`
to skip the module proxy and checksum database for matching paths, it
supplies no credential. A git URL rewrite (SSH `insteadOf`, or an
HTTPS rewrite backed by a scoped PAT in CI) is a separate, deferred
follow-up — land it together with dropping the `replace` directive and
pinning a real tag, not before. Until that follow-up lands, document
`GOPRIVATE` with an explicit note that it alone is insufficient, so a
developer adding a genuine private import before the credential piece
exists doesn't hit an unexplained fetch failure with no pointer back
to why.

A stage 1 prerequisite for the provider-fixture and fault-ordinal work
below, and for any future move off a local `go.mod` `replace`
directive. Cheap, do it early, do it before it blocks something else.

### Add an import-policy gate to the consumer

Source: `convergence.md`, Stage 0.

Mirror the SDK's `policy/layers.json` and `scripts/check_deps.py`. The
consumer has 33 Python gates and no import gate today — the absence is
why `internal/cli` reached 48 out-edges. Steps:

- Commit an integer edge cap. Today's count is 319; that is the
  pre-restructure baseline, not the cap. Measure again after the
  `internal/cli` split and the `internal/workflows` collapse below,
  then seed the real cap.
- Add an explicit deny-list of inverted edges (a persistence package
  importing a domain package). Two exist today: `storage` into
  `contextmgr`, and `storage` into `contextstate`. Cut both. The
  `storage`-to-`contextmgr` edge exists because
  `storage.NewUsageWriter` returns `contextmgr.UsageWriter` and
  `RecordUsageEvent` takes `contextmgr.UsageRecord`; cutting it
  relocates the usage-accounting types, the same concern the stage 4
  usage swap touches. Resolve both as one decision, not two.

### Split `internal/cli`

Source: `convergence.md`, Stage 0.

One flat package, 266 files, 53,103 non-test lines, in-degree one
(`cmd/mivia`), out-degree 48. No compiler-enforced seam exists between
its 47 terminal-interface files (13,045 non-test lines, the code
Stage 7 deletes) and the other 219.

- Inventory the unexported identifiers the 47 terminal files share
  with the other 219, before splitting. This inventory is the largest
  single task in Stage 0 — budget for it accordingly.
- `cmd/mivia` already exists and stays thin. Add a composition root
  holding construction only, and a command-handler package. The
  composition root becomes the single site that knows SDK types from
  internal types — that is what keeps each later swap small.
- Exit check: the command-handler package's out-degree stays below 10.
  The composition root is exempt, and is the only exempt package.

### Collapse `internal/workflows` from sixteen subpackages to eight or fewer

Source: `convergence.md`, Stage 0.

Sixteen subpackages hold one logical engine. `go list
./internal/workflows/...` must return eight packages or fewer to pass
Stage 0's exit criteria. Do this restructure before, not during, the
`flow`/`workflows` design spike below — the spike needs a stable
package layout to reason about.

## Blocked on an SDK-side or joint decision first

### `flow` versus `internal/workflows`: design spike required

Source: `convergence.md`, "`flow` versus `internal/workflows`: the
seam is not clean."

Not ready to start. `internal/workflows/localengine.Engine` holds
`ctrl *controller.LinearController` as a core field and calls
`controller.RunWithCancelReconciliationRetry` directly;
`engine_resume.go` builds concrete `LinearController`s through
`controller.NewLinearController`, `Admission`, and `StepRuntime`.
Twenty-seven `localengine` files import `controller`; zero
`controller` files import `localengine`. The admission, lease, and
resume logic this program wants to converge onto SDK `flow` is written
directly against `controller`'s concrete types, not behind an
abstraction `flow` could absorb in isolation.

One seam already exists and is not the right one:
`controller.AgentStepRunner` (`agent_step.go`) abstracts what one step
does, not the run-level admission/lease/resume machinery
`LinearController` owns.

Before this item can become a real absorption plan, answer one design
question: can `localengine`'s admission, lease, and resume logic be
rewritten against an abstracted step-runner interface, so `flow`
absorbs that logic and `controller`'s product-specific gate, prompt,
evidence, and stacking logic stays behind the interface in the
consumer? This spike happens in the consumer's code, reasoning about
consumer types, and its answer determines whether `flow` gains new
surface or the split is abandoned in favor of a different seam. It is
not SDK-side prep work; it does not start until the `internal/cli` and
`internal/workflows` restructures above give it a stable base.

### `internal/hooks`, `internal/jschema`, `internal/remainder`,
`internal/providerregistry`, `internal/memory`, `internal/ledger`,
`internal/subagents`, `internal/skills`, `internal/provider`, the
usage path, and `internal/mcp` absorptions (Stage 3, Stage 4, Stage 6)

Source: `convergence.md`, Stages 3, 4, and 6.

Each of these absorbs the consumer's implementation into a
corresponding SDK package that is not yet a superset. None can start
until:

- The mechanical export-diff script (Stage 3) exists and is run for
  that pair, so the SDK gains every symbol with a non-test consumer
  caller before the consumer's copy deletes. Hand-listing has already
  produced two wrong lists in this program's revision history —
  automate it once, do not repeat that mistake per absorption.
- That absorption's own SDK-side plan is written and reviewed. Each is
  its own `docs/plans/<pkg>.md` change in this repo, its own plan
  review, and its own conformance-vector commit, per
  `convergence.md`'s "Stage 3" and "Stage 6" exit criteria.

This file does not restate each absorption's detail — `convergence.md`
already has it. It exists to say: none of these are "SDK cleanup"
work. Each is a paired change touching both repos, sequenced import
first, deletion second, in separate commits, per `convergence.md`'s
"Module wiring" rollback rule.

### Recorded provider fixture corpus, fault-ordinal package consumer wiring, clock seams

Source: `convergence.md`, Stage 1.

- Record real provider traffic through the consumer's existing
  `Completer` seam, run every fixture through `internal/redact` and a
  committed secret-scan gate before it lands anywhere.
- Once the SDK's generalized fault-ordinal package exists (SDK-side
  work, tracked in `convergence.md` Stage 1, not in this file), the
  consumer imports it and deletes `internal/faultinject.Gate`.
- Add an injectable clock to consumer `storage`, `workflows`, `agent`,
  and `coordinator` — copy the pattern from SDK `ledger/store.go`.

### `trace` adoption

Source: `convergence.md`, Stage 2.

Wire the SDK's `trace` package through the composition root the
`internal/cli` split above creates. The consumer has no tracer, no
span type, and no exporter today. Depends on the composition root
existing first.

### Stage 5a/5b: `ports` implementations, `AutomationSettings`

Source: `convergence.md`, Stages 5a and 5b.

Build the SDK-backed implementations of the 18 `internal/uikit/ports`
interfaces once their backing SDK packages exist. Commit the
interface-to-package backing table first — an interface with no
backing SDK package is a Stage 6 dependency, not a Stage 5a task.
`AutomationSettings` additionally waits on the SDK scheduler gaining
enumeration and cancellation (Stage 6, SDK-side).

### Stage 7: delete the terminal interface

Source: `convergence.md`, Stage 7.

Delete the 47 terminal-interface files inside `internal/cli` (13,045
non-test lines) only after a mechanically-derived feature inventory —
every user-reachable command, screen, and keybinding, each mapped to
its `internal/ui` counterpart or marked `dropped` with a reason — has
zero unmapped, unmarked entries. Derive that inventory from the
command and keybinding registration sites, not by hand; hand-listing
has already been wrong twice elsewhere in this program.

## Explicitly out of scope for the SDK, permanently

Source: `convergence.md`, "Concerns that never converge," and the
boundary correction section.

- `internal/workspace/namespace.go` — mivia's own `.mivia` path
  convention. No second SDK consumer needs it.
- `internal/events`'s typed UI/workflow event union (`Kind`,
  `Delivery`, `MetricsAdapter`, `CacheUsageEvent`, `TokenUsageEvent`,
  `CompactionEvent`) — build the SDK-bus-to-typed-event projection in
  the consumer using `agentloop.Options.Audit` and its `Bus`/
  `HeartbeatInterval` events; do not ask the SDK for a typed surface.
- Hook subprocess execution (argument vectors, timeouts) — only the
  hook protocol codec converges; the SDK runs in-process closures.
- `internal/agentmsg`'s ask/steer routing — coupled to the consumer's
  coordinator run model, not a generic message-routing concern.
- The Go-module verification sandbox
  (`internal/workflows/verifier/sandbox_modules.go`, the sole
  `golang.org/x/mod` caller) — stays with `controller`'s
  product-specific logic regardless of how the `flow`/`workflows` seam
  question above resolves.
