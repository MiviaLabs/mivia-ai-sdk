# Package reference: machine

The machine package is the state-machine building block. Phase 1 ships
the status model: typed statuses, triggers, transitions, and guards.
Phase 2 ships the move dispatch `Fire`, the input and output records,
and the entry and exit actions. The wire form lands in a later phase.
The exported surface below mirrors `api/machine.txt`.

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
- `Definition` — an initial status and the transition table.

## Functions and methods

- `New(initial, transitions...)` — builds a `Definition` and validates
  the table.
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
- `Fire` returns an error on an unknown `from` status or trigger.
- `Fire` runs the guard, then the exit action, then the entry action.
- A failed guard blocks the move and skips the exit action.
- `Fire` checks, never invokes, a nil `Guard` or a nil `Action`.
- `Fire` runs an action on the record it carries. An action writes the
  output record through the `InOut` it receives. `Fire` returns that
  record in the result `InOut`.

## Wire contract

- No JSON wire form exists yet. A later phase adds it. The machine
  stays data-driven and serializable by then.

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
