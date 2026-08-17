# Plan: machine phase 1 — definition

Delivers the types and the definition for the machine package. No
firing yet. TDD from the first test.

## Goal

The types and the Definition type that describes a state machine. A
definition declares statuses, triggers, and transitions as data.

## Scope

Inside: the Status, Trigger, InOut, Action, Guard, and Transition
types; the Definition type; New; and Validate. Outside: the Fire
engine, the wire form, and chaining.

## API

- `type Status string`
- `type Trigger string`
- `type InOut struct` holding the input and the output record.
- `type Action func(Context) error`
- `type Guard func(Context) (bool, error)`
- `type Transition struct { From, To Status; Trigger Trigger; Guard Guard; OnEntry Action; OnExit Action }`
- `type Definition struct` holding the initial status and a list of
  transitions.
- `New(initial Status, ts ...Transition) (*Definition, error)`
- `(*Definition).Validate() error`

Actions and guards are code, not data. A definition keeps them as
fields. The wire form, later, stores only names and a registry rebinds
them.

## TDD

Write Validate tests first. Prove bad shapes fail. Then build New and
Validate. Then prove good shapes pass. Then write the type table test.

## Tests

Unit tables for definition shape:

- a transition with an empty from or to fails.
- a transition with an unmatched from fails.
- a duplicate transition from and trigger fails.
- an initial status that no transition allows fails.
- a valid definition passes Validate.

Versioned TDD checklist notes the order in docs/plans/phases/README.md.

## Verification

`make verify`. A plan-reviewer reads this phase before any code. The
coverage floor is 85 percent for the machine package.
