# Plan: machine

Status: future. No code yet. This plan sets the boundary before any
builder starts. Rationale in docs/research-state-machine.md. The build
phases live in docs/plans/agents/. See phases 1 through 3.

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
- `type Action func(Context) error` as an entry or exit action.
- `type Guard func(ctx Context) (bool, error)` as a transition guard.
- `type Transition struct { From, To Status; Trigger Trigger; Guard Guard; OnEntry Action; OnExit Action }`
  as a table row.
- `type Definition struct` holding the initial status and a list of
  transitions.
- `New(initial Status, ts ...Transition) (*Definition, error)` to
  build a definition and reject a bad shape.
- `(*Definition).Fire(ctx, from Status, trig Trigger, in InOut) (Status, InOut, error)`
  to move a record when the guard passes. The output record fills the
  returned InOut.
- `(*Definition).Validate() error` on the transitions.

Fire resolves the row by from and trigger. It runs the guard, then the
exit action, then the entry action. Dispatch is a map lookup, not
reflection. A trigger that does not match returns an error. OnExit does
not run when the guard fails. A nil Guard or a nil Action is checked,
never invoked.

A new row in policy/layers.json: machine imports nothing. The flow
package imports machine.

## Tests

Table-driven transition tests for each gate path. A gate that fails
blocks the move. Entry and exit actions run in order. Round-trip of a
definition through the wire form. A bad transition shape fails
Validate. Semgrep proves the machine uses no reflection.

## Verification

`make verify`. Conformance vectors for the definition form. The
rationale lives in docs/research-state-machine.md. `api/machine.txt`
lands via make api-update.
