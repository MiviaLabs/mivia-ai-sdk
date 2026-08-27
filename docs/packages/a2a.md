# Package reference: a2a

The a2a package maps an `envelope.Message` onto an A2A v1.0 message
part and back. It carries no network call and no third-party import.
The exported surface below mirrors `api/a2a.txt`.

## Types

- `Part` — one A2A v1.0 message part. A2A v1.0 has no `kind` field
  and no separate part classes: one part carries text, raw bytes, a
  url, or a data object. `Part` carries no message-level field.
  Fields: `Text`, `Data` (`json.RawMessage`), `Raw`, `URL`.
- `Mapped` — the result of `ToPart` and the input to `FromPart`. It
  pairs a `Part` with the two A2A message-level fields `Part` cannot
  carry: `ContextID` (the envelope `ThreadID`) and `MessageID` (the
  envelope `ID`).

## Functions

- `ToPart(m)` — validates `m`, then builds `Part.Data` by calling
  `m.Encode` and wrapping the result in `json.RawMessage`. Returns an
  error, not a zero `Mapped`, on a `Validate` failure. Signs nothing
  and does not modify `m`.
- `FromPart(mapped)` — unmarshals `mapped.Part.Data` into an
  `envelope.Message`, overwrites `ThreadID` with `mapped.ContextID`
  and `ID` with `mapped.MessageID`, then calls `Validate` before
  returning. Returns an error, not a partial `Message`, on a decode
  or a `Validate` failure.

## Invariants

- `ToPart` never returns a non-zero `Mapped` alongside a non-nil
  error.
- `FromPart` never returns a non-zero `Message` alongside a non-nil
  error. A malformed or invalid part never crosses the a2a boundary.
- `FromPart` applies the `ContextID`/`MessageID` override before it
  calls `Validate`. The override wins over any `thread_id`/`id`
  already embedded in `Part.Data`, even when the override empties an
  otherwise-valid embedded message.

## Wire contract

- `Part.Data` carries the exact bytes `envelope.Message.Encode`
  produces. `FromPart` reads it with `encoding/json.Unmarshal`, not
  `envelope.Decode`, because the `ContextID`/`MessageID` override must
  run before `Validate`.
- `Text`, `Raw`, and `URL` stay empty. `ToPart` sets only `Part.Data`.
- Conformance vectors live in `a2a/testdata/vectors/`, `valid_`
  prefixed. A vector pairs the source `envelope.Message` with its
  mapped `Part`, `ContextID`, and `MessageID`.

## Why this shape

`Part` mirrors the A2A v1.0 wire shape: one struct, no `kind` field,
no separate part classes. `ContextID` and `MessageID`
sit on `Mapped`, not on `Part`, because in the real A2A wire form
they belong to the wrapping A2A `Message`, not to an individual
`Part`.

## Failure modes

This package returns plain errors, not sentinels. A caller cannot
match them with `errors.Is`.

- `ToPart` fails when `m.Validate()` fails, for example on an unset
  `Version` or an invalid `Intent`. Pinned by `a2a_test/mapping_test.go`.
- `FromPart` fails when `mapped.Part.Data` does not unmarshal into an
  `envelope.Message`. Pinned by `a2a_test/mapping_test.go`.
- `FromPart` fails when the mapped message, after the `ContextID`/
  `MessageID` override, fails `Validate`. Pinned by
  `a2a_test/mapping_test.go`.

## Usage

```go
m := envelope.Message{
    Version:    envelope.Version,
    ID:         "msg-1",
    ThreadID:   "thread-1",
    Intent:     envelope.IntentAssert,
    Epistemic:  envelope.EpistemicInferred,
    Confidence: 0.5,
    Provenance: envelope.Provenance{Source: "model:self"},
    Payload:    "The build is green.",
}
mapped, _ := a2a.ToPart(m)
_ = mapped.ContextID // "thread-1"
_ = mapped.MessageID // "msg-1"

got, _ := a2a.FromPart(mapped)
_ = got.Payload // "The build is green."
```
