# Phase 44: provider token estimation

Status: future. Plan-only; it has not yet gone through plan review.
`provider` (see `docs/plans/provider.md`) and `contextbudget` (see
`docs/plans/contextbudget.md`) have both shipped. This phase extends
`provider` only; it does not require `contextbudget` to change, per
the decision below.

## Why this phase exists

A prior review confirmed a real gap: `provider.ContextAccountant`
(`provider/completer.go:19`) exposes only a model's context-window
ceiling, `ContextWindow() int`. No `Completer` capability answers how
many tokens a given `Request` would cost before a caller calls `Chat`
or `ChatStream`. `contextbudget.Limits`
(`contextbudget/contextbudget.go:8`) caps a call by byte count and
event count only; it carries no token-aware field. A caller who wants
to stay under a model's context window today has no SDK-native way to
estimate a `Request`'s token cost ahead of the call.

## The fix: one more optional capability interface

`provider` gains a second optional `Completer` capability, parallel
to `ContextAccountant`'s existing shape (`provider/completer.go:19`):
a small interface a concrete `Completer` implementation opts into, and
a caller reaches through a type assertion, never a required method on
`Completer` itself. This mirrors `ContextAccountant` and
`ReasoningPolicy`'s existing pattern exactly (`provider/completer.go:19`
and `:27`): a required two-method-plus-`Name` core, plus narrow,
independently-adoptable extensions.

`provider` ships no tokenizer. `Completer` already has zero
implementations in this SDK by design (`docs/plans/provider.md`'s
Scope section: "any concrete client" is explicitly outside `provider`).
A real token count needs a real tokenizer tied to a real model family;
that is caller-side, vendor-specific code, the same reason `Chat` and
`ChatStream` themselves ship with zero implementations. This phase
names the interface shape only.

## Decision: caller-side composition, no `contextbudget` change

`contextbudget.Limits` does not gain a token-aware field in this
phase. The composition between `EstimateTokens` and `ContextWindow`
stays entirely on the caller's side: a caller that has both a
`provider.TokenEstimator` and a `provider.ContextAccountant`
type-asserts each and compares `EstimateTokens(req) <
ContextWindow()` itself, with no new `contextbudget` type in between.

Reasoning for the smaller change:

- `contextbudget.Limits`'s own plan states its scope precisely: "a
  byte cap, an event or message count cap" (`docs/plans/contextbudget.md`'s
  Goal section), and separately states summarization, trimming, and
  any I/O-coupled decision stay outside it. A token cap is a third,
  separate dimension with its own unit and its own source (a
  `Completer`-specific estimate, not a byte count `agent.Run` already
  has on hand from a marshaled record). Folding it in would widen
  `Limits`'s scope past what its own plan commits to.
- `contextbudget.Limits.Fits` (`contextbudget/contextbudget.go:33`)
  takes plain `bytes, events int` the caller already has, with no
  dependency on `provider`. Adding a `tokens int` parameter, or a
  third cap field, would give `contextbudget` a reason to import
  `provider` for the `TokenEstimator` type it would need to call, or
  else force every `Fits` caller to pass a pre-computed token count
  regardless of whether it uses `provider` at all. Both outcomes cost
  more than the caller-side comparison this phase recommends instead.
- `policy/layers.json`'s `"contextbudget": []` row (an intentional
  leaf, `docs/plans/contextbudget.md`'s API section shows no internal
  import) would need to gain a `provider` edge for `Limits` to name a
  `provider.TokenEstimator` type in its own field, or `provider` would
  need to add a dependency toward `contextbudget`. Either edge
  contradicts `contextbudget`'s stated design as a storage- and
  provider-agnostic accounting check. The caller-side comparison
  avoids the edge entirely: neither package imports the other.
- A caller who wants both checks composed already writes that
  three-line comparison today with the existing `ContextAccountant`
  interface; adding `EstimateTokens` changes nothing about how that
  composition happens, only what the caller has to compare.

If a second real caller later needs `contextbudget` itself to enforce
a token cap without hand-writing the comparison, that need is the
trigger for a follow-up phase adding a `MaxTokens` field and an
`EstimateTokens`-shaped parameter to `Fits`, per the same
two-or-more-real-callers bar this repo already applies elsewhere
(see phase 43's channel decision above, and `docs/plans/channel.md`'s
own "three call sites" justification). This phase does not add that
field speculatively.

