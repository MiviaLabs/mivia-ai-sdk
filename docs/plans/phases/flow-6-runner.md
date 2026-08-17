# Plan: flow phase 6 — runner and panels

Adds the runner to the flow package. Steps execute in topological
waves. Independent steps run in parallel goroutines. Integration tests
cover a real workflow.

## Goal

Run executes a Definition. Steps run in wave order. A wave of
independent steps runs in parallel and gathers results and errors.

## Scope

Inside: the Run method, parallel gathering, and step failures. Outside:
chaining, retries, and persistence. Run is in-process and stdlib-only.

## API

- `Run(ctx, d *Definition, m machine.Definition, in InOut) (Status, Out, error)`
- Sentinel errors for a step failure and a canceled context.
- Run executes one wave at a time. A wave with one step runs it. A wave
  with many steps runs them in goroutines.

Parallel gathering uses a WaitGroup, a buffered channel, and
errors.Join. Never use errgroup; it is not stdlib. If a guard on a step
stops the run, Run returns the step error.

## TDD

Write the singlet-step test first. Then the panel test and the failure
test. Build Run to satisfy each failing test in turn.

## Tests

Unit and integration cases:

- a linear chain of steps runs in order.
- a diamond DAG passes the right output between branches.
- a panel runs its steps in parallel and gathers all errors.
- a failing guard stops the run and returns that error.
- context cancellation stops pending panels.
- integration: a three-step workflow with a parallel fan-in runs over
  one Definition and returns the final status.

## Verification

`make verify` and `go test -race ./...`. The race detector proves the
panel goroutines share no unprotected state.
