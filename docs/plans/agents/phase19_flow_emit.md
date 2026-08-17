# Phase 19: flow emits onto the events bus

Status: future. Builds on phase 17 and the flow runner. This phase wires
the flow block to the shared bus. The flow runner emits a step outcome
onto the bus. The flow block plan lives in `docs/plans/flow.md`. The
events block plan lives in `docs/plans/events.md`. See
`docs/plans/agents/PHASES.md` for the contract.

## Goal

Let the shared bus carry every flow step outcome. A caller that
subscribes to the bus sees each step complete, gate, or fail as one
event. The flow runner stays reusable; the bus is an output of the
run, not part of the graph logic.

## Scope

Inside: an emit from the runner when a step resolves. The import edge
`flow` imports `events` lands in `policy/layers.json`. Outside: the
envelope translation and any machine change. Those belong to phase 20
or already landed in 18.

## API

No new exported symbol on `flow`. The bus emit is a private dependency
of the runner. The `Run` signature stays
`(Status, machine.InOut, error)`.

The runner schedules the waves in topological order. When a step
completes, it emits a step outcome onto the bus. A failed gate emits
too. A handler error on the bus does not fail the run; the caller logs
it. The emit carries the step ID and its outcome in one `Event`.

## Tests

Test files live in `flow/flow_test/`:

- `phase19_tdd_test.go` — the red-green cases for the step emit. Start
  with the assertions. Confirm they fail on the empty implementation.
  Implement and watch them pass.
- `phase19_integration_test.go` — run a linear graph and prove an
  event per step. Feed a failed gate and prove a gate event arrives.
  Run under `go test -race`.
- `phase19_perf_test.go` — benchmark `Run` on a linear graph with the
  emit. State the allocation budget. The emit must not dominate the run.

## Verification

`make verify` passes. The coverage floor for `flow` holds. The
`flow` imports `events` row lands in `policy/layers.json`. No change to
`api/flow.txt`; the flow surface stays the same. The envelope
translation stays out of this phase.
