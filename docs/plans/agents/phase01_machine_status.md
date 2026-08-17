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
- `type Definition struct` with the unexported fields `initial Status`
  and `transitions []Transition`. Callers read them through `Initial`
  and `Transitions`.
- `(d Definition).Initial() Status` reads the initial status.
- `(d Definition).Transitions() []Transition` returns a copy of the
  transition table.
- `New(initial Status, ts ...Transition) (*Definition, error)`

The `Transition` struct omits `OnEntry` and `OnExit` in this phase.
Phase 2 adds them. The API lock updates with each phase.

`New` runs `Validate` on the table. An invalid table makes `New`
return `(nil, err)`. The fields are unexported, so external code cannot
construct an invalid `Definition`. `Validate` rejects a transition
whose `From` is not reachable from the initial status through the
table. Reachability means the status equals the initial status or
appears as a `To` in a reachable row. An unreachable `To` implies an
unreachable `From`, so the `From` check covers both. It rejects a self
loop where `From` equals `To`. It rejects two transitions with the
same `From` and `Trigger`. Dispatch must be unambiguous, so a
duplicate key is invalid. It accepts a nil `Guard`.

## Tests

Test files live in `machine/machine_test/`:

- `status_tdd_test.go` — the red-green cases for `New` and `Validate`.
  Start with the assertions. Confirm they fail on the empty phase.
  Then implement and watch them pass. Cases:
  - `New` rejects an empty transition list.
  - `New` accepts a valid transition list.
  - `New` rejects a self loop.
  - `New` rejects a `From` not reachable from the initial status.
  - `New` rejects duplicate `From` and `Trigger` pairs.
  - `New` accepts a nil Guard.
  - `New` accepts a valid table.
- No `status_integration_test.go`. The status model has no
  cross-package boundary. Phase 2 adds the first integration test.
- `status_perf_test.go` — benchmark `Validate` on a table of ten
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
