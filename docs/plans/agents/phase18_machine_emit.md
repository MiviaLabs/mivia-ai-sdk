# Phase 18: machine move emits at the call site

Status: done. Builds on phase 17. This phase proves the machine-to-bus
wiring. The emit happens at the call site, not inside `machine.Fire`.
A caller owns an `events.Bus`. It runs a real `machine.Definition`
through `Fire`. It wires the returned move onto the bus. The machine
block plan lives in `docs/plans/machine.md`. The events block plan
lives in `docs/plans/events.md`. See
`docs/plans/agents/PHASES.md` for the contract.

## Goal

Prove the caller-side wiring pattern end to end. A caller builds a real
`machine.Definition`. It runs `Fire` and reads the move from the return
value. It emits that move onto a caller-owned `events.Bus`. The machine
block stays completely untouched.

## Scope

Inside: a tested integration wiring. It uses `machine.Fire` and the
`events.Bus` API. It is an external test package. It runs a real
definition, not a mock. Outside: any change to the `machine` block and
any `machine` imports `events` edge. The machine surface stays as is.

## API

No new exported symbol on `machine`. No new exported symbol on
`events`. `machine` imports only what it already imports. The surface
of `api/machine.txt` does not change. The emit belongs to the caller,
not to `Fire`. This is not a singleton design; it is a call-site
design. The change is correct at the composition layer.

`machine.Fire` resolves the row, runs the guard, then the exit action,
then the entry action. It returns `(Status, InOut, error)`. The caller
reads the target status and the record from the return value. The
caller emits only after a successful move. A guard failure returns an
error and emits nothing. A handler error on the bus does not affect the
move; the caller already has the move result.

The emitted event uses a small named constant in the test or example
package. The constant value is `"machine.move"`. It is not exported
API. It does not live in the `events` package.

## Tests

Test files live in `machine/machine_test/`:

- `phase18_integration_test.go` — wire a real `machine.Definition`
  through `Fire`. Assert the bus event arrives once. Feed a guard
  failure and assert no event arrives. Run under `go test -race`.
- A small example test or perf test, as the smallest useful case. It
  shows a caller subscribing and emitting in one flow.

Each test owns its bus. Each test uses a unique event name per case to
avoid cross-test re-entry. The `machine.move` constant lives in this
test package. There are no conformance vectors; the emit is in
process, not the wire form.

## Verification

`make verify` passes. The coverage floor for `machine` holds. No change
to `api/machine.txt`; the machine surface stays the same. No
`machine` imports `events` row in policy/layers.json. The flow wiring
stays out of this phase.
