# Package reference: e2e

The e2e package is the end-to-end scenario suite. Each scenario
wires real high-level blocks together and drives one full run,
asserting the outputs every layer promised. Package suites prove
each block alone; e2e proves the handoffs. The harness below mirrors
`api/e2e.txt`.

## Harness

- `NewAgent(name, plan)` — builds an agent under a fresh identity
  and a one-capability card.
- `PrefixTool` — returns its prefix joined to the payload, so each
  step records a distinct, deterministic result.
- `EscalateTool` — fails with an error wrapping `agent.ErrEscalated`,
  so a wired `Ask` round trip can resolve it.
- `Recorder` — `NewRecorder` builds one; `Handler` subscribes it;
  `Names` reports every observed event name in arrival order.
- `ThreadCapture` — `NewThreadCapture` builds one; `Wait` confirms
  each step message and records it; `Messages` returns the signed
  thread for `envelope.VerifyThread`.

## Fault kit

The fault kit is a named harness capability. Each decorator wraps one
seam as an interface and fails its `FaultOn`-th call with an error
wrapping `ErrFault`; every other call passes through.

- `ErrFault` — the sentinel each injected fault wraps. A failing run
  matches it through `errors.Is`.
- `FaultStore` — wraps a `ledger.Store`. Its `FaultOn`-th call faults.
- `FaultNotifier` — wraps a `channel.Notifier`. Its `FaultOn`-th ask
  faults.
- `FaultCompleter` — wraps a `provider.Completer`. Its `FaultOn`-th
  `Chat` or `ChatStream` call faults.
- `FaultWait` — wraps an `agent.AckWait`. Its `FaultOn`-th ack
  resolution faults.

A block behind a concrete type carries no decorator. memory's `Store`
is such a case and stays out of the kit.

## Scenarios

The scenarios live in `e2e/e2e_test/`, one file per composed
behavior:

- `pipeline_test.go` — one run through a sequential step, a panel
  wave, a sub-workflow, and a two-iteration loop.
- `thread_test.go` — thread integrity across hops, plus replay
  determinism on a fresh thread.
- `escalation_test.go` — an escalated step resolved by a human over
  the NDJSON transport, approved and declined.
- `taskrun_ceremony_test.go` — the ledger ceremony around one full
  pipeline run, with blocked and replayed tasks.
- `subagent_parallel_test.go`, `subagent_tools_test.go`,
  `subagent_observe_test.go`, `subagent_depth_test.go`, and
  `subagent_messaging_test.go` — the subagent system: concurrent
  spawns, internal tools, live observation, the depth bound, and the
  two-direction message plane with a room admission.
- `sqlite_ceremony_test.go` — behind the `ledger_sqlite` tag, the
  ceremony over a SQLite file that is closed and reopened.
- `faults_store_test.go` — a ledger store faults mid-ceremony in a
  two-step pipeline; the run names the fault and step one's artifact
  survives.
- `faults_notifier_test.go` — a channel notifier dies mid-ask; the
  escalation surfaces as a step failure, not a hang.
- `faults_subagent_test.go` — one of three subagents fails through
  `RunAll`; the siblings land and the failing spec's `Err` is
  reported.

See [../plans/e2e.md](../plans/e2e.md) for the scenario map and the
growth backlog: a remote subagent over `a2aack` and `dispatch`, MCP
tools behind the chain, and scheduled liveness.

- The mivia-agent parity scenarios — `bugfix_flow_test.go`,
  `panel_review_test.go`, `delivery_repair_test.go`, and
  `feature_delivery_test.go` — mirror the sibling repo's workflow
  shapes: verdict routing, refinement loops, evidence repair, panel
  partial failure, delivery metadata repair, and the human merge.
- `wiring_scenario_test.go` — the four newest blocks composed at
  once: a traced orchestrator, a hook-observed step, a spawned
  subagent, and a registry fallback to a usage-wrapped provider.
- `wiring_edge_test.go` — the wiring's failure and edge cases: the
  pre-tool veto before the spawn, `ErrAllFailed` at the top,
  isolated session totals, and the stop-hook veto after the walk.

## Failure modes

The fault kit owns one sentinel, `ErrFault`. Every injected fault
wraps it, so a scenario asserts `errors.Is(runErr, e2e.ErrFault)`.
`EscalateTool.Run` wraps `agent.ErrEscalated`, so a caller with no
`Ask` wired sees the run fail with an error matching
`agent.ErrEscalated`. Pinned by `e2e_test/escalation_test.go`.

## Invariants

- A scenario composes at least two high-level blocks. A single
  package's behavior belongs in its own suite.
- No scenario substitutes a stand-in for an SDK block. Tools,
  answers, and clocks may be scripted; blocks never.
- Every scenario fails when its wiring breaks. Each drop proves it
  once with a planted fault in a throwaway copy.
