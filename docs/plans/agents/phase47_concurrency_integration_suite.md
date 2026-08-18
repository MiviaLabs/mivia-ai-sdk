# Phase 47: concurrency integration suite

Status: planned. Codebase-verified against the shipped tree. It
depends on phase 46. It adds no exported symbol and no new
`policy/layers.json` row.

## Why this phase exists

A gap audit compared phase 46 against every concurrency test in the
repo. Phase 46 proves the shipped surface composes in sequential
scenarios. Its race-detector run is a manual step, not a gate.

The audit confirmed five open gaps. A sixth gap from an earlier draft
is now partly closed and is restated below.

- Gap one: no gate runs the race detector. `make verify` and
  `make verify-fast` run plain `go test ./...`. Only
  `make verify-ledger-sqlite` passes `-race`.
- Gap two: no test runs concurrent `agent.Run` calls that share the
  SDK's stateful blocks.
  `agent/agent_test/liveness_integration_test.go` races two runs
  against one `heartbeat.Monitor` only. Nothing races runs against a
  shared `memory.Store`, `tools.Registry`, `ledger.Ledger`, or
  `events.Bus`.
- Gap three: `docs/plans/channel.md` records an open gap. No test
  wires a `channel.Notifier` into `tools.ScopeOptions.Approve`. The
  shipped `NewNDJSONNotifier` never drives a tool approval.
- Gap four: `ledger`'s race matrix covers Claim, Admit, Takeover,
  Renew against Renew, and Complete against Complete. No test races
  `Renew` against `Complete` on one key.
- Gap five: `mcp/client.go` claims concurrent `ListTools`, `CallTool`,
  and `CallToolWithProgress` are safe. Only the progress-token case
  has a concurrency test.
- Gap six is narrower than an earlier draft claimed.
  `a2aclient/client_concurrency_test.go` already races `Send`,
  `Status`, and `Result` over the stub transport. The open part is the
  real gRPC transport: the dial, the wire round trip, and the post-hop
  signature check stay unproven.

## Verified facts that shape this plan

- `go test -race -count=1 ./...` takes 7.4 seconds on this tree. That
  cost is small enough to gate on every `make verify`.
- `events.Bus`, `tools.Registry`, and `memory.Store` each guard their
  state with a mutex. `ledger.MemStore` uses compare-and-swap. No
  planned test is expected to expose a real defect.
- `NewNDJSONNotifier` is single-flight. A concurrent second call
  returns `ErrNotifierBusy`. The approval test therefore drives it
  sequentially, not from many goroutines.
- `mcp`'s own tests use `mcpsdk.NewInMemoryTransports()` and live in
  package `mcp`, not a `mcp_test` subdirectory. The new file follows
  that placement.
- The a2a-go server API exists at the pinned v0.3.15:
  `a2asrv.NewHandler(executor, opts...)` returns a
  `a2asrv.RequestHandler`, and `a2agrpc.NewHandler(h).RegisterWith(s)`
  registers it on a `grpc.Server`. Gap six is therefore feasible.

## Relation to phase 45 and phase 46

Neither plan changes. Phase 45 ships a doc walkthrough. Phase 46 ships
the sequential scenarios. This phase adds the concurrent scenarios and
turns phase 46's manual race run into a gate.

Where phase 46 substitutes a test-local closure for the channel
transport, this phase wires the shipped transport. Where phase 46
excludes a live gRPC server, this phase stands one up on a loopback
listener. `mcp/http_transport_test.go` sets the loopback precedent.

## Goal

Prove, under an enforced race detector, that concurrent `agent.Run`
calls stay correct while sharing the SDK's stateful blocks. Close the
five open gaps. Change no production Go code.

## Scope

Inside:

- One new step in `make verify`: `go test -race ./...`. It runs after
  `verify-fast` and before the coverage block. `verify-fast` stays
  unchanged, so the pre-commit hook keeps its runtime.
- `agent/agent_test/concurrent_composition_integration_test.go`. It
  races concurrent `agent.Run` calls against shared blocks.
- `channel/channel_test/notifier_approval_integration_test.go`. It
  wires the shipped `NewNDJSONNotifier` into
  `tools.ScopeOptions.Approve`.
- `ledger/ledger_test/renew_complete_race_test.go`. It races `Renew`
  against `Complete` on one key.
- `mcp/client_concurrency_test.go`. It races `ListTools` and
  `CallTool` over the in-memory transport.
- `a2aclient/grpc_loopback_integration_test.go`. It stands up an
  a2a-go server on a loopback listener and runs the full round trip.
- One line in `docs/plans/channel.md` naming the new approval test.

Outside:

- Any change to phase 45's or phase 46's plan, code, or tests.
- Any exported symbol. No `api/*.txt` lock changes.
- Any production Go change. A new test that exposes a real defect
  stops this phase. The defect escalates to the user.
- Any `policy/layers.json` change. `scripts/check_deps.py` scans only
  non-`_test.go` files.
- `durablefence`. `ledger`'s scenario tests already cover it.
- Separate concurrency tests for `envelope`, `identity`, `machine`, or
  `contextbudget`. The shared suite signs and verifies through real
  identities in every goroutine. Their contracts claim no shared
  mutable state.
- The pinned scope limits in `run_panel_integration_test.go` and
  `run_budget_test.go`. A panel wave skips the budget check and the
  heartbeat beat by design.
- `scheduler.Run`'s documented second-`Run` hazard. The contract
  already owns that caller error.
- External network access. The a2aclient server binds loopback only.

## API

