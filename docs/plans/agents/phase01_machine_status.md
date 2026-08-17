# Phase 1: machine status model

Status: done. Builds the machine block. See
`docs/plans/machine.md` for the boundary of the block. See
`docs/plans/agents/PHASES.md` for the test contract. This phase owns the
status types, the transition table, and the validation.

## Goal

Define typed statuses and the transition table. `Validate` rejects
invalid table shapes. The phase lands the types and the validation
only. `Fire` and the wire form belong to a later phase.

## Scope

Inside: the `Status` type, the `Transition` table row, the `Trigger`,
the `Guard`, and the `Validate` method. Outside: the input and output
records, the entry and exit actions, the `Fire` dispatch, and the wire
form. Those belong to a later phase.

## API

The base types follow `docs/plans/machine.md`. No public entry point
exists yet. The phase lands the types and the table validation only.

- `type Status string`
- `type Trigger string`
- `type Guard func(ctx context.Context) (bool, error)`
- `type Transition struct { From, To Status; Trigger Trigger; Guard Guard }`
- `type Definition struct` holding the initial status and the list of
  transitions.
- `New(initial Status, ts ...Transition) (*Definition, error)`

The `Transition` struct omits `OnEntry` and `OnExit` in this phase.
Phase 2 adds them. The API lock updates with each phase.

`Validate` rejects a transition whose `From` is not reachable from
the initial status through the table. Reachability means the status
equals the initial status or appears as a `To` in a reachable row.
An unreachable `To` implies an unreachable `From`, so the `From`
check covers both. It rejects a self loop where `From` equals `To`.
It accepts a nil `Guard`.

## Tests

Test files live in `machine/machine_test/`:

- `phase01_tdd_test.go` — the red-green cases for `New` and `Validate`.
  Start with the assertions. Confirm they fail on the empty phase.
  Then implement and watch them pass. Cases:
  - `New` rejects an empty transition list.
  - `New` accepts a valid transition list.
- `Validate` rejects a self loop.
- `Validate` rejects a `From` not reachable from the initial status.
- `Validate` accepts a nil Guard.
- `Validate` accepts a valid table.
- `phase01_integration_test.go` — merged into the TDD file.
  Phase 1 imports no other package. No cross-boundary path exists.
  See PHASES.md: "A phase may merge files only when a test kind has
  no case yet."
- `phase01_perf_test.go` — benchmark `Validate` on a table of ten
  transitions. The builder runs the benchmark against the empty
  implementation first and records the baseline in a comment. Target
  under one microsecond. State the allocation budget with
  `AllocsPerRun`.

## Verification

`make verify` passes. The coverage floor for `machine` holds at or
above 85. `api/machine.txt` lands via `make api-update` with the base
types only. The wire form stays out of this phase. Add `machine: []`
to `policy/layers.json`. The deps gate confirms machine imports
nothing.
