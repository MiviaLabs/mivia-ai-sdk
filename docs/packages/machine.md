# Package reference: machine

The machine package is the state-machine building block. Phase 1 ships
the status model: typed statuses, triggers, transitions, and guards. The
move dispatch `Fire`, the input and output records, the entry and exit
actions, and the wire form land in later phases. The exported surface
below mirrors `api/machine.txt`.

## Types

- `Status` — a typed machine state. Values are strings.
- `Trigger` — a typed label that selects a transition.
- `Guard` — a predicate over a context. It decides whether a move is
  allowed. The signature is `func(context.Context) (bool, error)`.
- `Transition` — one row in the table. Fields: `from`, `to`, `trigger`,
  and a `guard`.
- `Definition` — an initial status and the transition table.

## Functions and methods

- `New(initial, transitions...)` — builds a `Definition` and validates
  the table.
- `Definition.Validate()` — checks the transition table.

## Invariants

`New` and `Validate` enforce the rules below.

- `New` rejects an empty transition list.
- A transition never loops from a status to itself.
- Every `from` status is reachable from the initial status through the
  table. Reachability means the status equals the initial status or
  appears as a `to` in a reachable row.
- An unreachable `to` implies an unreachable `from`, so the `from`
  check covers both.
- `New` and `Validate` accept a nil `Guard`.

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
_ = d.Validate()
```
