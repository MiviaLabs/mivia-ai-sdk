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

- `internal/tools/write.go`, `edit.go`, `delete.go`, `read.go`,
  `list_dir.go`, and over twenty more callers use
  `internal/workspace.Root` today. Each needs to move to
  `workspace.Workspace`.
- `namespace.go` (`Namespace = ".mivia"`, `SkillsDir`, `SessionsDir`,
  `WorktreesDir`, `ContextStorePath`, `MemoryDBPath`) never converges.
  It stays in `internal/workspace` as mivia's own on-disk layout
  convention, composed with the SDK's `Workspace` rather than folded
  into it.
- Open question, not yet answered by either revision: does SDK
  `workspace.Workspace`'s API (`Open`, `OpenWith`, `ReadFile`,
  `ReadFileLimit`, `WriteFile`, `List`, `Stat`, `Root()`) cover every
  operation the twenty-plus callers need, or does the SDK package need
  new methods first? Audit the callers before starting the swap. If a
  method is missing, that is SDK-side work and belongs back in
  `convergence.md`, not here — flag it rather than routing around the
  SDK's API from the consumer side.
- Parity oracle: impure (filesystem I/O). Use invariant and
  fault-ordinal tests per `convergence.md`'s "The parity oracle"
  section, not byte-equal fixtures.

### Set `GOPRIVATE`

Source: `convergence.md`, Stage 1, first bullet.

`GOPRIVATE=github.com/MiviaLabs/*` in both repositories and both
continuous-integration pipelines. A stage 1 prerequisite for the
provider-fixture and fault-ordinal work below, and for any future move
off a local `go.mod` `replace` directive. Cheap, do it early, do it
before it blocks something else.

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