No exported Go symbol is added, changed, or removed. Every `api/*.txt`
lock stays byte-identical. The Makefile change adds no Go API surface.

## Tests

### `concurrent_composition_integration_test.go`

Budget: 300 lines.

Fixture:

- Eight goroutines, each driving one `agent.Run`. Each run owns its
  identity, card, thread id, and a two-step `flow.Definition`.
- All eight share one `memory.Store`, one `tools.Registry`, one
  `ledger.Ledger` over `ledger.NewMemStore()`, one `events.Bus`, and
  one `heartbeat.Monitor`.
- The shared registry holds one write-class tool behind a
  `tools.Scope` whose `Approve` closure counts calls with an atomic
  counter and approves.
- Each run's `AckWait` calls `RunScoped`, then puts the result into
  the shared store, then returns a confirmed ack.
- Each goroutine calls `Admit` with its own key, then `Claim`, then
  `agent.Run`, then `Complete` with the run's status.
- A contention sub-test: two goroutines call `Admit` with one shared
  key. Exactly one wins.
- A wrapper sub-test: one `scheduler.Job` and one `trigger.Action`
  both wrap `agent.Run`. The test fires both concurrently.
- A counting `events.Handler` with atomic counters records each kind.
- The test uses `context.WithTimeout` as a watchdog, so a wedged
  goroutine fails the test instead of hanging it.

Assertions:

- All eight runs reach the expected terminal `machine.Status`.
- Every ledger key reports `StatusCompleted`. The contention sub-test
  records exactly one successful `Admit` for the shared key.
- The `Approve` counter and the tool's run counter each equal the
  gated-step count.
- Every stored ref reads back with the exact bytes written.
- The per-kind event counts equal the expected per-run counts.
- `heartbeat.Monitor.Dead` is empty after every run returns.
- The file passes under `go test -race`.

### `notifier_approval_integration_test.go`

Budget: 180 lines.

Fixture:

- A real `NewNDJSONNotifier` over two `io.Pipe` pairs. A peer
  goroutine reads each `Question` line and writes an `Answer` line.
- A second closure adapts the notifier to `ScopeOptions.Approve`'s
  `(bool, error)` signature. It builds the `Question` from the
  `tools.ToolCall` and returns `Answer.Approved`.
- A `tools.Registry` with one write-class tool behind that scope.

Calls run sequentially. `NewNDJSONNotifier` is single-flight and
returns `ErrNotifierBusy` to a concurrent second caller.

Assertions:

- An approving peer answer runs the tool exactly once.
- A declining peer answer returns `tools.ErrToolDeclined`. The tool's
  run counter does not change for that call.
- Both answers crossed the real NDJSON wire, proven by the peer's
  read count.
- `docs/plans/channel.md`'s gap note names this test on shipping.

### `renew_complete_race_test.go`

Budget: 140 lines.

Fixture: one admitted and claimed key on a `ledger.Ledger` over
`ledger.NewMemStore()`. The file reuses `barrierStore` from
`ledger/ledger_test/renew_race_test.go`, which is already in the same
test package.

Assertions:

- Two goroutines race `Renew` against `Complete`. Exactly one ordering
  wins. Either `Renew` extends the lease and `Complete` then succeeds,
  or `Complete` wins and `Renew` returns `ledger.ErrNotClaimed`.
- The final state is consistent. Exactly one `CompletedEvent` fires.
- The test passes under `go test -race` in the default build and under
  `make verify-ledger-sqlite`.

### `client_concurrency_test.go`

Budget: 160 lines. It lives in package `mcp`, matching
`mcp/client_test.go`.

Fixture: one `mcp.Client` over `mcpsdk.NewInMemoryTransports()`. The
server side lists two tools and echoes each call's arguments.

Assertions:

- Sixteen goroutines mix `ListTools` and `CallTool` calls on one
  client. Every call returns its own result.
- No argument or result crosses between calls, proven by echoing a
  per-goroutine token.
- The test passes under `go test -race`.

### `grpc_loopback_integration_test.go`

Budget: 260 lines.

Fixture:

- A minimal `a2asrv.AgentExecutor` test double. Its `Execute` writes a
  completed task whose result carries a signed envelope payload. Its
  `Cancel` writes a cancellation event.
- `a2asrv.NewHandler` over that executor, wrapped by
  `a2agrpc.NewHandler`, registered through `RegisterWith` on a
  `grpc.Server`. The server listens on `127.0.0.1:0`.
- A client built through the exported `a2aclient.New`, pointed at the
  listener's address.

Assertions:

- `Send`, `Status`, and `Result` round trip over the real transport.
  The status poll reaches `StateCompleted`.
- The result payload matches what the executor signed.
- The client's post-hop signature check accepts the real response.
- A concurrent sub-test runs eight goroutines through the full
  sequence on one shared `Client`.
- No address outside loopback appears in the file.

## Verification

- `make verify` passes with the new `go test -race ./...` step. The
  measured cost of that step is 7.4 seconds on this tree.
- `make verify-fast` keeps its current steps and runtime. The
  pre-commit hook is unchanged.
- `make verify-ledger-sqlite` passes. The new ledger race test runs
  there too.
- `python3 scripts/check_plan.py`, `check_deps.py`, `check_prose.py`,
  `check_labels.py`, `check_docs.py`, and `check_names.py` pass.
- The coverage floor holds for every package and for the total. The
  new files add coverage to `agent`, `channel`, `ledger`, `mcp`, and
  `a2aclient`.
- No `api/*.txt` file changes. `make api-update` is not run.
