# Phase 46: system integration suite

Status: planned. Codebase-verified against the shipped tree. It ships
no new top-level package. It needs no new `policy/layers.json` row and
no `api/*.txt` change.

## Why this phase exists

The user asked for an asserting integration suite. The suite must
prove every shipped package works together as one system. It uses
in-memory and reference implementations only.

### Relation to phase 45

Phase 45 shipped `docs/examples/agent-composition.md`. That is one
narrated, non-asserting doc example. It shows `provider`, `tools`,
`mcp`, `ledger`, and `memory` composing around `agent.Run`.

Phase 46 is a sibling deliverable, not a revision. It adds Go test
files with real assertions in `agent/agent_test/`. The coverage floor
gates them on every `make verify` run. It covers more packages than
phase 45. It exercises panel waves, retry, fallback, checkpoint
resume, approval gating, scheduling, and triggering.

### Relation to the existing system-integration test

`agent/agent_test/exchange_integration_test.go` proves a two-agent
exchange today. It exercises `identity`, `discovery`, `flow`,
`envelope`, `events`, `machine`, `room`, `tools`, `memory`, and one
`a2a` hop. That is ten packages.

It does not touch `heartbeat`, `contextbudget`, `ledger`, `channel`,
`provider`, `scheduler`, or `trigger`. It runs `flow` on a plain
sequential graph only. It has no panel wave, no retry, no fallback,
and no checkpoint.

This phase widens that proof to the shipped 21-package surface. It
adds new files beside the existing ones. It does not modify
`exchange_integration_test.go` or `exchange_bench_test.go`.

## Goal

Prove, with real assertions, that every shipped package composes the
way its own plan claims. Use one request-response scenario and one
scheduled-invocation scenario. Mock nothing at a trust boundary.

## Verified API facts that shape this plan

The following facts come from reading the shipped source. Each one
corrects a guess an earlier draft of this plan made.

- `agent.Run` calls `flow.Run(ctx, a.plan, m, in, confirm, bus, nil)`.
  The `onCheckpoint` argument is hardcoded nil. No checkpoint fires
  through `agent.Run`. The checkpoint pause and resume cycle is
  therefore driven through `flow.Run` and `flow.Resume` directly, with
  an agent-shaped `flow.Confirm` closure. See `agent/run.go`.
- `events.Bus.Emit` returns an error when a name has no subscriber.
  `agent.Run` propagates that error and halts the step. Every fixture
  bus must subscribe every emitted name. See `events/bus.go` and
  `newRunBus` in `agent/agent_test/run_test.go`.
- `confirmStep` calls `EmitMessageDelivered` before the budget check.
  An over-budget step still emits `MessageDeliveredEvent`. It never
  reaches `hb.Beat`, `wait`, or `EmitMessageAcked`.
- `agent.Run` defers `hb.Forget(hbID)`. The beat id is gone once `Run`
  returns. A liveness assertion must read `Alive` from inside the
  `AckWait` closure, while the run is still in flight.
- A panel member step never reaches `confirmStep`. It builds no
  message, beats no heartbeat, and adds no budget bytes. That limit is
  already pinned by `run_panel_integration_test.go`.
- `memory.Store.Put` returns a content-addressed `string` ref. It is
  `envelope.ContextRef(string(content))`, not a distinct ref type.
- `ledger.Ledger.Admit` takes an `Actor`, an `IdempotencyKey`, a
  `Sequence`, a task value, a `time.Time`, and optional needs keys.
  `Claim` returns a `FenceToken` that `Complete` must present.
- Phase 42 shipped. `ledger.SQLiteStore` exists behind the
  `ledger_sqlite` build tag. Phase 43 shipped `NewNDJSONNotifier`.
  This phase still uses `ledger.MemStore` and a test-local
  `channel.Notifier` closure. Phase 47 wires the shipped transport.

## Scope

Inside:

- `agent/agent_test/system_composition_integration_test.go`. It runs
  the request-response scenario through `agent.Run`.
- `agent/agent_test/system_checkpoint_integration_test.go`. It runs
  the pause and resume cycle through `flow.Run` and `flow.Resume`,
  the only seam that fires a checkpoint.
