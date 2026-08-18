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
  existing composition seams. Depends only on already-shipped
  packages. It adds no code and no new `policy/layers.json` edge; see
  docs/plans/agents/phase45_agent_composition_example.md.
- Durability and reference gaps: phase 42, a `ledger.Store` backed by
  the pure-Go `modernc.org/sqlite` driver, behind a dedicated build
  tag, plus a bounded-entry-cap knob for `MemStore` in the default
  build; phase 43, an NDJSON-over-stdio `channel.Notifier` transport,
  shipped as real `channel` package API; phase 44, a `provider`
  token-estimation capability. Each depends only on its own
  already-shipped package (phase 34 `ledger`, phase 37 `channel`,
  phase 29 `provider`) and ships independently of the other two and of
  phase 45. See docs/plans/agents/phase42_ledger_durable_store.md,
  docs/plans/channel.md (phase 43's plan folded in on shipping), and
  docs/plans/agents/phase44_provider_token_estimation.md.
- Verification: phase 46, a system integration suite: two new
  `agent/agent_test/` files proving the current, widened package
  surface composes end to end, using `ledger.MemStore` and a
  test-local `channel.Notifier`-shaped closure in place of the
  still-plan-only phase 42 and phase 43 backends. It depends only on
  already-shipped packages, adds no exported symbol, and needs no new
  `policy/layers.json` row. It widens
  `agent/agent_test/exchange_integration_test.go`'s existing coverage
  without changing that file, and stays separate from phase 45's doc
  walkthrough. See
  docs/plans/agents/phase46_system_integration_suite.md.

Each plan names its phase number and its dependency on the prior phase.
Phase 35 depended on phase 14 (tools), which has since shipped.

Phases 22, 23, 25, 29, 30, 31, 32, 33, 34, 35, 36, 37, 39, 40, and 43
have shipped; see docs/plans/flow.md, docs/plans/durablefence.md,
docs/plans/ledger.md, docs/plans/provider.md, docs/plans/tools.md,
docs/plans/contextbudget.md, docs/plans/mcp.md, docs/plans/channel.md,
docs/plans/scheduler.md, and docs/plans/trigger.md. Phase 43's own
plan folded into docs/plans/channel.md on shipping; no standalone
phase 43 plan file remains. Phase 38 (flow
loop) is plan-only; it has not gone through plan review yet, but is
independently buildable now that phase 23 has shipped. Phase 45 (agent composition example) is
plan-only; it has not gone through plan review yet. It depends only on
already-shipped packages and adds no code, so it is independently
buildable now.

Phase 42 (ledger durable store) and phase 44 (provider token
estimation) are plan-only; neither has gone through plan review yet.
Each is independently buildable now, since phase 34 and phase 29 have
both shipped. Phase 42
weighed a stdlib-only file store against a database-backed store. The
user chose the database-backed option, first naming `go-libsql`, then
reconsidering, once that driver's cgo requirement and missing Windows
build were found, and naming the pure-Go `modernc.org/sqlite` instead,
the same driver `mivia-agent` already uses in production. The
resulting third-party exception is authorized, scoped to the
`ledger_sqlite` build tag; since the driver needs no cgo, the tag
exists only to keep a `MemStore`-only caller's dependency graph free
of it, not to gate a C-toolchain requirement. `MemStore` itself also
gains an optional, default-build `MaxEntries` cap in the same phase.
Phase 43 has shipped: an NDJSON-over-stdio
transport, matching `mivia-agent`'s own wire convention, as real
`channel` package API (`NewNDJSONNotifier`), not a `docs/examples/`
walkthrough only.

Phase 46 (system integration suite) is plan-only; it has not gone
through plan review yet. It is independently buildable now: every
package it exercises has already shipped, and it needs neither phase
42's durable backend nor phase 43's reference transport, using
`ledger.MemStore` and a test-local closure in their place.

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
