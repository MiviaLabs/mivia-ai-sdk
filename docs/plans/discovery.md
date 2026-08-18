# Plan: discovery

Status: shipped. This package has no internal imports. See
docs/packages/discovery.md's "Why this shape" section for the
capability-card decision.

## Goal

Answer whether an agent can do a task. A capability card declares an
agent's name and its capabilities. Parse reads the card. Match checks
one capability against a request. The card is the entry point a
caller uses to find a peer.

## Scope

Inside: the Card type, Parse, Validate, and Match. Card holds a name,
a description, and a capability list. Parse rejects malformed JSON,
including zero-length input. Validate rejects a blank name and an
empty capability list. Match compares a need against the capability
list. The comparison is case-insensitive and exact; a partial word
never matches.

Outside: fetching a card over a network, the a2a integration, and a
multi-card registry. Those belong to a later composition phase. This
package owns no transport and no client.

The package imports nothing of this module. It stays a leaf block,
like envelope and events. Stdlib only: encoding/json, errors, fmt,
strings. The policy row is `"discovery": []`.

## API

The surface below is the lock target. It lands in `api/discovery.txt`
via make api-update.

- `type Card struct { Name string; Description string; Capabilities []string }`
  holds the parsed card. JSON tags: `name`, `description` (omitempty),
  `capabilities`.
- `func Parse(data []byte) (Card, error)` unmarshals JSON into a Card,
  then calls Validate. A JSON decode error (syntax or type mismatch)
  wraps the decode error with context. An invariant failure returns
  the Validate error unchanged. Parse ignores an unknown JSON field,
  matching envelope.Decode's forward-compatibility rule.
- `func (c Card) Validate() error` checks the card invariants. It
  rejects a blank Name after TrimSpace. It rejects an empty
  Capabilities list. It applies TrimSpace to each capability entry
  before the next two checks. It rejects a capability entry that is
  blank after trim, including a whitespace-only entry. It rejects a
  duplicate entry, compared case-insensitive after trim.
- `func (c Card) Match(need string) (string, bool)` compares need
  against each capability with strings.EqualFold. It returns the
  matched capability and true on a hit. It returns an empty string and
  false when need is blank or no entry matches. Match does not trim
  need. A padded need, such as a leading space, does not match an
  entry with no padding.

Match never calls Validate. A Card built directly, bypassing Parse,
still works with Match: the method returns the first capability, in
slice order, that matches need. A duplicate or blank entry does not
error; it only takes part in the linear scan. This is defined
behavior, not undefined, because Go's range over a slice is ordered.

Card uses value receivers, not pointer receivers. Parse returns a
Card by value. This matches envelope.Decode, which returns a Message
by value. Card is a small value type once parsed: two strings and a
slice header. A value receiver lets a caller pass a Card by copy
without losing Match or Validate. The SDK already splits receivers
this way. envelope.Message and envelope.Ack use value receivers; they
are wire-decoded data. identity.Identity and events.Bus use pointer
receivers; they hold session state or a mutex. Card holds neither; it
is data, so it takes the Message convention. `Match` also stays a
value-receiver method for the same reason, overriding the
pointer-receiver shape the phase sketch proposed.

Card is a value type, not an immutable one. Capabilities is an
exported slice; a caller can mutate its backing array after Parse
returns. Parse does not defensively copy Capabilities. This matches
envelope.Message, whose exported slice fields (To, ContextRefs,
Provenance.Chain, Provenance.Evidence) carry the same caller-owned
mutability with no defensive copy. A copy-on-parse guarantee is a new
precedent; this plan does not set one.

Card does not reuse the A2A AgentCard shape. The a2a package is a
later, separate phase. Discovery must not import it or shape itself
around it. Card defines its own minimal shape: a name, a
description, and a flat capability list. A future a2a phase may map
an AgentCard into a Card, or the reverse. That mapping is out of
scope here.

The expected lock content:

```text
package discovery
  func (c Card) Match(need string) (string, bool)
  func (c Card) Validate() (error)
  func Parse(data []byte) (Card, error)
  type Card struct {
  Name string `json:"name"`
  Description string `json:"description,omitempty"`
  Capabilities []string `json:"capabilities"`
}
```

## Tests

Test files live in `discovery/discovery_test/`:

- `card_test.go` — the red-green cases for Parse, Validate, and
  Match. Assertions come first. The builder confirms they fail on the
  empty package, then implements the code to green. Table cases cover
  a valid card, a blank name, an empty capability list, a blank
  capability entry, a whitespace-only capability entry, a duplicate
  capability, malformed JSON (a syntax error), a type-mismatch JSON
  payload, and an unknown extra JSON field that Parse still accepts.
  Match cases cover an exact match, a case-insensitive match, a
  partial word that must not match, an unknown capability, a blank
  need string, and a padded need string that must not match an
  unpadded entry.
- `card_integration_test.go` — parse a real card fixture, match a
  request to a capability, and reject a stranger request. Prove a
  malformed card fixture fails Parse. Prove Validate rejects a Card
  built by struct literal, bypassing Parse. Prove Match on that same
  unvalidated, struct-literal Card, given a duplicate-case capability
  entry, returns the first slice-order match; Match never calls
  Validate.
- `card_bench_test.go` — benchmark Match over a card of one hundred
  capabilities. Target under one microsecond per call. AllocsPerRun
  states a budget of zero; EqualFold and a range loop over a string
  slice allocate nothing. The builder records the measured baseline
  in this file.

Card fixtures live in `discovery/discovery_test/testdata/`:

- `valid.json` — a card with a name, a description, and three
  capabilities. Parse accepts it.
- `blank_name.json` — a card with an empty name field. Parse rejects
  it.
- `empty_capabilities.json` — a card with an empty capabilities list.
  Parse rejects it.
- `whitespace_capability.json` — a card with a whitespace-only
  capability entry. Parse rejects it after TrimSpace.
- `duplicate_capability.json` — a card with two capabilities that
  differ only in case. Parse rejects it.
- `malformed.json` — truncated JSON. Parse rejects it with a syntax
  decode error.
- `type_mismatch.json` — a card whose capabilities field is a string,
  not a list. Parse rejects it with a type-mismatch decode error.
- `extra_field.json` — a valid card plus one unknown JSON field. Parse
  accepts it and ignores the field.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for discovery and for the total.
- The discovery row in policy/layers.json lists no internal imports.
  The row lands with this plan, before the code.
- `api/discovery.txt` lands through make api-update in the same
  change as the code. The lock matches the surface in the API
  section.
- The phase adds no conformance vectors. The card format is a local
  input format, not the envelope wire schema. A future a2a phase may
  add vectors for an AgentCard mapping.
- docs/architecture.md and docs/README.md gain the discovery plan
  reference in this change.
