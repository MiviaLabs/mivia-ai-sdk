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
// no separate part classes: one part carries text or a data object.
// Part carries no message-level field. ContextID and MessageID belong
// to the wrapping A2A Message, not to Part, so they live on Mapped,
// alongside Part, not on Part itself.
type Part struct {
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
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
// value. It returns an error, not a zero Mapped, on failure: m.Encode
// validates m before it marshals, so an invalid message never reaches
// Part.Text. It signs nothing and does not modify m. ToPart sets
// Part.Text to the exact bytes m.Encode returns; Data stays empty.
// Mapped.ContextID carries m.ThreadID and Mapped.MessageID carries
// m.ID.
func ToPart(m envelope.Message) (Mapped, error)

// FromPart maps a Mapped value back to an envelope.Message. It
// unmarshals mapped.Part.Text first; an empty Text decodes
// mapped.Part.Data instead, so an old peer's data part still maps
// until v0.4.0. Text wins over Data when both are set; both empty
// fails the decode. FromPart then overwrites ThreadID with
// mapped.ContextID and ID with mapped.MessageID before calling
// Validate. It returns an error instead of an invalid Message: no
// malformed part crosses the a2a boundary silently.
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
- `Part` carries only part-level content: `Text` and `Data`.
  `ContextID` and `MessageID` are A2A Message-level fields in the
  real wire shape, so they never sit on `Part`; they live on `Mapped`
  instead. `a2aclient` wires `Mapped.ContextID` and `Mapped.MessageID`
  into `a2aproject/a2a-go`'s `Message` type without touching `Part`'s
  shape.
- `ToPart` performs no signing and no mutation. The caller signs
  `m` with `envelope.Sign` or `identity.Sign` before calling `ToPart`,
  the same way `envelope.Encode` expects a caller-prepared message.
- `ToPart` builds `Part.Text` through `m.Encode()`, not a direct
  `json.Marshal` call. `semgrep/sdk-standards.yml`'s
  `sdk.go.marshal-via-encode` rule forbids `json.Marshal` outside
  `envelope`'s own `message.go`, `sign.go`, `ack.go`, `wire.go`, and
  test files. `a2a` is not on that exemption list, so `ToPart` reuses
  `envelope.Message.Encode` and converts the returned bytes to a
  string. A conversion to string is not a marshal, so the rule stays
  silent. `FromPart`'s `json.Unmarshal` is not a marshal-side call,
  so the rule does not apply there either.
- `FromPart` mirrors `envelope.Decode`'s parse-then-validate order
  (`docs/architecture.md`, Message flow, step 4): unmarshal the
  carrier into `envelope.Message`, overwrite `ThreadID` with
  `mapped.ContextID` and `ID` with `mapped.MessageID`, then call
  `Validate`. The `Mapped.ContextID`/`Mapped.MessageID` fields win
  over any `thread_id`/`id` already present in `Data`. A parse success
  with a failed `Validate` still returns an error, never a partial
  `Message`. A carrier that fails to unmarshal into
  `envelope.Message` also returns an error, before `Validate` ever
  runs.
- `ToPart` returns `Mapped`, not a wider tuple. No function in this
  codebase (`envelope`, `identity`, `room`, `machine`, `flow`) needs
  a wider result, and two adjacent string returns (`contextID`,
  `messageID`) invite an accidental swap at the call site. `Mapped` is exported because `a2aclient` callers need `Part`,
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

## Addendum: ToPart drops its duplicate Validate call
Status: shipped.


### Addendum goal

Delete the `m.Validate()` call in `ToPart`. `m.Encode()` on the next
line already validates. No behavior, API, or policy changes.

### Addendum scope

Delete `a2a/mapping.go` lines 42-44:

```go
	if err := m.Validate(); err != nil {
		return Mapped{}, err
	}
```

`ToPart`'s body then starts at `data, err := m.Encode()`.

Rewrite the second sentence of `ToPart`'s doc comment. The current
text claims a call the body no longer holds. The new comment is:

```go
// ToPart maps a signed or unsigned envelope.Message onto a Mapped
// value. It returns an error, not a zero Mapped, on failure: m.Encode
// validates m before it marshals, so an invalid message never reaches
// Part.Data. It signs nothing and does not modify m. ToPart
// builds Part.Data by calling m.Encode and wrapping the result in
// json.RawMessage, reusing envelope's wire encoder instead of a
// second marshal call; Text, Raw, and URL stay empty. Mapped.ContextID
// carries m.ThreadID and Mapped.MessageID carries m.ID.
```

Update the same comment where this plan quotes it, in the API section
above. Update three more prose sites that name the deleted call:

- `docs/packages/a2a.md`, the `ToPart(m)` bullet. New text: `ToPart(m)`
  builds `Part.Data` by calling `m.Encode` and wrapping the result in
  `json.RawMessage`. `m.Encode` validates `m` before it marshals, so
  `ToPart` returns an error, not a zero `Mapped`, on a `Validate`
  failure. Signs nothing and does not modify `m`.
- `a2a/a2a_test/mapping_test.go:63`, the comment on
  `TestToPartRejectsInvalidMessage`. Replace "proves ToPart calls
  Validate first" with "proves ToPart rejects an invalid message".
- `a2aclient/client_test.go:119`. Replace "which a2a.ToPart calls"
  with "which a2a.ToPart reaches through m.Encode".

Leave `docs/architecture.md` and
`docs/examples/a2a-mapping-roundtrip.md` unchanged. Both say `ToPart`
validates, which stays true.

Outside the addendum: `FromPart`, `Part`, `Mapped`, and every
`envelope` function.

### Addendum proof

`envelope.Message.Encode` at `envelope/message.go:199` calls
`m.Validate()` as its first statement and returns the error
unwrapped. No output and no other failure precedes it. `ToPart`'s
error value for an invalid message is therefore unchanged.

No test asserts a `ToPart` error string or error identity. The five
call sites that exercise the failure assert only `err != nil`:
`a2a/a2a_test/mapping_test.go:69`, `a2aclient/client_test.go:121`,
`a2aloopback/loopback_test.go:166`, and the two `agent` integration
mappers.

### Addendum tests

Add `TestToPartInvalidMessageErrorMatchesValidate` to
`a2a/a2a_test/mapping_test.go`. It builds a message with an empty
`Payload`, keeps `m.Validate()`'s error, calls `ToPart`, and asserts
the two error strings are equal. The test fails if a future `Encode`
wraps the validation error.

`TestToPartRejectsInvalidMessage` stays. It now reaches `ToPart`
through `Encode` and catches the mistake of dropping validation
altogether.

### Addendum coverage

`scripts/mutation_denylist/` holds no `a2a.json`, so `a2a` carries no
stored mutation floor. The neighboring `a2aclient.json` names a
different package, at floor 95.

Measured on a scratch copy: `a2a` moves from 92.9% to 100.0%. The
duplicate call made `ToPart`'s `Encode` error branch at
`a2a/mapping.go:46` unreachable, and that branch was the package's
only uncovered statement. The deletion makes it reachable, and
`TestToPartRejectsInvalidMessage` covers it.

### Addendum verification

`make verify` passes. `go test ./a2a/... ./a2aclient/...
./a2aloopback/... ./agent/...` passes. `make api-update` produces no
diff: the change edits one function body and one comment, and adds no
exported symbol. A non-empty `api/` diff is a failure.
`policy/layers.json` needs no row.

Predicted `scripts/check_test_tampering.py` findings: none. The change
deletes no test and adds one. The builder does not add a trailer.

## Addendum: the envelope crosses A2A as text

Status: shipped.

### Addendum goal

Carry the envelope's signed JSON bytes as `Part.Text`, a plain Go
string. Stop carrying them as `Part.Data`, whose `map[string]any` hop
rounds integers through a proto float64. The signature covers exact
bytes. A string carrier removes the numeric-precision class of bugs.

Remove `Part.Raw` and `Part.URL` as well. A grep over `a2a`,
`a2aclient`, `a2aloopback`, and `a2aack` finds no setter and no reader
of either field. Both are dead exported surface.

### Why text is the lossless carrier

`a2a-go` v0.3.15 declares `TextPart` with one Go string field
(`a2a/core.go:581`). The proto hop carries that field as a proto
string. A proto string preserves arbitrary UTF-8 bytes byte-exact.
`m.Encode()` emits UTF-8 JSON, so the text hop preserves every byte,
including integer literals above 2^53. The `DataPart` hop instead
carries `map[string]any`, and proto converts every value through
float64, which rounds those literals and breaks the signature check.
The string carrier is therefore the lossless choice, and no in-band
marker scheme is needed.

### Addendum scope

`a2a/mapping.go`:

- `Part` keeps `Text` and `Data`. It loses `Raw` and `URL`.
- `ToPart` sets `Part.Text` to `string(m.Encode())`. It no longer
  sets `Data`. It keeps `m.Encode()` as its byte source and adds no
  `json.Marshal` call. `sdk.go.marshal-via-encode` excludes no `a2a`
  file, so a marshal call here would fail the scan; a conversion to
  string is not a marshal.
- `FromPart` reads `Text` first. When `Text` is empty, it decodes
  `Data` instead, so an old peer's data part still decodes. When both
  are set, `Text` wins and `Data` is ignored. An empty `Text` and an
  empty `Data` fail the decode and return an error.
- The `Data` fallback exists for one release. It ships in v0.3.0, the
  release after the current v0.2.1 tag. Delete the fallback in
  v0.4.0, in the same change that deletes `a2aclient`'s data-part
  fallback and this package's `valid_mapped.json` vector.

Rewrite `ToPart`'s and `FromPart`'s doc comments for the new carrier
and the fallback. Update this plan's own `## API` code block and its
design notes to the post-change shape in the same commit. Update
`docs/packages/a2a.md` the same way: the field list, the `ToPart(m)`
bullet, the `FromPart(mapped)` bullet, the invariants naming
`Part.Data`, and the line that says `Text`, `Raw`, and `URL` stay
empty. Update `docs/examples/a2a-mapping-roundtrip.md`: its diagram
node, its example code, and its prose that names `Part.Data`.

`api/a2a.txt`: `make api-update` removes the `Raw` and `URL` lines of
the `Part` entry. Commit the lock diff in the same change.

`policy/layers.json` needs no change. `policy/thirdparty.json` needs
no change: `a2a` imports nothing new and stays standard-library only.

Conformance vectors: add `a2a/testdata/vectors/valid_mapped_text.json`
alongside the existing `valid_mapped.json`. Use the existing
`vectorFixture` shape: `message`, `part`, `context_id`, and
`message_id` as sibling fields. The new vector's `part` holds only
`"text"`, carrying the exact `m.Encode()` bytes of `message` as one
JSON string. Generate the file by running the new `ToPart`; do not
hand-write the bytes. Keep `valid_mapped.json` unchanged while the
fallback exists. Adding a vector file fires no tampering rule; do not
modify the old file in place.

### Addendum tests

All test names below live in `a2a/a2a_test/`, package `a2a_test`:

- `TestToPartRoundTrip` in `mapping_test.go`: replace the `Part.Data`
  assertion. Assert `Part.Text` is non-empty and `Part.Data` is empty.
- `TestFromPartReadsTextFirst` in `mapping_test.go`: new. Build a
  `Part` with a valid `Text` and a different valid `Data`. `FromPart`
  returns the message decoded from `Text`.
- `TestFromPartRejectsMalformedText` in `mapping_test.go`: new. A
  `Text` holding a malformed JSON value fails `FromPart` with a
  decode error and a zero `Message`.
- `TestFromPartRejectsEmptyPart` in `mapping_test.go`: new. Empty
  `Text` and empty `Data` fail `FromPart` and return no `Message`.
- `TestSignedRoundTripKeepsLargeIntegers` in
  `mapping_integration_test.go`: new. Sign a message with `MaxHops`
  9007199254740993, map through `ToPart` and `FromPart`, and assert
  `VerifySignature` passes and `MaxHops` equals the original.
- `TestConformanceVectors` in `mapping_test.go`: rework. Drop the
  `Part.Data` byte comparison. For every vector, assert `FromPart` on
  the fixture part reproduces the fixture message, and assert the
  same for a fresh `ToPart` mapping of the fixture message.
- `TestTextVectorByteExact` in `mapping_test.go`: new. Load
  `valid_mapped_text.json` and assert `ToPart` of the fixture message
  reproduces the fixture's `part.text`, byte for byte.
- `TestFromPartRejectsEmptyData` and `TestFromPartRejectsMalformedData`
  in `mapping_test.go` stay. They now pin the `Data` fallback's two
  error paths.
- `FuzzFromPart` in `fuzz_test.go`: run the fixed-point body over both
  carriers, `Part{Text: string(data)}` and `Part{Data: data}`.

Fallback coverage needs no new case: `TestFromPartOverridesEmbeddedIDs`
and the two data failure cases already drive `Data` through
`FromPart`.

### Addendum verification

`make api-update` produces exactly one lock diff: `api/a2a.txt` loses
the `Raw` and `URL` lines of `Part`. Commit it in the same change.

`python3 scripts/check_plan.py`, `check_deps.py`, `check_prose.py`,
and `check_labels.py` pass. `make verify` passes, including the
coverage floor at 85 for `a2a`.

Predicted tampering findings: none from this package. The change
deletes no test function and no vector, and it adds assertion sites.
The slice's commit-level trailer inventory lives in
`docs/plans/a2aclient.md`'s addendum.
