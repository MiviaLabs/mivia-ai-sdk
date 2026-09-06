# Package reference: machine

The machine package is the state-machine building block. It owns the
status model and the move dispatch `Fire`. The exported surface below
mirrors `api/machine.txt`.

## Constants

- `MoveEvent` — the typed event name a caller emits after a successful
  `Fire`. It is an `events.Name` constant; a caller uses it on both the
  publish and subscribe side. It imports the events package for the
  name type.

## Types

- `Status` — a typed machine state. Values are strings.
- `Trigger` — a typed label that selects a transition.
- `Guard` — a predicate over a context. It decides whether a move is
  allowed. The signature is `func(context.Context) (bool, error)`.
- `Action` — an entry or exit side effect on a move. The signature is
  `func(context.Context, *InOut) error`. The action writes the output
  record through the `InOut` it receives.
- `Transition` — one row in the table. Fields: `from`, `to`, `trigger`,
  `guard`, `on exit`, and `on entry`.
- `InOut` — the record a transition moves. Fields: `input` and `output`.
- `Definition` — an initial status and the transition table. The
  fields are unexported. Callers read them through `Initial` and
  `Transitions`.
## Functions and methods

- `New(initial, transitions...)` — builds a `Definition` and validates
  the table.
- `Definition.Initial()` — returns the initial status.
- `Definition.Transitions()` — returns a copy of the transition table.
- `Definition.AllowedTransitions(from)` — returns every transition
  whose `From` matches `from`, as a fresh copy. Returns an empty
  slice when no transition matches.
- `Definition.AllowedTriggers(from)` — returns the triggers of the
  same rows `AllowedTransitions(from)` returns, in the same order.
  `Validate`'s from/trigger uniqueness rule keeps the result
  duplicate-free.
- `Definition.Validate()` — checks the transition table.
- `Definition.Fire(ctx, from, trigger, in)` — moves the record through
  the row and returns the target status and the output record.

## Invariants

`New`, `Validate`, and `Fire` enforce the rules below.

- `New` rejects an empty transition list.
- A transition never loops from a status to itself.
- Every `from` status is reachable from the initial status through the
  table. Reachability means the status equals the initial status or
  appears as a `to` in a reachable row.
- An unreachable `to` implies an unreachable `from`, so the `from`
  check covers both.
- `New` and `Validate` accept a nil `Guard`.
- No two transitions share the same `from` and `trigger` pair. A
  duplicate makes dispatch ambiguous, so `Validate` rejects it.
- `New` copies the input list. A `Definition` is immutable after `New`.
  The fields are unexported, so the invariant is enforced. `Transitions`
  returns a copy of the table.
- `Fire` returns an error on an unknown `from` status or trigger.
- `Fire` runs the guard, then the exit action, then the entry action.
- A failed guard blocks the move and skips the exit action.
- `Fire` checks, never invokes, a nil `Guard` or a nil `Action`.
- `Fire` runs an action on the record it carries. An action writes the
  output record through the `InOut` it receives. `Fire` returns that
  record in the result `InOut`.

## Failure modes

This package returns plain errors, not sentinels. A caller cannot
match them with `errors.Is`.

- `New` and `Definition.Validate` fail on an empty transition list, a
  self loop, a transition whose `From` is not reachable from
  `initial`, or a duplicate transition sharing a `From` and
  `Trigger`. Pinned by `machine_test/status_test.go`.
- `Definition.Fire` fails when no transition row matches the current
  status and trigger, or when the row's `Guard` rejects the move.
  Pinned by `machine_test/fire_test.go`.

## Usage

```go
d, err := machine.New(
    machine.Status("idle"),
    machine.Transition{
        From:    machine.Status("idle"),
        To:      machine.Status("running"),
        Trigger: machine.Trigger("start"),
    },
)
if err != nil {
    // the table is invalid
}
next, out, err := d.Fire(context.Background(), "idle", "start", machine.InOut{})
if err != nil {
    // the move was rejected
}
_ = next
_ = out
```
