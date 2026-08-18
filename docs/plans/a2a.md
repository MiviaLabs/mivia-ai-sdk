# Plan: a2a

Status: shipped. The `a2aclient` package (`docs/plans/a2aclient.md`)
builds on this package's mapping. Agent Card discovery through A2A
stays future and out of scope until its own review.

## Goal

Map an `envelope.Message` to an A2A v1.0 message part and back. The
round trip keeps the envelope's fields intact. This phase carries no
network call and no third-party import.

## Scope

Inside this package:

- The `Part` type: a package-local struct that mirrors the A2A v1.0
  wire shape. A2A v1.0 dropped the old `kind` field and the separate
  part classes; one part carries text, raw bytes, a url, or a data
  object.
- `ToPart` and `FromPart`: the mapping functions between
  `envelope.Message` and `Part`.
- Conformance vectors for the mapped form.
- Zero imports outside the standard library and `envelope`.

Outside this package, owned by `a2aclient` instead:

- The `a2a-go` client import (`a2aproject/a2a-go`) and the
  stdlib-only exception that import needs.
- Sending a message to a remote agent, task polling, and the real A2A
  `Message` and `Task` wrapper types.
- Any network call.

Deferred to a future phase:

- Discovery through A2A Agent Cards.

This package is a client of nothing. It has no `Send`, no `Status`, no
`Result`, and no `AgentCard`. Those symbols belong to `a2aclient` and
a future discovery phase.

## API

```go
package a2a

// Part is one A2A v1.0 message part. A2A v1.0 has no kind field and
// no separate part classes: one part carries text, raw bytes, a url,
// or a data object. Part carries no message-level field. ContextID
// and MessageID belong to the wrapping A2A Message, not to Part, so
// they live on Mapped, alongside Part, not on Part itself.
type Part struct {
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	Raw  []byte          `json:"raw,omitempty"`
	URL  string          `json:"url,omitempty"`
}

// Mapped is the result of ToPart and the input to FromPart. It holds
// Part plus the two A2A Message-level fields Part cannot carry:
// ContextID (the envelope's ThreadID) and MessageID (the envelope's
// ID).
type Mapped struct {
	Part      Part
	ContextID string
	MessageID string
}

// ToPart maps a signed or unsigned envelope.Message onto a Mapped
// value. It calls m.Validate first and returns an error, not a zero
// Mapped, on failure. It signs nothing and does not modify m. ToPart
// builds Part.Data by calling m.Encode and wrapping the result in
// json.RawMessage, reusing envelope's wire encoder instead of a
// second marshal call; Text, Raw, and URL stay empty. Mapped.ContextID
// carries m.ThreadID and Mapped.MessageID carries m.ID.
func ToPart(m envelope.Message) (Mapped, error)

// FromPart maps a Mapped value back to an envelope.Message. It
// unmarshals mapped.Part.Data into an envelope.Message, then
// overwrites ThreadID with mapped.ContextID and ID with
// mapped.MessageID before calling Validate. FromPart returns an
// error instead of an invalid Message: no malformed part crosses the
// a2a boundary silently.
func FromPart(mapped Mapped) (envelope.Message, error)
```

Design notes:

- An earlier sketch pinned a two-value signature: `ToPart(m) (a2a.Part,
  error)` and `FromPart(p) (envelope.Message, error)`. This plan
  supersedes that signature: `Part` cannot carry message-level fields
  (`ContextID`, `MessageID`), so the fix moved those fields off `Part`
  and onto a separate return value. The semantic mapping contract
  still holds under the new shape: `contextId` maps to `thread_id`,
  and `messageId` maps to `id`.
- `Part` carries only part-level content: `Text`, `Data`, `Raw`,
  `URL`. `ContextID` and `MessageID` are A2A Message-level fields in
  the real wire shape, so they never sit on `Part`; they live on
  `Mapped` instead. `a2aclient` wires `Mapped.ContextID` and
  `Mapped.MessageID` into `a2aproject/a2a-go`'s `Message` type without
  touching `Part`'s shape.
- `ToPart` performs no signing and no mutation. The caller signs
  `m` with `envelope.Sign` or `identity.Sign` before calling `ToPart`,
  the same way `envelope.Encode` expects a caller-prepared message.
