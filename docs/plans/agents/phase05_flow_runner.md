# Phase 5: flow sequential runner

Status: future. Builds on phase 4. This phase adds the sequential
runner. It executes the graph in topological order. The ack confirms
each step before the next runs. See `docs/plans/agents/PHASES.md`.

## Goal

Run a step graph one step at a time. No step runs until the prior ack
confirms. The run returns the final status and the final record.

## Scope

Inside: `Run` for the sequential walk, the `Confirm` ack gate, and the
status result. Outside: the parallel panels, the error join, and the
chaining. Those belong to phases 6 and 7.

## API

- `type Confirm func(ctx context.Context, step Step) error`
- `Run(ctx, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm) (machine.Status, machine.InOut, error)`

`Run` walks the graph in topological order. Ready steps run in
declaration order. `Run` keeps the current status. Each `Fire` call
moves it. One record threads through the walk. Each step reads the
record and writes the next.

Each step picks its transition by target status. `Run` takes the rows
`m.AllowedTransitions(cur)` returns. It keeps the row whose `To`
equals `machine.Status(step.To)`. It fires that row's trigger. Zero
matches fail the run. Many matches fail the run. A guard rejection
stops the run. Every failure names the failing step ID.

`Run` fires a step, then calls `confirm`. The ack follows the envelope
ack flow. The caller owns the transport. It sends the envelope request
for the step and waits for every addressed recipient. A nil return
means full confirmation. `Run` rejects a nil `confirm`. A step without
a confirmed ack does not advance.

## Tests

Test files live in `flow/flow_test/`:

- `run_tdd_test.go` — the red-green cases for `Run`. Start with the
  assertions. Confirm they fail on the empty phase. Cover the order
  rule, the transition pick, the nil `confirm` rejection, and the
  ambiguity failure. Implement and watch them pass.
- `run_integration_test.go` — run a linear graph of three steps. Prove
  the order and the record threading. Run a diamond graph. Prove the
  declaration-order tie-break. Feed a gate failure and confirm the run
  stops. Prove an unconfirmed ack blocks the next step.
- `run_perf_test.go` — benchmark `Run` on the linear graph with a
  no-op `confirm`. Target under one millisecond for three steps. State
  the allocation budget. Record the measured baseline before the phase
  starts.

## Verification

`make verify` passes. The coverage floor for `flow` holds.
`policy/layers.json` gains the `flow` row: flow imports machine. The
envelope edge waits for phase 7. `api/flow.txt` gains `Run` and
`Confirm` via `make api-update`. `docs/architecture.md` and
`docs/packages/flow.md` update the flow sections in the same change.
Parallel work stays out of this phase.
