# Phase 19: flow emits onto the events bus

Status: shipped. Builds on phase 17 and the flow runner. This phase
wires the flow block to the shared bus. The flow runner emits a step
outcome onto the bus. The flow block plan lives in `docs/plans/flow.md`.
The events block plan lives in `docs/plans/events.md`. See
`docs/plans/agents/PHASES.md` for the contract.

## Goal

Let the shared bus carry every flow step outcome. A caller that
subscribes to the bus sees each step complete as one event. The flow
runner stays reusable; the bus is an optional output of the run, not
part of the graph logic.

## Scope

Inside: an emit from the runner when a step resolves. The import edge
`flow` imports `events` lands in `policy/layers.json`. A new exported
constant `StepCompletedEvent` names the event kind. The `Run` signature
gains a `bus *events.Bus` parameter. Outside: the envelope translation
and any machine change. Those belong to phase 20 or already landed in
18.

## API

`StepCompletedEvent` is an exported constant of type `events.Name`.
The `Run` signature gains `bus *events.Bus` as the last parameter.
The return type stays `(Status, machine.InOut, error)`. The bus is
optional; a nil bus suppresses all emits without error.

The runner schedules the waves in topological order. When a step
completes, it emits a step outcome onto the bus. Emit is best-effort:
the runner discards the Emit error so a missing subscriber never
fails the run. The emit carries the step ID in the event data.

## Tests

Test files live in `flow/flow_test/`:

- `phase19_tdd_test.go` — subscribe to StepCompletedEvent, run a
  linear graph with a non-nil bus, prove events arrive in topological
  order. Prove a nil bus does not panic. Prove a panel wave emits per
  member. Prove a chained step emits for the parent only. Prove a
  failed guard emits nothing.

## Verification

`make verify` passes. The coverage floor for `flow` holds. The
`flow` imports `events` row lands in `policy/layers.json`. The
`api/flow.txt` lock updates to reflect `StepCompletedEvent` and the
new `Run` parameter. The envelope translation stays out of this phase.
