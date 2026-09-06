# Plan: envelope

## Goal

The wire unit for AI message exchange: a natural-language
payload inside machine-checkable, authenticatable metadata.

## Scope

Inside: Message, Ack, their validation and JSON wire form, ed25519
signing, thread chain verification, conformance vectors. Outside:
membership (room), transport, discovery, sessions.

## API

Message, Ack, Intent, Epistemic, Provenance, AckStatus types; ContextRef,
Sign, VerifyThread, Decode, DecodeAck functions; Validate/Encode/Hash/
RequiresAck/VerifySignature methods. Locked in `api/envelope.txt`.
Value semantics throughout; errors over panics; validation centralized
in Validate and called by Sign, Encode, and Decode.

VerifyThread rejects duplicate message IDs inside one thread; the
`id` uniqueness invariant from message.go gains enforcement. No
signature changes: Hash keeps its signature and gains an honest comment
(NaN/Inf Confidence fails json.Marshal; Validate first). VerifySignature
returns the marshal error it used to swallow.

Bug fix round (logic review, no exported surface change): Hash's
marshal-failure fallback. `json.Marshal(m)` fails only on NaN/Inf
Confidence. On failure the old code hashed a nil buffer, so every
such message collided on one constant hash regardless of ID or
Payload. Hash now falls back to hashing
`fmt.Sprintf("%#v:%x", m, math.Float64bits(m.Confidence))`: a
Go-syntax dump of every field, plus the raw bits of Confidence mixed
in as hex. The bits matter because `fmt`'s `%#v` renders every NaN as
the literal string "NaN", so two messages that differ only in their
NaN bit pattern (payload or sign bits) would still collide on the
dump alone. Mixing in `math.Float64bits(m.Confidence)` keeps those
messages distinct too. `math` and `fmt` are already imported in
message.go (`math` for `Validate`'s `math.IsNaN` check). Hash keeps
its exact signature: `func (m Message) Hash() string`. This path is
unreachable through VerifyThread, because Validate rejects NaN/Inf
Confidence before Hash runs; it is reachable only when a caller calls
Hash on an unvalidated Message directly (for example, a dedup or
cache key). No exported symbol is added, removed, or changed in
signature. `api/envelope.txt` needs no update; `make api-update`
must produce no diff for this change. Update Hash's doc comment
(message.go:86-88) in the same change: the current last clause
("Hash then returns the address of an empty buffer") describes the
old, buggy behavior and becomes false after the fix. Replace it with
a sentence describing the new fallback, still starting with the word
"Hash": for example, "A NaN or Inf Confidence makes json.Marshal
fail; Hash then falls back to a Go-syntax dump of m with the raw
Confidence bits mixed in, so distinct invalid messages still hash
distinctly."

## Tests

Table-driven validation cases, ack state flow, sign/verify round trips
and tamper detection, thread chain adversarial cases, conformance
vectors in testdata/vectors, FuzzDecode, benchmarks with pprof.

Additions:

- Duplicate-ID case in the VerifyThread rejection table.
- Metadata-tamper table: mutate To, MaxHops, Epistemic, or PrevHash
  after signing; expect a VerifySignature failure.
- New vector `invalid_sig_tampered_metadata.json`: a signed message
  with one metadata field changed in the JSON. Build it with Sign,
  then edit one field. The mutated value must still pass Validate
  (for example max_hops); a mutation that breaks validation belongs
  under invalid_decode_ instead.
- VerifySignature with NaN Confidence returns the marshal error.
- No vector for the duplicate-ID rule: vectors pin single-message
  wire contracts; this is a multi-message thread rule.

Additions (bug fix and test-gap round):

- `TestHashDoesNotCollideOnMarshalFailure` in message_test.go, placed
  near `TestHashIsDeterministicAndValidatesAsPrevHash`. Two
  assertions:
  - Determinism: build one message with `Confidence: math.NaN()`,
    call `Hash()` twice, assert the two results are equal. This
    covers the marshal-failure fallback path that
    `TestHashIsDeterministicAndValidatesAsPrevHash` does not reach
    (that test only covers the marshal-succeeds path).
  - Distinctness: build two messages, both with NaN Confidence, that
    differ only in their NaN bit pattern (same ID and Payload
    otherwise). Pin the exact recipe for the second NaN, to avoid a
    bit flip that silently produces a non-NaN float:
    - `bits := math.Float64bits(math.NaN())`.
    - Second value: `math.Float64frombits(bits ^ 1)`. This flips only
      the lowest mantissa bit (bit 0), staying inside the mantissa
      range (bits 0-51). The exponent bits (all 1s) stay untouched
      and the mantissa stays non-zero, so the result cannot degrade
      into a non-NaN value.
    - Before comparing hashes, assert `math.IsNaN(second.Confidence)`
      is true (`t.Fatal` if false). This self-check proves the
      constructed value is still NaN, so a future edit to this test
      cannot silently defang it.
    - Then assert the two messages' `Hash()` values differ. This
      proves the bit-mixing fix in Hash's fallback, not just
      field-content sensitivity: two messages with identical `%#v`
      dumps (since `fmt` renders every NaN as "NaN") still hash
      distinctly.
  A third case with different ID and Payload, both NaN Confidence,
  asserting distinct hashes, stays in to cover the ordinary
  content-sensitive path.
- Two new cases in sign_test.go, next to `TestVerifyRejectsMalformedSigner`
  and `TestVerifyRejectsBadSignatureLength`. Those two existing tests
  each isolate only one half of an `err != nil || len(x) != size` OR
  condition. Follow the existing single-case function style in this
  file (neither target function is table-driven):
  - A case that sets `m.Signer` to valid-lowercase-hex of the wrong
    length (for example `"aabb"`, 2 bytes, not the 32-byte ed25519
    public key). This isolates the `len(pub) != ed25519.PublicKeySize`
    half for Signer; the existing malformed-signer test only ever
    exercises the `hex.DecodeString` error half.
  - A case that sets `m.Signature` to a string that fails
    `hex.DecodeString` itself (a non-hex character, for example
    `"zz"`). This isolates the decode-error half for Signature; the
    existing bad-length test only exercises the wrong-length half.
  Each new case asserts `VerifySignature` returns a non-nil error.
- A new case in message_test.go's `TestDecodeRejectsInvalid`, or a new
  sibling test, asserting `Decode([]byte("not json"))` returns a
  non-nil error. This mirrors `TestDecodeAckRejectsInvalid` in
  ack_test.go, which already covers the equivalent `json.Unmarshal`
  syntax-error branch for `DecodeAck`. The existing
  `TestDecodeRejectsInvalid` case (`{"id":"x"}`) covers only the
  semantically-invalid-but-syntactically-valid-JSON branch, by way of
  the `m.Validate()` failure inside `Decode`.
- No new conformance vector: NaN/Inf Confidence and malformed JSON
  never pass Validate, so neither path reaches the wire vectors in
  testdata/vectors. Those vectors pin valid-message wire contracts.
- No docs/architecture.md update: none of the three items change
  wire format, validation rules, or message semantics. Hash's
  fallback only changes behavior on an already-invalid Message that
  Validate would reject; the other two items are test-only.

## Metamorphic test suite

New file `envelope/metamorphic_test.go`, package `envelope`, following
the round-trip convention already used in `a2a/mapping_test.go`,
`agent/agent_test/exchange_integration_test.go`, and
`identity/sign_integration_test.go`. Each case is a property pair:
apply a transformation to a valid input, assert the stated outcome.
Table-driven; one `TestMetamorphic*` function per property.

- `TestMetamorphicDecodeEncodeHashStable` — property: decode then
  re-encode preserves `Hash()`. Table of valid messages varying
  `Intent`, `Epistemic`, `PrevHash`, signed and unsigned. For each:
  `Encode`, `Decode`, `Encode` again. Assert the decoded message's
  `Hash()` equals the original's `Hash()`. Confirmed true against
  `message.go`: `Decode` unmarshals into the fixed-field `Message`
  struct, so `json.Marshal` always emits fields in struct order
  regardless of input key order, and `Hash` marshals the same way.
- `TestMetamorphicThreadReorderBreaksVerify` — property: reordering
  two messages in a thread breaks `VerifyThread`. Generalizes the one
  fixed `"reordered": {m1, m3, m2}` case already in
  `envelope/thread_test.go:36` to a table of valid threads, length
  three to five, each entry naming which two positions to swap
  (adjacent and non-adjacent cases). Build the chain with `Sign` and
  `PrevHash` links, swap the two named positions, assert
  `VerifyThread` returns a non-nil error. Confirmed true against
  `thread.go`: a swap either breaks the first-message
  `PrevHash == ""` rule or breaks a middle `PrevHash` match, since
  swapped messages carry different `Hash()` values.
- `TestMetamorphicDecodeRoundTrips` — property: any accepted decode
  round-trips. Table of raw JSON byte inputs that `Decode` accepts:
  reordered JSON keys, added whitespace, and one added unknown field,
  each layered over an otherwise-valid message. For each: `Decode`,
  then `Encode` the result, then `Decode` again. Assert the two
  decoded `Message` values are equal (`reflect.DeepEqual`) and their
  `Hash()` values match. Confirmed true against `message.go`: unknown
  fields are dropped by `json.Unmarshal` into a typed struct, and
  `Validate` runs identically on both decodes.

## Verification

`make verify`. Adds the conformance-vector convention: every schema or
rule change adds a `valid_`, `invalid_decode_`, or `invalid_sig_` file.
The duplicate-ID rule and the VerifySignature marshal error land with
their docs/architecture.md update in the same change.

The metamorphic suite above is test-only: no exported symbol changes,
`make api-update` must produce no diff for `api/envelope.txt`.
`go test -race ./envelope/...` covers the new file.

Bug fix and test-gap round: `make verify` covers gofmt, vet, tests,
doc gate, structure gate, Semgrep, and probes; run it after the
change. `make api-update` must produce no diff; a diff means the fix
touched the exported surface and has gone out of scope. Check file
sizes before and after: message.go (231 lines before this change),
message_test.go (282 lines before), and sign_test.go (137 lines
before) all stay at or below the 500-line structure-gate limit after
the additions, and `Hash` stays at or below the 80-line function
limit. Coverage must stay at or above 85% total and per package; this
round only adds tests and a small internal fallback, so it must not
lower the floor.

## Addendum: Sign validates a normalized copy

Part of the maintenance addenda batch. See
docs/plans/agents/maintenance-addenda-batch.md, item 6a.

`docs/plans/envelope.md:19` says validation is "centralized in
Validate and called by Encode/Decode". That list is now incomplete.
`Sign` calls `Validate` too, after its key-length check and before it
sets `Signer`.

`Sign` validates a normalized copy, not the message as supplied:

```go
check := m
check.Signer = ""
check.Signature = ""
if err := check.Validate(); err != nil {
    return Message{}, err
}
```

The copy is load-bearing. `validateSignature` at
`envelope/message.go:185` constrains `Signer` and `Signature` whenever
either field is non-empty. `Sign` overwrites both fields immediately
after. Validating them would reject the natural strip-and-re-sign
idiom, which clears `Signature` and keeps `Signer`. That is the same
move `Sign` and `VerifySignature` make internally. The normalized copy
validates exactly the content that gets signed.

One branch becomes unreachable through `Sign`. A NaN or infinite
`Confidence` is the only input that makes `json.Marshal` fail on a
`Message`, and `Validate` already rejects it. Keep the `marshal for
signing` branch as a defensive branch.

This change adds no field, no schema rule, and no `Validate` rule. The
set of valid messages on the wire is unchanged. So it needs no
conformance vector, and it does not trigger the AGENTS.md rule that
sends a message-semantics change to docs/architecture.md's "Why the
envelope is shaped this way" section.

Three more sites state the old contract and change with the code:
`envelope/message.go:107`, `docs/packages/envelope.md:106`, and
docs/architecture.md's pipeline step one at line 695.

### Addendum tests

- `envelope/sign_test.go` replaces `TestSignRejectsUnserializableMessage`
  with `TestSignRejectsInvalidMessage`. The old test pinned the
  marshal branch this change makes unreachable. The new test asserts
  the intent error and the confidence error, so it keeps two
  assertions where the old had one.
- `a2aack/a2aack_test/helpers_test.go` and `room/integration_test.go`
  each gain a local `signBypassingValidate` helper. Each case needs a
  message whose signature verifies and whose content `Validate`
  rejects, which `Sign` can no longer build.
- Each helper ends with a `VerifySignature` self-check that fails the
  test on error. The two copies re-implement envelope's canonical
  signing form, so the self-check catches drift if that form changes.

### Addendum verification

- `make verify` passes. Envelope measured 99.4 percent coverage with
  the change applied, and `Sign` measured 91.7 percent.
- No `api/` diff. No new conformance vector.
- The commit carries one `Allow-Test-Change: TT01` trailer naming both
  test replacements in the batch.

## Addendum: maintenance batch — delegate the ref-form check to contextstate

This addendum is one of three that ship in one commit. See "Addendum:
maintenance batch — drop StaleMembers and the heartbeat edge" in
`docs/plans/room.md` for the batch.

### Addendum goal

Make `contextstate` the single source of truth for the canonical ref
form. `envelope.isHashRef` delegates to `contextstate.IsRef` instead
of reimplementing it.

### Addendum scope

`envelope/message.go:219-221` declares `isHashRef`. Its body strips
`hashPrefix` and checks the remainder. `contextstate/ref.go:38-41`
declares `IsRef` with the same two steps.

This edge is not new. `envelope/message.go:12` already imports
`contextstate`, for `contextstate.HashPrefix` at line 22 and
`contextstate.Mint` at line 84. `policy/layers.json` already grants
`"envelope": ["contextstate"]`. This addendum changes no policy row
and no lock file.

Change one function body:

```go
func isHashRef(ref string) bool {
	return contextstate.IsRef(ref)
}
```

Length check, confirmed by reading both constants. `envelope` passes
`sha256.Size*2`, which is 64. `contextstate/ref.go:14` sets
`digestHexLen = 64`. The two checks accept the same set of strings,
so the delegation preserves behavior exactly.

Finding, reported instead of assumed. Do not delete `envelope`'s own
`isLowerHex`. `command grep -rn "isLowerHex" envelope/` shows two
callers besides `isHashRef`: `message.go:189` checks `m.Signer` at 64
characters, and `message.go:192` checks `m.Signature` at 128
characters. `contextstate.IsRef` cannot serve either, because both
take a bare hex string with no prefix and one of them takes a
different length. `isLowerHex` stays, with both remaining callers.

The gain is smaller than a whole-function deletion. It is still real:
the prefix-strip and length rule for a canonical ref now lives in one
package, and a future change to that form cannot leave `envelope`
behind.

Import check after the change, confirmed by grep on
`envelope/message.go`:

- `crypto/sha256` stays. Line 97 calls `sha256.Sum256`.
- `strings` stays. Lines 120 and 177 call `strings.TrimSpace`.
- `encoding/hex` stays. Line 98 calls `hex.EncodeToString`.
- The `hashPrefix` constant stays. Line 98 uses it.

No import line changes.

Out of scope: moving `contextstate/ref.go` into its own leaf package.
That is a separate plan. Do not start it here.

### Addendum API

No exported symbol changes. `isHashRef` and `isLowerHex` are both
unexported. `make api-update` must produce no diff for
`api/envelope.txt`. A diff means the change went out of scope.

`policy/layers.json` does not change.

### Addendum tests

Every existing `envelope` test stays. No test targets `isHashRef` or
`isLowerHex` by name; `command grep -rn "isHashRef\|isLowerHex"
envelope/ --include='*_test.go'` returns nothing. Both functions are
covered through `Validate`, `Decode`, the conformance vectors, and
`FuzzDecode`.

Add no test and no conformance vector. The wire contract does not
move, and the vectors under `envelope/testdata/vectors/` already pin
the accepted and rejected ref forms.

`contextstate`'s own `FuzzIsRef` in
`contextstate/contextstate_test/ref_fuzz_test.go` already proves
`IsRef` accepts exactly the canonical form. That is now the proof for
`envelope` as well.

### Addendum verification

Commands:

- `make verify`.
- `python3 scripts/check_plan.py`.
- `python3 scripts/check_deps.py`. The `envelope` row is unchanged and
  must still pass.
- `python3 scripts/check_api.py` after `make api-update`, which must
  produce no diff.
- `python3 scripts/check_docs.py`.
- `python3 scripts/check_orphan_packages.py`.
- `python3 scripts/check_prose.py`.
- `python3 scripts/check_test_tampering.py`. It must report nothing
  for `envelope`.

Coverage: `envelope`, `contextstate`, and the total must stay at or
above 85. The change removes two lines of covered logic and adds one.

Caution for the next `make mutation-gate` run: `envelope`'s stored
floor is 92, in `scripts/mutation_denylist/envelope.json`, and this
change removes two well-killed lines. The mutation gate is not part of
`make verify`, so it does not block this commit. Check the floor on
the next mutation run and do not lower it.

Doc site, found by grep, landing in the same commit:

- `docs/architecture.md`, the `envelope/` bullet: it says `ContextRef`
  delegates to `contextstate.Mint`, so every ref has one form. Add
  that the ref-form check delegates to `contextstate.IsRef` for the
  same reason.

`docs/packages/envelope.md` and `docs/packages/contextstate.md`
document no unexported helper. Leave both unchanged.

## Addendum: migrate canonical ref delegation to contextref

Status: shipped.

### Addendum goal

Migrate reference minting and checking delegation from `contextstate` to `contextref`.

### Addendum scope

Inside:

- Update `envelope/message.go` to import `contextref` instead of `contextstate`.
- Delegate `ContextRef` to `contextref.Mint`.
- Delegate `isHashRef` to `contextref.IsRef`.
- Alias `hashPrefix` to `contextref.HashPrefix`.
- Update `policy/layers.json` to replace `contextstate` with `contextref` for `envelope`.

Outside:

- Any wire format or API signature changes in `envelope`.

### Addendum API

No exported symbol changes in `api/envelope.txt`.

### Addendum tests

All existing tests in `envelope/` must continue to pass without changes.

### Addendum verification

- `policy/layers.json` updates `envelope`'s allowed imports to `["contextref"]`.
- `make verify` passes.
- `api/envelope.txt` produces no diff.

## Addendum: drop the last private isLowerHex copy

Status: shipped.

### Addendum goal

The "Delegate isHashRef" addendum above kept `envelope`'s private
`isLowerHex`, reasoning that `contextstate.IsRef` could not serve
`Signer` and `Signature`'s different lengths and prefix-free form.
That reasoning did not require the scan to stay unexported; it only
showed `IsRef` itself was the wrong function. `contextref/ref.go`
gained an exported `IsLowerHex` (see `docs/plans/contextref.md`'s
"Export IsLowerHex" addendum), so the reason to keep a second copy is
gone.

### Addendum scope

Inside:

- Delete `envelope/message.go`'s private `isLowerHex`.
- `Message.Validate`'s `Signer` and `Signature` checks call
  `contextref.IsLowerHex` directly, at the same two call sites named
  in the addendum above.

Outside:

- No change to `isHashRef`, `ContextRef`, or the wire form.

### Addendum API

No exported symbol changes in `api/envelope.txt`.

### Addendum tests

All existing `envelope` tests pass unchanged; they exercise the rule
through `Validate`, not the helper by name.

### Addendum verification

- `grep -rn "func isLowerHex" --include='*.go'` over the tree returns
  zero.
- `make verify` passes.
