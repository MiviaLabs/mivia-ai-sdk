# Phase 4: flow step graph

Status: future. Builds the flow block. The flow block plans live in
`docs/plans/flow.md`. This phase owns the step graph and the cycle
check. See `docs/plans/agents/PHASES.md` for the contract.

## Goal

Define a step graph with dependencies and panels. Build a `Definition`
from the steps. Reject a cycle with Kahn's algorithm before any step
runs. The graph is data, not code.

## Scope

Inside: `Step`, `Panel`, `Definition`, `New`, and the cycle detection.
Outside: the runner, the scheduling, the parallel waves, and the
chaining. Those belong to later flow phases.

## API

- `type Step struct { ID string; Needs []string; To string; Payload string }`
- `type Panel []string` as a group of step IDs that run together.
- `type Definition struct` holding the steps and the panels.
- `New(steps []Step, panels []Panel) (*Definition, error)`

`New` validates every step ID. It rejects a missing dependency. It
rejects a panel that names an unknown step. Kahn's algorithm rejects a
cycle. A step with no `Needs` is a root.

## Tests

Test files live in `flow/flow_test/`:

- `phase04_tdd_test.go` — the red-green cases for `New`. Start with
  the assertions. Confirm they fail on the empty phase. Implement and
  watch them pass.
- `phase04_integration_test.go` — build a diamond graph and a linear
  graph. Prove the roots are correct. Feed a cycle and confirm `New`
  rejects it.
- `phase04_perf_test.go` — benchmark `New` on a graph of one hundred
  steps. Target under one millisecond. State the allocation budget.

## Verification

`make verify` passes. The coverage floor for `flow` holds. The graph
shape lands in `api/flow.txt`. The runner stays out of this phase.
