# Phase 46: system integration suite

Status: future. Plan-only; it has not yet gone through plan review.
Depends only on already-shipped packages. It ships no new top-level
package and needs no new `policy/layers.json` row.

## Why this phase exists

The user asked for a real, asserting integration test suite that
proves every currently-shipped package works together, using only
in-memory and reference implementations. Phase 42 (durable `ledger`
backend) and phase 43 (reference `channel.Notifier` transport) are
still plan-only. This phase scopes itself to work without either:
`ledger.MemStore` and a test-local `channel.Notifier`-shaped closure
stand in.

### Relation to phase 45

Phase 45 (`docs/plans/agents/phase45_agent_composition_example.md`)
adds one narrated, non-asserting doc example,
`docs/examples/agent-composition.md`. It shows `provider`, `tools`,
`mcp`, `ledger`, and `memory` composing around `agent.Run` through
existing seams. It is prose with one runnable program; a reader
verifies it by re-running the program and comparing printed output to
the doc text.

Phase 46 is a separate, sibling deliverable. It adds Go test files
with real `t.Fatal`/assertion calls in `agent/agent_test/`, gated by
`make verify`'s coverage floor on every run. It covers more packages
than phase 45's five, and it exercises behavior phase 45 does not
touch: panel waves, retry, fallback, checkpoint pause and resume,
approval gating, scheduled and triggered invocation. Phase 46 does not
supersede phase 45 and needs no change to it. Both stay in the repo:
one doc walkthrough, one asserting test suite.

### Relation to the existing system-integration test

