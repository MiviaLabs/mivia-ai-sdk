# Phase 11: discovery agent card

Status: future. Builds the discovery block. An agent card declares a
capability. A caller asks the card whether it can do a task. This
phase parses the card and matches a capability.

## Goal

Read a capability card and answer a match question. Parsing validates
the card shape. Matching returns the capability that fits a request.
The card is the entry point a caller uses to find a peer.

## Scope

Inside: the `Card` type, the parse, and the match. Outside: fetching
the card from a network, and the a2a integration. Those belong to the
agent phase. See `docs/research-agents.md` for the card decision.

## API

- `type Card struct` holding the name, the description, and the
  capability list.
- `Parse(data []byte) (Card, error)`
- `(*Card).Match(need string) (string, bool)`

`Parse` rejects an empty card and a blank name. `Match` compares a
need against the capability list. A partial word does not match. The
match is case-insensitive. The decision to reuse the A2A shape stays
open for plan review.

## Tests

Test files live in `discovery/discovery_test/`:

- `card_test.go` — the red-green cases for `Parse` and `Match`.
  Start with the assertions. Confirm they fail on the empty phase.
  Implement and watch them pass.
- `card_integration_test.go` — parse a real card, match a request
  to a capability, and reject a stranger. Prove a malformed card fails
  parse.
- `card_bench_test.go` — benchmark `Match` over a card of one hundred
  capabilities. Target under one microsecond. State the allocation
  budget.

## Verification

`make verify` passes. The coverage floor for `discovery` holds. The
card shape lands in `api/discovery.txt`. The shape decision follows
plan review, not this phase.
