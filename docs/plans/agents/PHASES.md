# Phases: how we build the agent work

This document is the framework for the phase plans. It defines a phase,
the test layout for every phase, and the test contract for each test
kind. It lists the phase order. The phase plans live in
`docs/plans/agents/`. Read this file before any phase plan.

## A phase

A phase is the smallest unit of implementation that ships. It builds one
slice of one block. A block from the research table often splits into
two or more phases. A phase is small enough to review in one sitting.

Every phase has the same shape. It has a plan in `docs/plans/agents/`.
It has code in the target package. It has one directory of tests. It
ends green under `make verify`.

A phase never ships unfinished. A phase owns its tests and its doc. A
phase does not pull in a later phase.

## Test layout

The flat layout puts every test in one subdirectory. The concern under
test and the test kind live in the filename, not in folders.

```text
<package>/<package>_test/
  <concern>_integration_test.go
  <concern>_test.go
  <concern>_bench_test.go
```

- `<concern>` names the behavior under test, such as `status` or `fire`.
- `_integration_test.go` holds the end-to-end tests.
- `_test.go` holds the red-green unit tests for this phase.
- `_bench_test.go` holds the benchmarks and the allocations.
- No file basename carries a process-artifact word: `phase`, `tdd`,
  `perf`, `wip`, `draft`, `scratch`, `tmp`, `old`, `backup`, or a
  version suffix. `scripts/check_names.py` rejects those. Name the
  file for the concern under test, not the phase or the test kind's
  old label.
- The directory is a Go test package that imports the package under
  test. It sits inside the package directory, so the plan gate in
  `scripts/check_plan.py` does not rescan it as a new top-level package.

A phase may merge files only when a test kind has no case yet. That is
rare. Prefer one file per kind so the contract stays readable.

## The test contracts

### Integration tests

An integration test exercises the real path across a boundary. It
calls other packages. It uses the real wire form. It never mocks the
trust boundary.

For a message phase, the integration test runs the full ladder:
sign, encode, transport, decode, verify, admit, ack, thread. For a
workflow phase, it runs a real graph end to end. It proves two blocks
work together.

### Red-green tests

A red-green test is the red-green loop for one unit of behavior. It
asserts the intended behavior before the code exists. It must fail on
the empty implementation. Then the behavior lands and the test turns
green.

The file starts with the assertions only. The builder writes them
first, runs the test, and records the red. Then the builder writes the
smallest code that makes them pass. Then the builder refactors without
changing the assertions.

An empty red-green file that never saw red is a failed contract. The
plan records the red step for each case.

### Benchmark tests

A benchmark file holds a Go benchmark. It measures time and allocations.
`AllocsPerRun` states the allocation budget. The baseline runs before
the phase, so the improvement is measurable.

The plan records a measured baseline for each benchmark. A benchmark
without a baseline documents nothing. The `make bench` target runs
them.

A benchmark may skip the allocation budget when the count depends on
non-deterministic runtime overhead, such as goroutines, channels, or
closures. It reports the allocs/op ratio instead. The phase plan
states the rationale for the exception.

## Phase order

The phases build in dependency order. Foundations come first. The
composition comes last.

- Foundation: machine status model, machine move dispatch, machine
  wire form, flow step graph, flow sequential runner, flow parallel
  panels, flow chaining.
- Transport and identity: identity key wrap, a2a mapping, a2a client,
  discovery agent card, MCP tool-calling client.
- Composition: agent definition, agent execution loop, tools
  registry, memory context store.
- System: the end-to-end two-agent exchange.
- Reaction: events core, machine emit, flow emit, envelope emit. The
  reaction phases ship when their dependencies allow it.
- Liveness: agent step-liveness heartbeat, room membership staleness.
  Both build on the shipped `heartbeat` package and ship independently
  of each other.
- Model and durability: model provider interface, flow retry policy,
  tools capability markers, agent context budget, durable-fence
  conformance kit, ledger durable admission, tool approval gating,
  escalation and notification channel abstraction, flow loop
  iteration, scheduled invocation, and trigger composition. Phase 30
  and phase 38 each depended on phase 23 landing first; phase 23 has
  since shipped, so both are now independently buildable. Phase 33
  depended on phase 34; both have since shipped. Phase 36 depended on
  phase 31, which has since shipped. Phase 37, phase 39, and phase 40
  each shipped as a new leaf-shaped package with no dependency on any
  other phase in this group. The rest ship independently of each
  other.
