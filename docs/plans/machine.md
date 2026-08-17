# Plan: machine

Status: shipped through phases 1 through 3. Rationale in
docs/research-state-machine.md. The build phases live in
docs/plans/agents/. See phases 1 through 3.

## Goal

Own status mechanics for one record. A definition lists statuses,
transitions, gates, and input and output bindings. A transition fires
only when its gate passes. Entry and exit actions run on the move.

## Scope

Inside: typed statuses, transitions, gates, inputs, outputs, entry
actions, and exit actions. A definition has a Validate method that
checks every listed transition. A transition moves one record from a
status to a status and passes an input and an output. The machine is
data-driven and serializable.

Outside: the step graph, panels, parallel execution, scheduling, and
chaining. The flow package owns those concerns. The machine never
schedules. It never knows a graph. It stays reusable on its own.

## API

Proposed shape, subject to plan review. It follows the action model
pattern. See docs/research-state-machine.md for the pattern sources.

- `type Status string` as the typed status enum base.
- `type Trigger string` as the label that selects a transition.
- `type InOut struct { Input any; Output any }` holding the input
  record and the output record. A bound function reads Input and
  writes Output. The caller type-asserts concrete payloads.
- `type Action func(ctx context.Context, rec *InOut) error` as an entry
  or exit action. The action writes the output record through `rec`.
- `type Guard func(ctx Context) (bool, error)` as a transition guard.
- `type Transition struct { From, To Status; Trigger Trigger; Guard Guard; OnEntry Action; OnExit Action }`
  as a table row. Phase 1 omits `OnEntry` and `OnExit`; the struct
  grows in Phase 2.
- `type Definition struct` with the unexported fields `initial Status`
  and `transitions []Transition`. Callers read them through `Initial`
  and `Transitions`.
- `(d Definition).Initial() Status` reads the initial status.
- `(d Definition).Transitions() []Transition` returns a copy of the
  transition table. The value receiver also serves non-addressable
  values, such as the `Decode` result in Phase 3.

A `Definition` is immutable after `New`. The unexported fields make
the invariant enforced, not caller-honored. `Transitions` returns a
copy, so a caller cannot mutate the internal table. The `Definition`
doc comment states the enforced invariant.

- `New(initial Status, ts ...Transition) (*Definition, error)` to
  build a definition and reject a bad shape.
- `(*Definition).Fire(ctx, from Status, trig Trigger, in InOut) (Status, InOut, error)`
  to move a record when the guard passes. The output record fills the
  returned InOut.
- `(*Definition).Validate() error` on the transitions. It stays
  exported. Phase 3 wire decode calls it. It still rejects an empty
  zero-value `Definition`.

Fire resolves the row by from and trigger. It runs the guard, then the
exit action, then the entry action. Dispatch is a scan over the
transition list, not reflection. A trigger that does not match returns
an error. OnExit does not run when the guard fails. A nil Guard or a
nil Action is checked, never invoked.

The machine row in policy/layers.json already declares no imports.
This change adds no edge. The flow package imports machine.

## Tests

Table-driven transition tests for each gate path. A gate that fails
blocks the move. Entry and exit actions run in order. Round-trip of a
definition through the wire form. A bad transition shape fails `New`.
Semgrep proves the machine uses no reflection.

Definition tests construct via `machine.New`. External code cannot
build a `Definition` directly. `TestNewRejects` covers the reject
cases. `TestNewAccepts` covers the accept cases. The two names replace
`TestValidateRejects` and `TestValidateAccepts`. `TestNew` folds into
the two tables. Its empty-list case lands in `TestNewRejects`. Its
valid-list case lands in `TestNewAccepts`. The reject cases cover the
empty list, a self loop, and an unreachable `From`. They cover a
duplicate `From` and `Trigger` pair. `TestNewAccepts` covers a nil
`Guard` and a valid table.

`TestValidateRejectsZeroValue` calls `Validate` on a zero-value
`Definition`. It asserts the "must not be empty" error. This path is
reachable only through a zero-value `Definition`. `New` returns early
on an empty list, so it never reaches `Validate`'s empty-list branch.
`TestDefinitionFields` reads the state through `Initial` and
`Transitions`. `TestNewCopiesInputSlice` writes one element of the
caller's slice after `New`. It sets `ts[0].To = "evil"`. Then `Fire`
still lands on the original target. Appending to the caller's slice
cannot change the internal table. `TestTransitionsReturnsCopy` writes
one element of the returned slice. It sets `copy[0].To = "evil"`. A
second `Transitions` read returns the original `To`. No test covered
`New`'s validation-error path before.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in docs/research-state-machine.md. `api/machine.txt`
lands via make api-update. The lock update drops the exported fields
and adds the two methods.