- `agent/agent_test/scheduled_trigger_integration_test.go`. It proves
  `scheduler.Job` and `trigger.Action` each wrap `agent.Run`.
- `agent/agent_test/system_fixture_test.go`. It holds the shared
  fixture builders the three files above reuse. The split keeps every
  file under the 500-line cap.

Outside:

- Any change to `exchange_integration_test.go` or
  `exchange_bench_test.go`. Both stay as a regression pin.
- Any change to `docs/examples/agent-composition.md`.
- `mcp`. Its own tests already prove the mapping onto `tools.Tool`
  over a real transport. A second server proves nothing new here.
- `a2aclient`. It needs a running gRPC server. Phase 47 stands one up
  on a loopback listener; this phase stays in-process.
- `durablefence`. It is a test-only kit for `ledger`'s own suite.
- A new `policy/layers.json` row. `scripts/check_deps.py` scans only
  non-`_test.go` files, so test files import freely.
- Any `api/*.txt` change. This phase adds no exported symbol.

### Why three test files, not one

`docs/plans/agents/PHASES.md` frames an integration test around one
real path across a boundary. A request-response exchange and a
scheduled invocation are two different shapes. The checkpoint cycle is
a third, because it cannot route through `agent.Run` at all.

Forcing all three into one graph would need steps whose only purpose
is to satisfy another scenario's wiring. Three focused files stay
readable. A scheduler regression fails in the scheduler file alone.

### Why `agent/agent_test/`

Both scenarios route through `agent.Run`. AGENTS.md names `agent` the
one package allowed to see every block. A test file in
`agent/agent_test/` counts toward `agent`'s coverage floor.

`make verify` runs `go test -coverpkg=<pkg> ./<pkg>/<pkg>_test` per
package. New files there raise `agent`'s covered lines only. Placing
them under a narrower package would inflate that package's denominator
with unrelated setup.

## API

No exported Go symbol is added, changed, or removed. No `api/*.txt`
lock changes. `make api-update` is not run in this phase.

## Tests

All files live in `agent/agent_test/`. That external test package
already imports `agent`, `identity`, `discovery`, `flow`, `envelope`,
`events`, `machine`, `room`, `tools`, and `memory`.

Existing helpers are reused, not re-derived: `newRunBus`,
`newRunAgent`, `confirmingWait`, `lifecycleRecorder`, and
`equalNames`. Every value is real. No mock sits at a trust boundary.

### `system_fixture_test.go`

Budget: 220 lines. Every function stays at or below 80 lines.

- `systemFixture` bundles the shared state. It holds two
  `identity.Identity` values, an `agent.Agent`, a `machine.Definition`,
  a `room.Room`, a `tools.Registry`, a `tools.Scope`, a
  `memory.Store`, a `ledger.Ledger`, an `events.Bus`, a
  `heartbeat.Monitor`, and the call counters.
- `newSystemBus` subscribes every emitted name on one bus. It covers
  the three `agent` names, `flow.StepCompletedEvent`, the five
  `ledger` names this suite reaches, and `scheduler.JobFailedEvent`.
  An unsubscribed name would fail `Emit` and halt the run.
- `reviewTool` is a write-class tool. It implements `tools.Tool` and
  `tools.ProfiledTool`, reporting `ExecutionClassWrite`. It counts
  every `Run` call, so a declined call proves it never ran.
- `approvalNotifier` is a test-local closure with `channel.Notifier`'s
  exact type. A second closure adapts it to `ScopeOptions.Approve`.
  The adapter proves the two signatures compose.
- `cannedCompleter` implements `provider.Completer`. `Name`, `Chat`,
  and `ChatStream` return a canned `Response`.
- `admitAndClaim` calls `ledger.Admit` then `ledger.Claim`. It returns
  the `FenceToken`.

### `system_composition_integration_test.go`

Budget: 320 lines.

Fixture: the `systemFixture`, plus a `flow.Definition` with a
two-member panel wave, a retry-wrapped step, a failing step, and an
`AdmissionOnFailed` fallback step. `provider.RunTurn` runs once before
the plan is built. Its output seeds a step `Payload`. A
`memory.Store.Put` ref threads into that same payload.

Assertions:

- `agent.Run` returns the expected terminal `machine.Status`.
- Both panel members ran. Neither reached `Confirm`. This pins the
  documented panel gap.