- Documentation: phase 45, a runnable example composing `agent.Run`
  with `provider`, `tools`, `mcp`, `ledger`, and `memory` through their
  existing composition seams (shipped; see docs/packages/agent.md and
  docs/examples/agent-composition.md; no standalone phase 45 plan file
  remains). Depended only on already-shipped packages. It added no
  code and no new `policy/layers.json` edge.
- Durability and reference gaps: phase 42, a `ledger.Store` backed by
  the pure-Go `modernc.org/sqlite` driver, behind a dedicated build
  tag (shipped; see docs/plans/ledger.md); phase 42b, a
  bounded-entry-cap knob for `MemStore` in the default build, split
  out of phase 42 to stay independently reviewable and revertible
  (shipped; see docs/plans/ledger.md); phase 43, an NDJSON-over-stdio
  `channel.Notifier` transport, shipped as real `channel` package API;
  phase 44, a `provider` token-estimation capability. Each depends
  only on its own already-shipped package (phase 34 `ledger`, phase 37
  `channel`, phase 29 `provider`) and ships independently of the
  others and of phase 45. See docs/plans/channel.md (phase 43's plan
  folded in on shipping). Phase 42c is a follow-on to phase 42: it
  adds an `Actor` type and
  `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt` fields to
  `TaskState`, threaded through every `Ledger` mutating method, plus
  matching `SQLiteStore` columns and a startup migration for a
  database file created under the pre-42c schema. It depends only on
  phase 42, which has shipped, and is an exported-API break for
  `Ledger`'s `Admit`, `Claim`, `Renew`, `Release`, `Takeover`, and
  `Complete` (shipped; see docs/plans/ledger.md).
- Verification: phase 46, a system integration suite: two new
  `agent/agent_test/` files proving the current, widened package
  surface composes end to end, using `ledger.MemStore` and a
  test-local `channel.Notifier`-shaped closure in place of the tag-
  gated `SQLiteStore` backend and the shipped phase 43
  transport. It depends only on
  already-shipped packages, adds no exported symbol, and needs no new
  `policy/layers.json` row. It widens
  `agent/agent_test/exchange_integration_test.go`'s existing coverage
  without changing that file, and stays separate from phase 45's doc
  walkthrough.
- Concurrency verification: phase 47, a concurrency integration
  suite: the race detector joins `make verify`, and five new test
  files close the gaps phase 46 leaves open: concurrent `agent.Run`
  calls over shared blocks, the `channel.Notifier`-to-approval wiring,
  the `ledger` Renew-Complete race, `mcp` concurrent calls, and a
  loopback `a2a-go` server round trip. It depends on phase 46 and on
  already-shipped packages, adds no exported symbol, and needs no new
  `policy/layers.json` row. It has shipped; its plan folded into this
  file on shipping.
- Composition wave: phase 48, run-time payload resolution on
  `flow.Step` through a `PayloadFrom` func reading the live record;
  phase 49, `agentrun`, the config-struct composition layer over
  `agent.Run` with up-front validators, including the
  plan-versus-machine transition-matrix check; phase 50, `taskrun`,
  the ledger admit-claim-complete ceremony around a work func;
  phase 51, `a2aack`, the remote step ack over `a2aclient`; phase
  52, `dispatch`, a stdlib NDJSON and HTTP envelope endpoint for the
  receive ladder; phase 53, `a2aserver`, an `a2a-go` server bridge,
  deferred behind two gates. See each phase plan under
  docs/plans/agents/.

Each plan names its phase number and its dependency on the prior phase.
Phase 35 depended on phase 14 (tools), which has since shipped.

