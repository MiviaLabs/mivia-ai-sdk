# Phase 47: concurrency integration suite

Status: future. Plan-only; it has not yet gone through plan review.
Depends on phase 46, which is in implementation now, and on
already-shipped packages only. It adds no exported symbol and no new
`policy/layers.json` row.

## Why this phase exists

A gap audit compared phase 45 and phase 46 against every integration
and concurrency test already in the repo. Phase 46 proves the shipped
surface composes in two sequential scenarios. Its race-detector run is
a manual verification step, not a gate. The audit confirmed six gaps
that neither plan closes.

- Gap one: no gate runs the race detector. `make verify` and
  `make verify-fast` run plain `go test ./...`. Only
  `make verify-ledger-sqlite` passes `-race`. Every
  "run under `go test -race`" comment outside `ledger` is unenforced.
- Gap two: no test runs concurrent `agent.Run` calls that share the
  SDK's stateful blocks. `agent/agent_test/liveness_integration_test.go`
  races two runs against one `heartbeat.Monitor` only. Nothing races
  runs against one `memory.Store`, one approval-gated
  `tools.Registry`, one `ledger.Ledger`, or one `events.Bus`.
- Gap three: `docs/plans/channel.md` records an open gap. No test
  wires a `channel.Notifier` into `tools.ScopeOptions.Approve`. The
  shipped `NewNDJSONNotifier` never drives a tool approval end to end.
- Gap four: `ledger`'s race matrix covers Claim, Admit, Takeover,
  Renew against Renew, and Complete against Complete. No test races
  `Renew` against `Complete` on one key.
- Gap five: `mcp/client.go` claims concurrent `ListTools`, `CallTool`,
  and `CallToolWithProgress` are safe. Only the progress-token case
  has a concurrency test.
- Gap six: `a2aclient`'s tests never reach the real gRPC transport.
  The stub transport covers mapping and concurrency. No test stands up
  a server. The dial, the wire round trip, and the post-hop signature
  re-verification over a real transport stay unproven.

## Relation to phase 45 and phase 46

Neither plan changes. Phase 45 ships a doc walkthrough; this phase
touches no example. Phase 46 ships two sequential scenarios; this
phase adds the concurrent scenarios and turns phase 46's manual race
run into a gate. Where phase 46 substitutes a test-local closure for
the channel transport, this phase wires the shipped transport instead.
Where phase 46 excludes a live `a2a-go` server, this phase stands one
up on a loopback listener. A loopback listener keeps phase 46's
in-memory scope: no test needs an external network, a subprocess, or
a third-party service. `mcp/http_transport_test.go` already sets the
loopback precedent.

## Goal

Prove, under an enforced race detector, that concurrent `agent.Run`
calls stay correct when they share the SDK's stateful blocks. Close
the five remaining composition and race gaps with focused test files.
Change no production Go code.

## Scope

Inside:

- One new step in `make verify`: `go test -race ./...` over the
  default build, after `verify-fast` and before the coverage block.
  `verify-fast` stays unchanged, so the pre-commit hook keeps its
  current runtime.
- One new file
  `agent/agent_test/concurrent_composition_integration_test.go`. It
  races concurrent `agent.Run` calls against shared blocks. Placement
  follows phase 46's reasoning: `agent` is the composition layer, and
  the coverage counts toward `agent`'s floor.
- One new file
  `channel/channel_test/notifier_approval_integration_test.go`. It
  wires the shipped `NewNDJSONNotifier` into `tools.ScopeOptions.Approve`.
  On shipping, the gap note in `docs/plans/channel.md` gains one line
  naming the new test.
- One new file `ledger/ledger_test/renew_complete_race_test.go`. It
  races `Renew` against `Complete` on one key.
- One new file `mcp/client_concurrency_test.go`. It races `ListTools`
  and `CallTool` over the in-memory transport.
- One new file `a2aclient/grpc_loopback_integration_test.go`. It
  stands up an `a2a-go` server on a loopback listener and runs the
  full client round trip, sequential and concurrent.
- One new entry in `docs/plans/agents/PHASES.md`, marked plan-only.

Outside:

- Any change to phase 45's or phase 46's plan, code, or tests.
- Any exported symbol, in any package. No `api/*.txt` lock changes.
  `make api-update` is not run.
- Any production Go change. A new test that exposes a real defect
  stops the phase. The defect escalates to the user; this phase does
  not patch it in place.
- Any `policy/layers.json` change. `scripts/check_deps.py` scans only
  non-`_test.go` files, so the new test files import freely.
- `durablefence`. `ledger`'s scenario tests already cover it.
- Separate concurrency tests for `envelope`, `identity`, `machine`, or
  `contextbudget`. The shared-suite runs sign and verify through real
  identities in every goroutine, exercising the stateless blocks'
  concurrent use implicitly. Their own contracts claim no shared
  mutable state.
- The pinned scope limits in
  `agent/agent_test/run_panel_integration_test.go` and
  `run_budget_test.go`. Panel waves skip the budget check and the
  heartbeat beat by design. Closing those pins needs a design change,
  not a test.
- `scheduler.Run`'s documented second-`Run` hazard. `scheduler/run.go`
  names a second concurrent `Run` an undefended caller error. This
  phase does not test an error the contract already owns.
- External network access of any kind. The `a2aclient` server binds a
  loopback port only.

## API

