# Plan: flow

Status: the step graph ships. The sequential runner ships next. The
panels and the chaining stay future. This plan expands the earlier
step-list design into a step runner for v1. Rationale in
docs/research-state-machine.md. The build phases live in
docs/plans/agents/. See phases 4 through 7. Phase 4 owns the step graph
and the cycle check. Phase 5 owns the sequential `Run` and the
`Confirm` ack gate; see docs/plans/agents/phase05_flow_runner.md for
its exact API and error contract. Phases 6 and 7 own the panels and
the chaining.

## Goal

Run a declarative workflow over steps. A workflow is a step graph.
Steps hold dependencies, gates, inputs, outputs, and a target status.
The runner schedules steps in topological order and supports parallel
panels.

## Scope

Inside: a step graph, panels, parallel execution, chaining of
workflows, and a runner. A step composes the machine package for its
status transitions. A panel is a group of independent steps that run
together. A chained step runs a nested workflow as one step. The
runner detects cycles with Kahn's algorithm before any step runs. The
consumer is real; another system needs these capabilities now.

Outside: retries, compensation, scheduling, persistence, and history
replay. A future version adds these only when that consumer asks.
Parallel panels run in goroutines; the runner is in-process, not a
distributed service. Each wave reads the incoming record. Each step in
a wave runs with a copy of that record. The wave collects results
and errors. errors.Join reports failures across the wave. No goroutine
mutates the shared record. The design is correct, not hardened. It
meets the need without overengineering.

## API

Proposed shape, subject to plan review. It follows the DAG scheduler
and step-as-data patterns. See docs/research-state-machine.md for the
pattern sources.

- `type Step struct { ID string; Needs []string; To string; Payload string }`
  as a graph node.
- `type Panel []string` as a group of step IDs that run in parallel.
- `type Definition struct` holding the step graph and the panels.
- `New(steps []Step, panels []Panel) (*Definition, error)` to build a
  definition and reject cycles with Kahn's algorithm.
- `type Confirm func(ctx context.Context, step Step) error` as the ack
  gate a caller supplies. Phase 5 ships this shape.
- `Run(ctx, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm) (machine.Status, machine.InOut, error)`
  to execute the graph and return the final status. Phase 5 ships the
  sequential walk only: no panels, no chaining, no envelope import.
  Later phases may add a panel-aware entry point without breaking this
  signature.
- A chained step nests another Definition as one step. This lands in
  phase 7.

The machine instance passes by pointer. The input and output records
come from the machine package. Run may pass any in and out through the
graph. A panel of steps that run in parallel gather results and errors
without a third-party library.

Panels map to topological waves. A wave is a set of steps with no
remaining dependencies. The scheduler runs one wave at a time. Steps
inside a wave run in goroutines. It gathers results with a WaitGroup
and a buffered channel. It combines errors with errors.Join, which is
stdlib. It never uses errgroup.

Chaining is function composition. A step takes an input and returns an
output. A chained step runs a nested Definition and returns its
status. The parent reads the child result as one output.

The policy/layers.json row for flow grows in two steps. Phase 5 sets
`"flow": ["machine"]`. Phase 7 widens it to `["envelope", "machine"]`
when chaining needs the audit thread. The ack transport stays
caller-owned in every phase. The runner enforces the gate; the caller
provides the transport.

## Tests

Topological order on a diamond DAG. Cycle detection rejects a bad
graph. Phase 5 covers the sequential case: linear order, the
declaration-order tie-break, a gate failure, and an unconfirmed ack.
A panel of independent steps runs in parallel; this lands in phase 6.
Chaining runs a nested workflow and returns its status; this lands in
phase 7. The audit thread verifies with VerifyThread after the run,
once phase 7 lands.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in docs/research-state-machine.md. `api/flow.txt`
lands via make api-update.