- The retried step's `Fire` ran three times. The step ends
  `OutcomeSucceeded`.
- The fallback step reads `flow.FailureFrom`. It returns the failed
  step's ID and an error satisfying `errors.Is`.
- An approved tool call runs `reviewTool` exactly once.
- A declined tool call returns `tools.ErrToolDeclined`. The tool's run
  counter stays unchanged for that call.
- `heartbeat.Monitor.Alive` is true when read inside `AckWait`.
  `Monitor.Dead` is empty after `Run` returns.
- `ledger.State` reports `StatusCompleted` after `Complete`. A second
  `Complete` with the same fence returns `ledger.ErrNotClaimed`.
- One `a2a.ToPart` and `a2a.FromPart` round trip verifies the
  signature after the hop.
- `envelope.VerifyThread` accepts the captured message chain.
- The recorded event sequence matches the expected name list.

A separate budget sub-test reruns the graph twice. A generous
`contextbudget.Limits` completes. A tight `MaxBytes` returns
`agent.ErrOverBudget`. The tight case still records one
`MessageDeliveredEvent` for the failing step, and no
`MessageAckedEvent`, matching `confirmStep`'s verified order.

### `system_checkpoint_integration_test.go`

Budget: 200 lines.

Fixture: a `flow.Definition` whose graph produces one succeeded step,
one skipped step through a `Route`, and one failed step caught by an
`AdmissionOnFailed` fallback. The test passes a real `onCheckpoint`
hook to `flow.Run`. The hook captures each `Checkpoint`.

The `Confirm` closure is the same shape `agent.confirmStep` builds. It
signs a message with a real `identity.Identity` and chains it through
`PrevHash`. `agent.Run` cannot be used here, because it passes a nil
`onCheckpoint`.

Assertions:

- A captured mid-run `Checkpoint` carries a non-empty `Done`, a
  non-empty `Skipped`, and a non-empty `Failed`.
- `Checkpoint.Validate` accepts it. No step ID appears in two lists.
- `Checkpoint.Encode` and `flow.Decode` round trip it unchanged.
- `flow.Resume` from that checkpoint reaches the same final
  `Report.Outcomes` as one uninterrupted run.
- The resumed run re-runs no step already listed in `Done`.

The plan records one known limit. A still-pending fallback's
bookkeeping does not survive the round trip. The test pins that limit
rather than asserting behavior the contract does not promise.

### `scheduled_trigger_integration_test.go`

Budget: 260 lines.

Fixture: one `agent.Agent` with a short single-step plan. One
`ledger.Ledger` over `ledger.NewMemStore()`. One `scheduler.Scheduler`
with a `Job` closure that claims, runs, and completes. One
`trigger.Registry` with an `Action` closure of the same shape. A
test-local `channel.Notifier` closure resolves the gated step.

`scheduler.Run` blocks, so the test runs it in a goroutine and cancels
`ctx` once the job has fired. `scheduler.Every` supplies an interval
short enough for one deterministic tick.

Assertions:

- The scheduled run and the triggered run each record
  `ledger.StatusCompleted` for their own idempotency key.
- `scheduler.JobFailedEvent` never fires on the happy path.
- A forced-failure sub-test fires `JobFailedEvent` exactly once.
- `trigger.Registry.Fire` returns `trigger.ErrConditionNotMet` when
  the condition is false. The action never runs.
- The notifier stub receives exactly one call per gated step.

## Verification

- `python3 scripts/check_plan.py` passes. This phase adds no new
  top-level package.
- `python3 scripts/check_deps.py` passes. `policy/layers.json` is
  unchanged.
- `python3 scripts/check_names.py` passes. No filename carries a
  process word.
- `python3 scripts/check_prose.py`, `check_labels.py`, and
  `check_docs.py` pass over this plan file.
- `make verify` passes. That covers gofmt, vet, tests, the doc gate,
  the structure gate, Semgrep, the probes, and the coverage floor.
- `agent`'s coverage and the module total both stay at or above 85
  percent with the new files counted.
- `go test -race ./agent/...` passes. Both the panel wave and the
  scheduler spawn goroutines.
- No `api/*.txt` file changes. `make api-update` is not run.
