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
  escalation and notification channel abstraction. Phase 30 depends
  on phases 21 through 23 landing first. Phase 33 depends on phase 34.
  Phase 36 depends on phase 31 landing first. Phase 37 ships
  independently; it adds a new leaf package with no dependency on any
  other phase in this group. The rest ship independently of each
  other.

Each plan names its phase number and its dependency on the prior phase.
Phase 35 depends on phase 14 (tools) landing first.

Phases 29, 31, 32, 35, and 37 have shipped; see docs/plans/provider.md,
docs/plans/tools.md, docs/plans/contextbudget.md, docs/plans/mcp.md,
and docs/plans/channel.md. Phase 30 passed plan review in three rounds
and depends on phases 22 and 23 landing first. Phase 33 passed in
three rounds and depends on phase 34, which passed in five rounds;
both are ready to build. Phase 36 is plan-only; it has not gone
through plan review yet.

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