`agent/agent_test/exchange_integration_test.go` and
`exchange_bench_test.go` (`agent.md`'s "System integration" section)
already prove a two-agent exchange. Read today, that test exercises
`identity`, `discovery`, `flow`, `envelope`, `events`, `machine`,
`room`, `tools`, `memory`, and one `a2a.ToPart`/`FromPart` hop — ten
packages. It does not touch `heartbeat`, `contextbudget`, `ledger`,
`channel`, `provider`, `scheduler`, or `trigger`, and it exercises
`flow` only on a plain sequential graph, with no panel wave, no retry,
no fallback, and no checkpoint.

This phase widens that proof to the current 21-package surface. It
adds two new test files beside the existing ones. It does not modify
`exchange_integration_test.go` or `exchange_bench_test.go`; both stay
as a regression pin for the exchange shape they already prove.

## Goal

Prove, with real assertions and no mock at a trust boundary, that
every shipped package still composes the way its own plan claims,
across one realistic request-response scenario and one realistic
scheduled-invocation scenario. Use only shipped, in-memory, or
reference-free implementations: `ledger.MemStore`, no durable backend;
a test-local `channel.Notifier`-shaped closure, no channel-package
reference transport.

## Scope

Inside:

- Two new integration test files in `agent/agent_test/`:
  `system_composition_integration_test.go` and
  `scheduled_trigger_integration_test.go`.
- Real values only: two `identity.Identity`, a `room.Room`, a
  `flow.Definition` with a panel wave, a retry-wrapped step, a
  fallback step, and a checkpoint pause/resume cycle, an `agent.Agent`
  driven through `agent.Run` with a real `heartbeat.Monitor` and a
  real, non-zero `contextbudget.Limits`, a `tools.Registry` gated by
  `tools.Scope.Approve`, a `provider.Completer` test double, a
  `memory.Store`, a `ledger.Ledger` over `ledger.NewMemStore()`, an
  `events.Bus`, and one `a2a.ToPart`/`FromPart` hop.
- A second, smaller scenario proving `scheduler.Scheduler` and
  `trigger.Registry` each wrap `agent.Run` as a plain closure, gated
  by a `channel.Notifier`-shaped stub, with `ledger` admission around
  the scheduled task and `events.Bus` recording the job's outcome.

Outside:

- Any change to `agent/agent_test/exchange_integration_test.go` or
  `exchange_bench_test.go`. Both stay unchanged; this phase adds new
  files, not edits.
- Any change to `docs/examples/agent-composition.md` or phase 45's
  plan. Phase 46 is additive, not a revision of phase 45.
- `mcp`. `mcp/client_test.go` and `mcp/connect_test.go` already prove
  `RegisterAll`'s mapping onto `tools.Tool` with a real stdio
  transport. Standing up a second MCP server inside this suite, only
  to register one more tool into a `Registry` this suite already
  builds, proves nothing `mcp`'s own tests do not already prove. This
  phase cites that proof instead of duplicating a live transport.
- `a2aclient`. `a2aclient.Client` needs a running `a2a-go` gRPC server.
  No reference server ships in this SDK, and standing one up inside a
  test would add a network dependency this phase's in-memory-only
  scope rejects. `a2a`'s wire-mapping functions, `ToPart` and
  `FromPart`, stay covered through the same one-hop use
  `exchange_integration_test.go` already established, reused in the
  new `system_composition_integration_test.go`.
- `durablefence`. It is a test-only conformance kit, imported from
  `ledger/ledger_test/`. It is not a subject of a cross-package
  integration test; `ledger`'s own plan and tests already cover it.
- A durable `ledger.Store` backend (phase 42) and a reference
  `channel.Notifier` transport (phase 43). Both stay plan-only; this
  phase uses `ledger.MemStore` and a test-local `Notifier`-shaped
  closure instead, and states that choice is deliberate, not a gap.
- A new `policy/layers.json` row or edge. Test files are exempt from
  the import policy; `scripts/check_deps.py` scans only non-`_test.go`
  files in each top-level package directory, never a `_test`
  subdirectory. No production code changes, so no edge is added.
- Any `api/*.txt` change. This phase adds no exported symbol to any
  package.

### Why two tests, not one

`docs/plans/agents/PHASES.md`'s integration-test contract frames an
integration test around proving a real path across one boundary, or
two blocks working together — not a single maximal scenario. A
request-response exchange (agent, tools, provider, ledger claim over
one task, memory, room admission) and a scheduled or triggered
invocation (scheduler or trigger wrapping `agent.Run`, ledger
admission around a recurring or event-fired task) are two genuinely
different shapes. Forcing both into one graph would need a step whose
purpose is only to let a scheduler fire it, tangled with a step whose
purpose is a tool call a human approves. Neither shape needs the
other's packages to prove its own composition. Two focused tests stay
readable and fail independently: a scheduler regression shows up in
`scheduled_trigger_integration_test.go` alone, not buried inside a
sixteen-package trace.

### Why `agent/agent_test/`

Both scenarios route through `agent.Run`, the composition layer
AGENTS.md names as the one package allowed to see every block. Per
`docs/plans/agents/PHASES.md`'s "Gate interactions" section, a test
subdirectory's coverage counts toward the package under test; a test
file in `agent/agent_test/` counts toward `agent`'s own coverage
floor, not toward `scheduler`'s, `tools`'s, or any other package's
floor. Since `agent` is the one package meant to touch everything,
that is the correct package for its coverage to land against. Placing
either test under a narrower package (for example `scheduler/
scheduler_test/`) would inflate that package's coverage denominator
with fifteen packages' worth of setup code unrelated to its own
concern, the opposite of what the floor should measure.

## API

No exported Go symbol is added, changed, or removed anywhere. No
`api/*.txt` lock changes. `make api-update` is not run in this phase.

## Tests

Both files live in `agent/agent_test/`, the existing external test
package that already imports `agent`, `identity`, `discovery`,
`flow`, `envelope`, `events`, `machine`, `room`, `tools`, and `memory`
via `exchange_integration_test.go`. Every value in both files is real;
no test builds a mock at a trust boundary.

### `system_composition_integration_test.go`

Fixture:

- Two `identity.Identity` values (`identity.New`), Agent A and Agent
  B.
- A `discovery.Card` for Agent A and an `agent.Agent` built through
  `agent.New`.
- A `room.Room` admitting both signers, with distinct roles.
- A `tools.Registry` holding one locally defined review tool. A
  `tools.Scope` with `Approve` set to a closure that logs a call and
  returns `(true, nil)` for a low-risk call and `(false, nil)` for a
  call at or above `ApprovalThreshold`, proving both branches of
  `RunScoped`'s approval gate. The `Approve` closure's signature
  matches `channel.Notifier`'s shape (`func(ctx, Question) (Answer,
  error)`) wrapped in a second closure that adapts it to
  `ScopeOptions.Approve`'s exact signature, the same adapter pattern
  `channel.md`'s `notifier_integration_test.go` already established
  for `agent.AckWait`. No reference `channel.Notifier` transport
  (phase 43) is needed; the test builds the closure by hand.
- A package-local `provider.Completer` test double (`Name`, `Chat`,
  `ChatStream`) returning a canned `Response`. `provider.RunTurn`
  drives one call before the plan is built, and its output seeds the
  gated step's `Payload`, following phase 45's plan-construction-time
  seam.
- A `memory.Store` (`memory.New`) holding one context blob, `Put`
  before the plan is built; the ref threads into the seeded payload.
- A `ledger.Ledger` over `ledger.NewMemStore()` and the same
  `events.Bus` the run uses. The test calls `Admit` and `Claim` before
  `agent.Run` starts, and `Complete` with the resulting status after
  `Run` returns, following `ledger.md`'s "task body" framing. This
  test states, in its file comment, that `MemStore` is deliberate: a
  durable `Store` backend is phase 42, out of scope here.
- A `flow.Definition` with: one two-member panel wave; one singleton
  step wrapped in a `flow.RetryPolicy` whose guard fails twice before
  it succeeds, proving the retry loop actually retries; one step whose
  `Fire` fails and is caught by an `AdmissionOnFailed` fallback step
  reading `FailureFrom`; and a pause/resume cycle triggered by
  canceling `ctx` right after a checkpoint fires mid-run. The test
  calls `flow.Resume` on the captured `Checkpoint` and asserts the
  resumed run reaches the same final `Report` an uninterrupted run
  would. The checkpoint asserted mid-pause carries a non-empty `Done`,
  a non-empty `Skipped` (from a route or admission skip earlier in the
  graph), and a non-empty `Failed` (from the already-caught fallback),
  proving `Checkpoint.Validate`'s one-list-per-step-ID invariant holds
  across all three lists at once in a real run, not only in
  `flow`'s own unit tests.
- A `heartbeat.Monitor` passed as `agent.Run`'s `hb` argument. The
  test asserts at least one `Beat` landed (through `Monitor.Alive`)
  during the run and that the id is `Forget`-ten after `Run` returns.
- A `contextbudget.Limits` with a `MaxBytes` value small enough that
  the scenario's total step-payload bytes would exceed it, but large
  enough that the earlier gated steps still fit. The test asserts the
  run stops with `agent.ErrOverBudget` on the step whose cumulative
  payload first exceeds the cap, proving the budget gates something
  real, not a zero/uncapped pass-through. A second, separate sub-test
  reruns the same graph with a generous `Limits` and asserts the run
  completes, isolating the budget's effect from every other mechanic
  in the graph.
- One `a2a.ToPart`/`FromPart` round trip on the confirmed step's
  message, matching `exchange_integration_test.go`'s existing pattern,
  reused here rather than re-derived.

Assertions:

- `agent.Run` returns the expected terminal `machine.Status` for the
  happy-path sub-test.
- The panel wave's two members both ran, in goroutines, with no
  `Confirm` call for either (matching the panel's existing documented
  gap).
- The retried step's guard ran exactly three times and the step ends
  `OutcomeSucceeded`.
- The fallback step's `FailureFrom` returns the failed step's ID and a
  wrapped error satisfying `errors.Is`.
- `Resume` reaches the same final `Report` as an uninterrupted run on
  an equivalent graph with no pause.
- `ledger.State` reports `StatusCompleted` after `Complete`, and a
  second `Complete` call with the same fence returns
  `ledger.ErrNotClaimed`, proving the claim is real, not a no-op.
- The declined tool call returns `tools.ErrToolDeclined` and never
  calls the tool's `Run`, proven by a call counter.
- The budget sub-test returns `errors.Is(err, agent.ErrOverBudget)`
  exactly at the step whose cumulative bytes first exceed `MaxBytes`.
- The `events.Bus` collected, in order:
  `agent.MessageDeliveredEvent`, `agent.MessageAckedEvent`,
  `flow.StepCompletedEvent` for every succeeded, retried, and
  fallback-caught step, `agent.ThreadVerifiedEvent`, and the ledger
  events (`ledger.AdmittedEvent`, `ledger.ClaimedEvent`,
  `ledger.CompletedEvent`). No `flow.StepCompletedEvent` fires for a
  skipped step, matching `flow.md`'s documented rule.

### `scheduled_trigger_integration_test.go`

Fixture:

- One `agent.Agent`, built the same way as the first test, with a
  short, single-step `flow.Definition` (no panel, no retry, no
  fallback — this test's concern is the scheduling wrapper, not the
  graph shape already proven above).
- A `ledger.Ledger` over `ledger.NewMemStore()`, admitting and
  claiming the task before invocation, completing it after.
- A `scheduler.Scheduler` with one `scheduler.Job` whose body is a
  closure: claim the ledger task, run `agent.Run`, complete the
  ledger task with the resulting status, and return the run's error
  unwrapped. The test drives the scheduler through one fire, using
  `scheduler.Every` with an interval short enough for one deterministic
  tick inside the test's timeout.
- A `trigger.Registry` with one `trigger.Condition`/`trigger.Action`
  pair whose `Action` is the same claim-run-complete closure shape,
  fired once by the test calling `Fire` directly (no polling loop),
  proving `trigger.Action`'s closure shape composes with `agent.Run`
  the same way `scheduler.Job`'s does.
- A test-local closure matching `channel.Notifier`'s func type,
  standing in for phase 43's not-yet-shipped reference transport, used
  as `agent.Run`'s `AckWait` to resolve the one gated step
  automatically for both the scheduled and the triggered run.
- The same `events.Bus`, asserting `scheduler.JobFailedEvent` never
  fires on the happy path, and a second sub-test whose `Job` body
  returns an error on purpose, asserting `JobFailedEvent` fires
  exactly once with the wrapped error.

Assertions:

- Both the scheduled run and the triggered run complete with
  `ledger.StatusCompleted` recorded for their respective idempotency
  keys.
- `scheduler.JobFailedEvent` fires only on the forced-failure sub-test,
  never on the happy path.
- The `channel.Notifier`-shaped stub receives exactly one call per
  gated step across both the scheduled and the triggered run.

## Verification

- `python3 scripts/check_plan.py` passes. This phase adds no new
  top-level package; the test subdirectory needs no separate plan
  entry.
- `python3 scripts/check_deps.py` passes. `policy/layers.json` is
  unchanged; the new test files import freely across package
  boundaries, since `check_deps.py` scans only non-`_test.go` files.
- `python3 scripts/check_prose.py`, `check_labels.py`, `check_docs.py`
  pass over this plan file and `docs/plans/agents/PHASES.md`.
- `make verify` passes: gofmt, vet, tests, the doc gate, the structure
  gate, the Semgrep scan and probes, and the coverage floor. The
  `agent` package's coverage and the module total both stay at or
  above 85 percent with the new files counted in.
- `go test -race ./agent/...` passes, covering both new files; each
  spawns goroutines through `flow`'s panel wave and through the
  scheduler's own internal ticking.
- No `api/*.txt` file changes; `make api-update` is not run.
- `docs/plans/agents/PHASES.md`'s phase order gains this phase, marked
  plan-only until it passes plan review.