## Scope

Inside: `provider.TokenEstimator`, the new optional interface;
its doc comment describing the type-assertion usage pattern.

Outside: any concrete tokenizer or token-counting implementation.
Outside: any change to `contextbudget.Limits`, `Fits`, or `Validate`,
per the decision above. Outside: any change to `agent.Run`'s
signature; wiring a `TokenEstimator` check into `agent.Run` is a
separate, later phase's plan, the same way `docs/plans/provider.md`
itself declares wiring `provider` into `agent` out of scope for its
own phase. Outside: any change to `ContextAccountant` or
`ReasoningPolicy`; both stay unchanged.

`provider` gains no new import. `TokenEstimator`'s method signature
uses only `provider.Request`, already defined in the package, and
`int`, so no `policy/layers.json` change and no new stdlib import.

## API

The surface below lands in `api/provider.txt` via `make api-update`.

- `type TokenEstimator interface { EstimateTokens(Request) int }` —
  an optional `Completer` capability exposing a best-effort token
  count for a given `Request`, ahead of a `Chat` or `ChatStream`
  call. A caller type-asserts: `if te, ok :=
  c.(provider.TokenEstimator); ok { n := te.EstimateTokens(req) }`,
  the same pattern `ContextAccountant` and `ReasoningPolicy` already
  use. `EstimateTokens` takes the same `Request` value the caller
  intends to pass to `Chat`, so an implementation can account for
  every field: `Messages`, `Tools`, and any provider-specific
  overhead a real tokenizer adds for message framing. The estimate is
  best-effort and provider-defined; `provider` states no accuracy
  guarantee and computes no estimate itself.

No change to `Completer`, `ContextAccountant`, `ReasoningPolicy`,
`Request`, `Response`, `RunTurn`, or any existing sentinel error. No
constructor: like the other two optional capabilities, a caller's own
`Completer` implementation adds the method; `provider` builds nothing
concrete.

## Tests

Test files live in `provider/provider_test/`:

- `token_estimator_test.go` — a new red-green case file. A fake
  `Completer` (reusing `completer_test.go`'s existing fake shape) that
  additionally implements `TokenEstimator`, returning a fixed count
  from a table of `Request` inputs; the type assertion succeeds and
  `EstimateTokens` returns the configured count. A second fake that
  implements `ContextAccountant` and `ReasoningPolicy` but not
  `TokenEstimator`; the type assertion for `TokenEstimator` fails
  cleanly (`ok == false`), proving the three optional capabilities
  are independently adoptable, matching `completer_test.go`'s
  existing "second fake implements both, third implements neither"
  case shape for the other two capabilities.
- `token_estimator_integration_test.go` — a fake `Completer`
  implementing both `ContextAccountant` and `TokenEstimator`; the
  test performs the caller-side comparison this plan recommends,
  `EstimateTokens(req) < ContextWindow()`, for a `Request` under the
  window and a `Request` over it, asserting the boolean result each
  way. This proves the two interfaces compose in caller code with no
  `provider`-internal glue, matching the "no `contextbudget` change"
  decision above.

No new benchmark file: `EstimateTokens` is a single interface-method
call with no `provider`-owned computation; a benchmark would measure
the fake's own body, not anything `provider` contributes.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `provider` and for the total,
  with the new interface and its tests counted in.
- `api/provider.txt` gains `TokenEstimator` via `make api-update`,
  committed in the same change as the code.
- `policy/layers.json` is unchanged: `"provider": []` and
  `"contextbudget": []` both stay as they are; this phase adds no new
  edge between the two packages.
- `docs/packages/provider.md` gains a `TokenEstimator` entry next to
  `ContextAccountant` and `ReasoningPolicy`, stating the same
  type-assertion usage pattern.
- `docs/packages/contextbudget.md` is unchanged; this phase makes no
  claim requiring an update there.
- No conformance vector change: `provider` defines no wire format,
  and this phase adds no `Encode`/`Decode` pair.
