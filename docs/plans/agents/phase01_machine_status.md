# Phase 1: machine status model

Status: future. Builds the machine block. See
`docs/plans/machine.md` for the boundary of the block. See
`docs/plans/agents/PHASES.md` for the test contract. This phase owns the
status model, the transitions, the guards, and the validation.

## Goal

Define typed statuses and the transitions between them. A transition
moves one record. A guard blocks a move when its predicate fails.
`Validate` checks the transition table.

## Scope

Inside: the `Status` type, the `Transition` table row, the `Trigger`,
the `Guard`, and the `Validate` method. Outside: the input and output
records, the entry and exit actions, the `Fire` dispatch, and the wire
form. Those belong to a later phase.

## API

The base types follow `docs/plans/machine.md`. No public entry point
exists yet. The phase lands the types and the table validation only.

- `type Status string`
- `type Trigger any`
- `type Guard func(ctx Context) (bool, error)`
- `type Transition struct { From, To Status; Trigger Trigger; Guard Guard }`
- `type Definition struct` holding the initial status and the list of
  transitions.
- `New(initial Status, ts ...Transition) (*Definition, error)`

`Validate` rejects an empty name, a transition from a status that is
not declared, and a self loop. It accepts a table with no guard.

## Tests

Test files live in `machine/machine_test/`:

- `phase01_tdd_test.go` — the red-green cases for `New` and `Validate`.
  Start with the assertions. Confirm they fail on the empty phase.
  Then implement and watch them pass.
- `phase01_integration_test.go` — build a small definition and run it
  through the exported types. Prove the table round-trips through the
  struct fields.
- `phase01_perf_test.go` — benchmark `Validate` on a table of ten
  transitions. Target under one microsecond. State the allocation
  budget with `AllocsPerRun`.

## Verification

`make verify` passes. The coverage floor for `machine` holds at or
above 85. `api/machine.txt` lands via `make api-update` with the base
types only. The wire form stays out of this phase.