Phases 22, 23, 25, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 42,
and 43 have shipped; see docs/plans/flow.md, docs/plans/durablefence.md,
docs/plans/ledger.md, docs/plans/provider.md, docs/plans/tools.md,
docs/plans/contextbudget.md, docs/plans/mcp.md, docs/plans/channel.md,
docs/plans/scheduler.md, and docs/plans/trigger.md. Phase 38 (flow
loop) shipped; see docs/plans/flow.md's Phase 38 subsection. Phase 43's
own plan folded into docs/plans/channel.md on shipping; no standalone
phase 43 plan file remains. Phase 45's own plan folded into
docs/packages/agent.md and docs/examples/agent-composition.md on
shipping; no standalone phase 45 plan file remains. It depended only
on already-shipped packages and added no code to any package. Phase
47's own plan folded into this file's phase 47 paragraph on shipping;
no standalone phase 47 plan file remains.

Phase 42 (ledger durable store) has shipped: `SQLiteStore` landed
behind the `ledger_sqlite` build tag, backed by the pure-Go
`modernc.org/sqlite` driver. The user chose the database-backed option
over a stdlib-only file store, first naming `go-libsql`, then
reconsidering, once that driver's cgo requirement and missing Windows
build were found, and naming `modernc.org/sqlite` instead, the same
driver `mivia-agent` already uses in production. The resulting
third-party exception is authorized, scoped to the `ledger_sqlite`
build tag; since the driver needs no cgo, the tag exists only to keep
a `MemStore`-only caller's dependency graph free of it, not to gate a
C-toolchain requirement. Phase 42b (`MemStore`'s `MaxEntries` cap) and
phase 44 (provider token estimation) are plan-only; neither has gone
through plan review yet. Each is independently buildable now, since
phase 34 and phase 29 have both shipped. Phase 43 has shipped: an
NDJSON-over-stdio
transport, matching `mivia-agent`'s own wire convention, as real
`channel` package API (`NewNDJSONNotifier`), not a `docs/examples/`
walkthrough only.

Phase 46 (system integration suite) has shipped. It adds four test
files to `agent/agent_test/`. It uses `ledger.MemStore` and a
test-local closure in place of a durable backend and the reference
transport. Its checkpoint case drives `flow.Run` and `flow.Resume`
directly, because `agent.Run` passes a nil `onCheckpoint`.

Phase 47 (concurrency integration suite) has shipped. It adds
`go test -race ./...` to `make verify` and five test files. Those
files close the concurrent-run, approval-transport, ledger race,
`mcp` concurrency, and loopback gRPC gaps. It strengthens
`make verify`; it weakens no existing gate.

Phase 48 ships in this change. It widens the shipped `flow` package with
`Step.PayloadFrom` and the `Definition.Steps` and `Definition.Panels`
accessors, and adds the `agent` `Plan` and
`Signer` accessors ahead of phase 49's validators. The shipped
contract lives in docs/plans/flow.md and docs/plans/agent.md.

Phase 50 (`taskrun`) has shipped. It adds the ledger
admit-claim-complete ceremony around a work func as one top-level
package: admission with dependency keys, claim with lease and fence,
work execution, and completion with the mapped status. It depends only
on the shipped `ledger` package and adds one `policy/layers.json`
edge. Its package plan lives at docs/plans/taskrun.md.

Phase 49 (`agentrun`) has shipped. It adds the config-struct
composition layer over `agent.Run` as one top-level package, with
up-front validators including the plan-versus-machine transition-matrix
check. It depends on phase 48 and adds one `policy/layers.json` edge.
Its package plan lives at docs/plans/agentrun.md; no standalone
phase 49 plan file remains.

Phases 51 and 52 each add one package and depend on no unshipped
phase; they shipped independently of each other and of phase 49.

Phase 51 (`a2aack`) has shipped. It turns a remote A2A task round
trip into the composition layer's `AckWait` as one top-level package,
over `a2aclient`. It adds one `policy/layers.json` edge. Its package
plan lives at docs/plans/a2aack.md; no standalone phase 51 plan file
remains.

Phase 52 (`dispatch`) has shipped. It adds the NDJSON envelope
endpoint and client as one top-level package: an `http.Handler` runs
the receive ladder per line and answers with confirmed acks or
per-line error objects; `Send` posts a batch and collects results in
order. `EmitMessageDelivered` and `EmitMessageAcked` are best-effort
diagnostics, not ladder stages. It depends on no unshipped phase and
adds one `policy/layers.json` edge. Its package plan lives at
docs/plans/dispatch.md; no standalone phase 52 plan file remains.

Phase 53 is plan-only and not scheduled. It builds nothing until two
gates open: phase 52 proving the receive ladder, and the user
authorizing a widened `a2a-go` import exception. See
docs/plans/agents/phase53_a2a_go_server.md.

Phase 54 (`scripts/check_mutation.py`) has shipped its first rollout
step. It adds no package: one stdlib-only script applies text-level
operator mutations per package, runs the package suite per mutant,
and checks the result against a stored per-package kill floor.
`--probe` joins `make verify`; a full sweep stays a separate
`make mutation` tier and never joins `verify-fast`. `envelope`,
`machine`, and `ledger` hold measured floors; the remaining packages
are open future work. No standalone phase 54 plan file remains.

Phase 55 (`subagent`) has shipped. It adds one package: `AsTool`
wraps a built runner as a `tools.Tool`, `RunAll` joins concurrent
spawns behind a ctx-carried depth guard, ten internal tools expose
the SDK's blocks, and a signed-message mailbox carries both
directions between orchestrators, subagents, and humans. Its
package plan lives at docs/plans/subagent.md.

Phase 56 (`providerregistry`) has shipped. It adds one package:
`Registry` holds named `provider.Completer` values, and `Route`
walks a caller-chosen order of names through `provider.RunTurn`.
`Route` falls through to the next name only when the caller's
`Retryable` predicate approves the failure. It depends on the
shipped `provider` package and one `policy/layers.json` edge. Its
package plan lives at docs/plans/providerregistry.md; no
standalone phase 56 plan file remains.

Phase 57 (`hooks`) has shipped. It adds one package: a named,
multi-handler registry for a lifecycle point. `Point` names the
point (`PointPreTool`, `PointPostTool`, `PointStop`); `Fire` runs
every handler at a point in registration order and stops at the
first veto. It depends on no unshipped phase and imports no other
package in this module. It ships with no caller, the same way
phase 14 shipped `tools`. Its package plan lives at
docs/plans/hooks.md; no standalone phase 57 plan file remains.

Phase 58 (`usage`) has shipped. It adds one package: an
`Accumulator` records one `provider.Usage` per call, keyed by a
caller-supplied session identifier. `Total` sums every field for
a session, `Reset` clears one session, and `Record` is safe for
concurrent use. It reuses `provider.Usage`, so one token-count
definition serves the whole SDK. It depends on the shipped
`provider` package and adds one `policy/layers.json` edge. Its
package plan lives at docs/plans/usage.md; no standalone phase 58
plan file remains.

Phase 59 (`trace`) has shipped. It adds one package: a `Span`
records one named operation, a `Tracer` links spans through `ctx`,
and `SpanFrom` reads the current span back. `End`, `SetAttribute`,
and `Attributes` are safe for concurrent use on one shared span. It
depends on no unshipped phase and imports no other package in this
module. It ships with no caller, the same way phase 57 shipped
`hooks`. Its package plan lives at docs/plans/trace.md; no
standalone phase 59 plan file remains.

Phase 60 has shipped. It changes no package surface: a loop child
that ends every iteration on one status is representable, because
the parent re-enters without a transition row when its standing
already matches the child final. The parity workaround stays valid
but is no longer required. No standalone phase 60 plan file
remains.

Phase 61 has shipped. It swaps the admission zero value: a step
runs only when every need succeeded, and skip tolerance is the
explicit opt-in. Route exclusion propagates by default, matching
the sibling repo's transition-driven readiness. No standalone
phase 61 plan file remains.

Phase 63 (`skills`) has shipped. It adds one leaf package: `Skill`
carries a name, instructions text, trigger phrases, and required
tool names; `Skill.Validate` checks a non-blank name, non-blank
instructions, and no blank or duplicate (case-insensitive) trigger
entry. `Registry` holds skills by name behind a `sync.RWMutex`;
`Add`, `Get`, `Remove`, and `Names` match `tools.Registry`'s
concurrency shape and contracts. `Match` returns every registered
skill whose `Triggers` list contains the query under
`strings.EqualFold`, sorted by `Name`, mirroring
`discovery.Card.Match`'s comparison rule without reusing its type. A
skill is read, not called: it carries no `Run` method. It depends on
no unshipped phase and imports no other package in this module. It
ships with no caller, the same way phase 57 shipped `hooks`. Its
package plan lives at docs/plans/skills.md.

Phase 69 (`agentloop`) has shipped. It adds one composition package:
`Run` takes a `provider.Completer`, a `tools.Registry`, and a
starting message list, offers the registry's tools to the model,
runs the tool calls the model requests, appends the results as
`RoleTool` messages, and repeats until the model returns no tool
call or a bound trips. It depends on the shipped phase 62
(`Message.ToolCalls`) and adds no import cycle. Its package plan
lives at docs/plans/agentloop.md; no standalone phase 69 plan file
remains.

Phase 71 (file tools, gated by a mandatory secret policy) has
shipped. It corrects the five `subagent` file tools shipped without
an enforced secret policy: `FileToolOptions{Root, Deny
*secretpath.Matcher, MaxReadBytes}` and `OpenFileTools` now gate
every `WorkspaceReadTool`/`WorkspaceWriteTool`/`WorkspaceListTool`/
`WorkspaceStatTool`/`DiffTool` behind a mandatory `Deny` matcher;
`FileToolOptions.Validate` rejects a nil `Deny` before any workspace
opens. It adds no new package. Its addendum lives in
docs/plans/subagent.md's "File tools addendum"; no standalone phase
71 plan file remains.

Phase 74 is plan-only and not scheduled. It is the mutation kit's
second rollout step: seven new per-package floors (`workspace`,
`subagent`, `agentloop`, `mcp`, `dispatch`, `a2aclient`, `schema`),
a re-measured `ledger` floor, and a new `make mutation-gate` target
that runs every floored package's sweep on demand. It depends on the
shipped phase 54 kit and adds no new package. See
docs/plans/agents/phase74_mutation_coverage_rollout.md.

Phase 75 (orphan-package gate and probe-basename collision gate) has
shipped. It adds `scripts/check_orphan_packages.py`, which fails on
a top-level package with zero non-test internal callers unless
`policy/pending_wiring.json` declares it with a reason, a target,
and a `permanent` flag; `make verify-fast` and `make verify --probe`
both run it. It also adds a post-write basename-collision check
inside `scripts/check_semgrep_probes.py`, so two scoped-rule probes
sharing one fixture basename fail loudly instead of silently
overwriting each other's expected entry. It adds no new package. No
standalone phase 75 plan file remains.

Phase 76 (agentrun schema-tool argument decode) has shipped. It fixed
a confirmed gap `agentrun.Runner.chain` left open: a resolved tool
implementing `tools.SchemaTool` never got its `DecodeArguments`
called, so the five `subagent` file tools phase 72 bound to a `Kind`
could not drive a real end-to-end `Runner.Run`. It also fixed
`runconfig`'s `stepTool` wrapper, which dropped `tools.ProfiledTool`,
`tools.ResultBudgetTool`, and `tools.PrivilegedTool` from every
wrapped tool, following `spool/tool.go`'s capability-composition
pattern. It added no new package and no new `policy/layers.json`
edge. See docs/plans/agents/phase76_agentrun_schema_tool_decode.md.

Phase 77 (workspace list and stat tools return a JSON string result)
has shipped. It fixed a confirmed gap phase 76's addendum left open:
`subagent.WorkspaceListTool.Run` and `subagent.WorkspaceStatTool.Run`
returned a typed struct in `tools.Out.Value`, not a string, so
`agentrun`'s `chain` failed both `runconfig.WorkspaceListKind` and
`runconfig.WorkspaceStatKind` with `ErrResultNotText` on every real
`Runner.Run`. It changed both tools to return a JSON-encoded string,
matching every other `subagent` tool's result convention. It added no
new package and no new `policy/layers.json` edge. See
docs/plans/agents/phase77_workspace_list_stat_json_result.md.

## Gate interactions

The plan gate scans top-level Go directories. A test subdirectory
sits inside the package, so it needs no separate plan. The coverage
floor counts every package. The test package adds coverage to the
package under test. The builder confirms the floor still holds after
each phase.

The prose gate scans this file and every phase plan. The label gate
scans for letter-digit tokens. The drift scan rejects unfinished-work
markers. Every phase plan stays inside those rules.

All prose in this file and in the phase plans is simplified technical
English. One idea per sentence. Sentences stay at or below 25 words.
