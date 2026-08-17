# Phase 5: flow sequential runner

Status: future. Builds on phase 4. This phase adds the sequential
runner. It executes the graph in topological order. The ack confirms
each step before the next runs. See `docs/plans/agents/PHASES.md`.

## Goal

Run a step graph one root at a time. Each step becomes an envelope
request. No step runs until the prior ack confirms. The run returns
the final status.

## Scope

Inside: `Run` for a single wave, the ack gate, and the status result.
Outside: the parallel panels, the error join, and the chaining. Those
belong to phases 6 and 7.

## API

- `Run(ctx, d *Definition, m *machine.Definition, in machine.InOut) (Status, machine.InOut, error)`

`Run` walks the roots in order. It fires each step through the machine
definition. A step with a failed gate stops the run. A step without a
confirmed ack does not advance. The output of a step feeds the next.
The machine instance passes by pointer. The records come from the
machine package.

Ack confirmation uses the envelope ack flow. A request to a room waits
for every addressed recipient to confirm. The caller owns the ack
transport. The runner enforces the gate.

## Tests

Test files live in `flow/flow_test/`:

- `phase05_tdd_test.go` — the red-green cases for `Run`. Start with
  the assertions. Confirm they fail on the empty phase. Implement and
  watch them pass.
- `phase05_integration_test.go` — run a linear graph of three steps.
  Prove the order. Feed a gate failure and confirm the run stops. Prove
  an unconfirmed ack blocks the next step.
- `phase05_perf_test.go` — benchmark `Run` on the linear graph.
  Target under one millisecond for three steps.

## Verification

`make verify` passes. The coverage floor for `flow` holds. The runner
signature lands in `api/flow.txt`. Parallel work stays out of this
phase.