No exported Go symbol is added, changed, or removed. Every `api/*.txt`
lock stays byte-identical. `make api-update` is not run in this phase.
The Makefile change adds no Go API surface.

## Tests

### `concurrent_composition_integration_test.go`

Fixture:

- Eight goroutines, each driving one `agent.Run`. Each run owns its
  identity, card, and a short two-step `flow.Definition`. All eight
  share one `memory.Store`, one `tools.Registry`, one `ledger.Ledger`
  over `ledger.NewMemStore()`, one `events.Bus`, and one
  `heartbeat.Monitor`.
- The shared registry holds one tool behind a `tools.Scope` whose
  `Approve` closure counts calls and approves. Each run's `AckWait`
  calls `RunScoped` and puts the tool result into the shared store.
- Each goroutine calls `ledger.Admit` with its own idempotency key,
  then `Claim`, then `agent.Run`, then `Complete` with the run's
  status.
- A contention sub-test: two goroutines call `Admit` with the same
  key. Exactly one wins.
- A wrapper sub-test: one `scheduler.Job` and one `trigger.Action`
  both wrap `agent.Run`. The test fires both concurrently against the
  shared bus.
- A counting `events.Handler` with atomic counters records every
  event by kind.
- A watchdog timer fails the test if any goroutine wedges.

Assertions:

- All eight runs reach the expected terminal `machine.Status`.
- Every ledger key reports `StatusCompleted`. The contention sub-test
  records exactly one `AdmittedEvent` for the shared key.
- The `Approve` counter and the tool's run counter each equal the
  gated-step count.
- Every `memory.Store.Put` is retrievable by its ref. No blob comes
  back corrupted.
- Event counts per kind equal the per-run counts exactly.
- `heartbeat.Monitor.Dead` is empty after all runs return.
- The whole file passes under `go test -race`.

### `notifier_approval_integration_test.go`

Fixture:

- A real `NewNDJSONNotifier` over an `io.Pipe` pair. A peer goroutine
  reads each `Question` and writes a fixture `Answer`.
- A second closure adapts the notifier to `ScopeOptions.Approve`'s
  `(bool, error)` signature, per the adapter pattern in
  `docs/plans/channel.md`.
- A `tools.Registry` with one write-class tool behind that scope.

Assertions:

- An approving peer answer runs the tool exactly once.
- A declining peer answer returns `tools.ErrToolDeclined`. The tool's
  run counter stays at zero for that call.
- Both answers crossed the real NDJSON wire, proven by the peer's
  read count.
- On shipping, `docs/plans/channel.md`'s gap note names this test.

### `renew_complete_race_test.go`

Fixture:

- One admitted and claimed key on a `ledger.Ledger` over
  `ledger.NewMemStore()`. The barrier-store pattern from
  `ledger/ledger_test/renew_race_test.go` forces the collision.

Assertions:

- Two goroutines race `Renew` against `Complete`. Exactly one ordering
  wins: either `Renew` extends the lease and `Complete` then succeeds
  with the post-renew fence, or `Complete` wins and `Renew` returns
  the not-claimed error.
- The final state is consistent. Exactly one `CompletedEvent` fires.
- The test passes under `go test -race`, in the default build and
  under `make verify-ledger-sqlite`.

### `client_concurrency_test.go`

Fixture:

- One `mcp.Client` connected over the SDK's in-memory transport, the
  pattern `mcp/client_test.go` already uses. The server side lists
  two tools and echoes call arguments.

Assertions:

- Sixteen goroutines mix `ListTools` and `CallTool` calls on the one
  client. Every call returns its own result. No argument or result
  crosses between calls.
- The test passes under `go test -race`, proving `mcp/client.go`'s
  concurrency claim for the two untested methods.

### `grpc_loopback_integration_test.go`

Fixture:

- A minimal `a2asrv.AgentExecutor` test double. It completes each
  task with a result message whose data part carries a signed
  envelope payload.
- An `a2asrv.NewHandler` over that executor, wrapped in
  `a2agrpc.NewHandler`, registered on a `grpc.Server`. The server
  listens on `127.0.0.1:0`.
- A client built through the public `a2aclient.New`, pointed at the
  listener's address. The file sits beside the existing internal test
  files but touches only exported API.

Assertions:

- `Send`, `Status`, and `Result` round trip over the real gRPC
  transport. The status poll reaches `StateCompleted`. The result
  payload matches what the executor signed.
- The client's post-hop signature re-verification accepts the real
  server's response.
- A concurrent sub-test: eight goroutines share one `Client` and each
  run the full Send, Status, Result sequence. The test passes under
  `go test -race`.
- No address outside loopback appears anywhere in the file.

## Verification

- `make verify` passes with the new `go test -race ./...` step
  included. The race detector now runs over the whole default build
  on every full verify.
- `make verify-fast` keeps its current steps and runtime. The
  pre-commit hook is unchanged.
- `make verify-ledger-sqlite` passes; the new ledger race test runs
  there too.
- `python3 scripts/check_plan.py`, `check_deps.py`, `check_prose.py`,
  `check_labels.py`, `check_docs.py`, and `check_names.py` all pass.
- The coverage floor holds for every package and for the total. The
  new files add coverage to `agent`, `channel`, `ledger`, `mcp`, and
  `a2aclient`; none lowers a floor.
- No `api/*.txt` file changes; `make api-update` is not run.
- `docs/plans/agents/PHASES.md` gains this phase in the phase order,
  marked plan-only until it passes plan review.
