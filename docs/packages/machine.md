# Package reference: machine

The machine package is the state-machine building block. Phase 1 ships
the status model: typed statuses, triggers, transitions, and guards.
Phase 2 ships the move dispatch `Fire`, the input and output records,
and the entry and exit actions. Phase 3 ships the JSON wire form and
the name registry. The exported surface below mirrors `api/machine.txt`.

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
- `Registry` — the named guards and actions the wire form rebinds.
  Fields: `Actions` and `Guards`. The two name sets are separate
  namespaces. A registered name never maps to a nil function; `Decode`
  rejects such a name.

## Functions and methods

- `New(initial, transitions...)` — builds a `Definition` and validates
  the table.
- `Definition.Initial()` — returns the initial status.
- `Definition.Transitions()` — returns a copy of the transition table.
- `Definition.Validate()` — checks the transition table.
- `Definition.Fire(ctx, from, trigger, in)` — moves the record through
  the row and returns the target status and the output record.
- `NewRegistry()` — builds an empty `Registry`.
- `Definition.Encode(reg)` — serializes the definition to JSON. Each
  bound name must resolve in `reg`.
- `Decode(data, reg)` — parses JSON and rebinds each name through the
  `Registry`. It returns a `Definition`.

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

## Wire contract

- `Encode(reg)` serializes a definition to JSON. Guard and action names
  are pointers in the wire form. A nil pointer means the field is
  absent. `omitempty` skips absent fields.
- `Decode(data, reg)` parses JSON and rebinds each name through `reg`.
  A name that is missing from the registry returns an error. An empty
  name returns an error. A name that resolves to a nil function returns
  an error. Unknown fields are ignored.
- A function does not serialize. The wire form stores a name for each
  guard and action. `New` never records a name, so an anonymous
  function cannot encode. Only a name that `Decode` read back can
  encode.
- Conformance vectors live in `machine/testdata/vectors/`. The prefix
  `valid_` means the vector decodes. The prefix `invalid_decode_`
  means the vector fails `Decode`.

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
