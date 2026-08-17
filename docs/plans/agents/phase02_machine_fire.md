# Phase 2: machine fire dispatch

Status: future. Builds on phase 1. This phase adds the `Fire` method
and the record it moves. See `docs/plans/agents/PHASES.md` for the contract.

## Goal

Move a record through a transition table. `Fire` resolves the row by
from status and trigger. It runs the guard, the exit action, and the
entry action. It returns the new status and the output record.

## Scope

Inside: the `InOut` record, the `Action` type, `Fire`, and the entry
and exit action order. Outside: the wire form and the validation
list. Those belong to phase 3.

## API

- `type InOut struct` holding the input and the output record.
- `type Action func(c Context) error` as an entry or exit action.
- `(*Definition).Fire(ctx, from Status, in InOut) (Status, Out, error)`

`Fire` returns an error on an unknown from status or trigger. A guard
that fails returns an error and moves nothing. The exit action runs
before the entry action. The output record carries the result.

## Tests

Test files live in `machine/machine_test/`:

- `phase02_tdd_test.go` — the red-green cases for `Fire`. Start with
  the assertions. Confirm they fail on the empty phase. Implement and
  watch them pass.
- `phase02_integration_test.go` — build a definition with a guard and
  an action. Fire it across two transitions. Prove the order of the
  exit and entry actions.
- `phase02_perf_test.go` — benchmark `Fire` on a ten-row table.
  Target under one microsecond. State the allocation budget.

## Verification

`make verify` passes. The coverage floor for `machine` holds. The
`Fire` signature lands in `api/machine.txt`. Nothing else changes.
