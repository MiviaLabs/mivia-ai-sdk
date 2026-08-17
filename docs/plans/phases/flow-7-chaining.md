# Plan: flow phase 7 — chaining

Adds chaining to the flow package. A step runs a nested Definition as
one unit of work. Chaining composes workflows without merging them.

## Goal

A step nests another Definition and returns its status. The parent
reads the child result as one output. Deep chains run to completion.

## Scope

Inside: a chained step, the child result, and nested validation.
Outside: retries and persistence. Chaining stays in-process and
stdlib-only.

## API

- A chained step carries a nested Definition in a field.
- Run treats the nested Definition as one step. It runs the child and
  writes the child's final status to the step output.
- A chained step composes any number of levels deep.

Chaining is function composition. A step takes an input and returns an
output. A child Definition is another callable that returns a status
and an output.

## TDD

Write the single-child test first. Then the deep-chain test and the
child-failure test. Build chaining to satisfy each failing test.

## Tests

Unit and integration cases:

- a step that nests one child runs the child and returns its status.
- output passes from a child back to the parent step.
- a three-level chain runs to a final status.
- a child that stops on a guard propagates the error to the parent.
- a child with its own parallel panel runs correctly inside a step.
- integration: a parent workflow chains a child workflow that fans in
  parallel steps, then returns.

## Verification

`make verify` and `go test -race ./...`. Chaining integration tests run
end-to-end through Run.
