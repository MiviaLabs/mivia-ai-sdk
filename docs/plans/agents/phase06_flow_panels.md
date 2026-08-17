# Phase 6: flow parallel panels

Status: future. Builds on phase 5. This phase adds the parallel waves.
Panels run in goroutines. The runner gathers the results. See
`docs/plans/agents/PHASES.md` for the contract.

## Goal

Run a panel of independent steps at once. Each wave holds steps with
no remaining dependency. The waves run in sequence. The steps inside a
wave run in parallel. Errors combine without a third-party library.

## Scope

Inside: the wave scheduler, the goroutine pool, the buffered channel,
and `errors.Join`. Outside: the chaining and the nested workflow. Those
belong to phase 7.

## API

No new exported symbol. `Run` gains parallel behavior for a panel. The
wave is internal state. The return stays `(Status, machine.InOut, error)`.

A wave runs its steps in goroutines. Each step receives a copy of
the incoming record. A `WaitGroup` waits for the wave. A buffered
channel carries the results. `errors.Join` combines the failure
across the wave. A failed gate fails the whole run. No goroutine
mutates the shared record.

## Tests

Test files live in `flow/flow_test/`:

- `phase06_tdd_test.go` — the red-green cases for a parallel panel.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass.
- `phase06_integration_test.go` — run a panel of four independent
  steps. Prove they complete. Feed one failing step and confirm
  `errors.Join` reports it. Run under `go test -race`.
- `phase06_perf_test.go` — benchmark a ten-step panel. Prove the time
  is near the slowest step, not the sum. Record the ratio.

## Verification

`make verify` passes. The coverage floor for `flow` holds. Run
`go test -race ./...` for the goroutine pool. Parallelism does not
trade correctness.
