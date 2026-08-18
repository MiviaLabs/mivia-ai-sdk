# Package reference: discovery

The discovery package answers whether an agent can do a task. It
holds a capability card: a name, an optional description, and a
capability list. The exported surface below mirrors
`api/discovery.txt`.

## Types

- `Card` — a parsed capability card. Fields: `Name`, `Description`
  (optional), `Capabilities` (a string slice).

## Functions and methods

- `Parse(data)` — unmarshals JSON into a `Card`, then calls
  `Validate`.
- `Card.Validate()` — the invariant check.
- `Card.Match(need)` — compares `need` against each capability entry.

## Invariants

`Validate` enforces the rules below.

- `Name` must not be blank after `strings.TrimSpace`.
- `Capabilities` must not be empty.
- Each capability entry is trimmed with `strings.TrimSpace` before the
  next two checks run.
- A capability entry that is blank after trim fails, including a
  whitespace-only entry.
- A duplicate capability entry fails. Two entries count as duplicates
  when `strings.EqualFold` matches them after trim. `Match` uses the
  same fold comparison, so a validated card never hides a second entry
  that `Match` would treat as equal.

## Failure modes

This package returns plain errors, not sentinels. A caller cannot
match them with `errors.Is`.

- `Parse` fails when `data` is not well-formed JSON. Pinned by
  `discovery_test/card_test.go`.
- `Card.Validate` fails when `Name` is blank after trim. Pinned by
  `discovery_test/card_test.go`.
- `Card.Validate` fails when `Capabilities` is empty or nil. Pinned by
  `discovery_test/card_test.go`.
- `Card.Validate` fails when a capability entry is blank after trim.
  Pinned by `discovery_test/card_test.go`.
- `Card.Validate` fails when a capability entry repeats another,
  fold-compared after trim. Pinned by `discovery_test/card_test.go`.

## Match semantics

- `Match` returns an empty string and `false` when `need` is blank.
- `Match` compares `need` against each capability entry with
  `strings.EqualFold`. It does not trim `need`. A `need` with leading
  or trailing space does not match an entry with no padding.
- `Match` returns the first matching entry in slice order and `true`.
- `Match` does not call `Validate`. On a card with a duplicate-case
  capability entry, it still returns the first slice-order match.

## Why this shape

`Card` is a minimal capability card, not the A2A Agent Card format.
[../architecture.md](../architecture.md)'s "Why the envelope is shaped
this way" section explains this decision, under what the wire
protocol deliberately leaves out.

## Wire contract

- `Parse` unmarshals JSON into a `Card`, then calls `Validate`. A
  decode error wraps the underlying JSON error. A validation failure
  returns the `Validate` error unchanged.
- `Parse` ignores unknown JSON fields, matching `envelope.Decode`'s
  forward-compatibility rule.
- `Capabilities` is an exported slice. `Parse` does not defensively
  copy it; the caller owns the backing array, the same convention
  `envelope.Message` uses for its exported slice fields.

## Usage

```go
data := []byte(`{
    "name": "invoice-agent",
    "capabilities": ["invoice.review", "invoice.approve"]
}`)
card, err := discovery.Parse(data)
if err != nil {
    // the JSON was malformed, or the card failed Validate
}
matched, ok := card.Match("Invoice.Review")
_ = matched // "invoice.review", the stored entry, not the query casing
_ = ok      // true
```

Cross-reference: [agent.md](agent.md) — `agent.New` validates a card
through `Card.Validate` before it binds an `Agent`.
