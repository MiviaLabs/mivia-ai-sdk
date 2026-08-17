# Plan: flow

Status: future. No code yet. This plan expands the earlier step-list
design into a step runner for v1. Rationale in
docs/research-state-machine.md. The build phases live in
docs/plans/phases/. See phases 5 through 8.

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
distributed service. The design is correct, not hardened. It meets the
need without overengineering.

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
- `Run(ctx, d *Definition, m machine.Definition, in InOut) (Status, Out, error)`
  to execute the graph and return the final status.
- A chained step nests another Definition as one step.

Panels map to topological waves. A wave is a set of steps with no
remaining dependencies. The scheduler runs one wave at a time. Steps
inside a wave run in goroutines. It gathers results with a WaitGroup
and a buffered channel. It combines errors with errors.Join, which is
stdlib. It never uses errgroup.

Chaining is function composition. A step takes an input and returns an
output. A chained step runs a nested Definition and returns its
status. The parent reads the child result as one output.

A new row in policy/layers.json: flow imports envelope and machine.
The import edge lands when the code lands.

## Tests

Topological order on a diamond DAG. Cycle detection rejects a bad
graph. A panel of independent steps runs in parallel. A step that
fails its gate stops the run. Chaining runs a nested workflow and
returns its status. The audit thread verifies with VerifyThread after
the run.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in docs/research-state-machine.md. `api/flow.txt`
lands via make api-update.
