# Plan: machine phase 2 — the fire engine

Adds the Fire method. A definition becomes a working state machine.
TDD drives every path.

## Goal

Fire moves a record between statuses. It resolves a transition by from
and trigger, runs the guard, and then runs the exit and entry actions.

## Scope

Inside: the Fire method and its sentinel errors. Outside: the wire
form, chaining, and any scheduling. Fire stays stateless and
dependency-free.

## API

- `Fire(ctx, from Status, in InOut) (Status, Out, error)`
- Sentinel errors for an unknown transition and a blocked guard.

Fire resolves the transition row by from and trigger. It runs the
guard, then the exit action, then the entry action. A nil guard means
the move is always allowed. A nil action means nothing runs.

## TDD

Write the happy-path test first. Then write the guard-block, the
unknown-trigger, and the action-order tests. Build Fire to satisfy
each failing test in turn.

## Tests

Unit tables for every resolve path:

- a known from and trigger moves to the target status.
- a missing transition returns the sentinel error.
- a guard that returns false blocks the move.
- a guard error returns the error, not a move.
- the exit action runs before the entry action.
- a nil guard and a nil action are no-ops.
- an entry action error stops the move and returns that error.

Integration follows in phase 3, after the wire form exists.

## Verification

`make verify`. `go test -race ./...` proves concurrent Fire calls on
read-only definitions stay safe.