- `ToPart` builds `Part.Data` through `m.Encode()`, not a direct
  `json.Marshal` call. `semgrep/sdk-standards.yml`'s
  `sdk.go.marshal-via-encode` rule forbids `json.Marshal` outside
  `envelope`'s own `message.go`, `sign.go`, `ack.go`, `wire.go`, and
  test files. `a2a` is not on that exemption list, so `ToPart` reuses
  `envelope.Message.Encode` and wraps the returned bytes in
  `json.RawMessage`. `FromPart`'s `json.Unmarshal(mapped.Part.Data,
  &m)` is not a marshal-side call, so the rule does not apply there.
- `FromPart` mirrors `envelope.Decode`'s parse-then-validate order
  (`docs/architecture.md`, Message flow, step 4): unmarshal
  `mapped.Part.Data` into `envelope.Message`, overwrite `ThreadID`
  with `mapped.ContextID` and `ID` with `mapped.MessageID`, then call
  `Validate`. The `Mapped.ContextID`/`Mapped.MessageID` fields win
  over any `thread_id`/`id` already present in `Data`. A parse success
  with a failed `Validate` still returns an error, never a partial
  `Message`. A malformed `Data` object that fails to unmarshal into
  `envelope.Message` also returns an error, before `Validate` ever
  runs.
- `ToPart` returns `Mapped`, not a four-value tuple. No function in
  this codebase (`envelope`, `identity`, `room`, `machine`, `flow`)
  returns more than two values, and two adjacent string returns
  (`contextID`, `messageID`) invite an accidental swap at the call
  site. `Mapped` is exported because `a2aclient` callers need `Part`,
  `ContextID`, and `MessageID` together.
- `Part` carries no `Metadata` field. No caller reads or writes A2A
  part metadata; a future phase adds it only when a concrete caller
  needs it.
- No string literal replaces an existing envelope constant; `Part`
  introduces no new enum.

`policy/layers.json` gains one row: `"a2a": ["envelope"]`. No other
internal import is allowed here. `a2a` stays a leaf against the
standard library; `a2aclient` is the only package granted the
third-party exception, in `docs/plans/a2aclient.md`.

## Tests

Test files live in `a2a/a2a_test/`, per
`docs/plans/agents/PHASES.md`:

- `mapping_test.go` — red-green unit cases for `ToPart` and
  `FromPart`. Cases: a minimal valid message round-trips; `ID` maps to
  `Mapped.MessageID` and back; `ThreadID` maps to `Mapped.ContextID`
  and back; an empty `Payload` fails `ToPart` through
  `Message.Validate`; a `Mapped` whose `Part.Data` is an empty JSON
  object fails `FromPart`; a `Mapped` whose `Part.Data` fails to
  unmarshal into `envelope.Message` (a structurally malformed JSON
  value, for example `Data` holding a string under a field
  `Message.Validate` expects as an object) fails `FromPart` before
  `Validate` runs, and returns no `Message` value; a `Mapped` whose
  `Part.Data` decodes to a `Message` that fails `Validate` (for
  example, an empty `payload` field) fails `FromPart` and returns no
  `Message` value; a `Mapped` whose `Part.Data` carries `thread_id`
  and `id` values that differ from `Mapped.ContextID` and
  `Mapped.MessageID` returns a `Message` whose `ThreadID` and `ID`
  match `Mapped.ContextID`/`Mapped.MessageID`, not the embedded `Data`
  values. Each case asserts the failing behavior first and turns
  green only once `ToPart`/`FromPart` exist.
- `mapping_integration_test.go` — build a message, sign it with a
  generated ed25519 key through `envelope.Sign`, map it to a `Mapped`
  value with `ToPart`, map it back with `FromPart`, then call
  `VerifySignature` on the result. Assert `ThreadID` survived as
  `Mapped.ContextID` and returned intact, and that every other field
  matches the original message by value.
- `mapping_bench_test.go` — `BenchmarkRoundTrip` runs `ToPart` then
  `FromPart` on a full message (every optional field set: `To`,
  `ContextRefs`, `Provenance.Chain`, `Provenance.Evidence`,
  `PrevHash`, `Signer`, `Signature`). Target: under fifty microseconds
  per round trip on the reference machine. `ReportAllocs` states the
  allocation count per run; the `Encode`/`Unmarshal` pair through
  `Part.Data` is the expected allocation source, so the budget is not
  zero.

Conformance vectors land in `a2a/testdata/vectors/`, prefixed
`valid_` for a correctly mapped part. Fixture format matches
`envelope/testdata/vectors/`: one JSON file per vector, the source
`envelope.Message` and its mapped `Part` side by side. `context_id`
and `message_id` sit as sibling fields next to `message` and `part` in
the vector JSON: they are `Mapped`'s fields, not part of the wire
`Part` shape, so the fixture keeps them outside the `part` object.

## Verification

`make verify` passes for the new `a2a` package: gofmt, vet, the
python gates (including `check_plan.py` and `check_deps.py` against
the new row above), the Semgrep scan, and the coverage floor at 85
for `a2a`. `make api-update` locks `Part`, `Mapped`, `ToPart`, and
`FromPart` into `api/a2a.txt` in the same change as the code.

The mapping section of `docs/architecture.md`'s "Deliberately
omitted" entry for A2A updates in the same change: it currently says
capability discovery uses the `discovery` package instead of the A2A
Agent Card; phase 9 adds one sentence recording that the envelope now
maps onto an A2A v1.0 part through the `a2a` package, still with no
task-lifecycle or transport claim.
