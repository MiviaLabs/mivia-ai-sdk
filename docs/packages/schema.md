# Package reference: schema

The schema package compiles a JSON Schema document, validates JSON
payloads against it, and renders a bounded, model-safe corrective
message on a validation failure. `Compile` and `Validate` fail closed
on adversarial input: an oversized document, a deeply nested document,
an out-of-document `$ref`, or an oversized payload. The exported
surface below mirrors `api/schema.txt`.

## Third-party dependency

`schema` imports `github.com/santhosh-tekuri/jsonschema/v6`.
`policy/thirdparty.json` records `schema` as the exception that grants
this module, with no build tag. No other package may import it without
its own plan review and its own row in that file.

## Types

- `Compiled` — a schema ready for repeated `Validate` calls. A caller
  builds one only through `Compile`; the zero value carries no
  compiled schema.

## Constants

- `MaxSchemaBytes` (`16 << 10`) — the admission cap on a schema
  document's byte length. `Compile` checks it before it parses the
  document.
- `MaxSchemaDepth` (`32`) — the admission cap on a schema document's
  object/array nesting depth. `Compile` checks it after parsing, before
  compilation. A `$ref` hop does not count as a nesting level.
- `MaxPayloadBytes` (`64 << 10`) — the admission cap on a `Validate`
  payload's byte length, checked before any unmarshal attempt. This
  cap is symmetric with `MaxSchemaBytes`: a payload is raw tool output
  or a raw model completion, the same class of adversarial input as
  the schema document.
- `MaxCorrectiveBytes` (`1024`) — the byte cap `Corrective` truncates
  its rendered message to.

## Functions and methods

- `Compile(schemaBytes)` — admits and compiles a JSON Schema document.
  Rejects a document over `MaxSchemaBytes`, over `MaxSchemaDepth`
  nested containers, or carrying a `$ref` outside the document, each
  wrapped in `ErrAdmission` before compilation runs. Disables the
  underlying library's default `FileLoader` by calling `UseLoader(nil)`
  on the compiler, so a `$schema` keyword naming an external URI fails
  closed instead of reading local disk: the admission scan inspects
  only `$ref`, not `$schema`, and `jsonschema/v6` resolves an unhandled
  `$schema` reference through the loader during compilation. Returns
  `ErrCompile`, wrapped with the compiler's own reason, when an
  admitted document is not a legal JSON Schema. Returns a `*Compiled`
  on success.
- `Compiled.Validate(payload)` — validates `payload` as JSON against
  the compiled schema. Rejects `payload` over `MaxPayloadBytes` with
  `ErrAdmission`, before any unmarshal attempt. Returns
  `ErrMalformedPayload` when an admitted payload does not parse as
  JSON. Returns `ErrValidation`, wrapped with the failing instance
  paths and kinds, when parsed JSON does not match the schema. Returns
  nil on a match. Safe for concurrent use: many goroutines may call
  `Validate` on one shared `*Compiled` value.
- `Corrective(err)` — builds a bounded, plain-text corrective message
  from a `Validate` error, safe to resend to a model. Returns `""` for
  a nil `err`. Renders only the failing schema path and kind for
  `ErrValidation`, never the payload's failing instance value. Renders
  a fixed, generic message naming the failure kind for
  `ErrMalformedPayload`, `ErrAdmission`, and `ErrCompile`, so a
  caller-supplied payload byte stream can never inject arbitrary text
  into the corrective message. Truncates the result at
  `MaxCorrectiveBytes`, never splitting a UTF-8 rune.

## Failure modes

Use `errors.Is` to test these, except where noted.

- `ErrAdmission` ("schema: admission rejected") — `Compile`'s error
  when a schema document fails an admission cap or carries an
  out-of-document `$ref`, before compilation runs. Also `Validate`'s
  error when `payload` exceeds `MaxPayloadBytes`, before any unmarshal
  attempt.
- `ErrCompile` ("schema: compile failed") — `Compile`'s error when an
  admitted document is not a legal JSON Schema.
- `ErrMalformedPayload` ("schema: payload is not valid JSON") —
  `Validate`'s error when `payload` is not parseable JSON.
- `ErrValidation` ("schema: payload does not match schema") —
  `Validate`'s error when parsed JSON does not match the compiled
  schema. `Validate` returns this wrapped inside an unexported
  `*validationError`, which reads only the schema-derived failure path
  and kind. `errors.Is(err, ErrValidation)` still matches through its
  `Unwrap` method.

## Invariants

- `Compile` checks admission caps in a fixed order: byte length, then
  JSON parse, then nesting depth, then the `$ref` scan. Every rejection
  in this stage wraps `ErrAdmission`, before any call into the
  underlying compiler.
- `Compile` never resolves a schema reference from local disk: the
  compiled `jsonschema.Compiler` always runs with `UseLoader(nil)`.
- `Validate` checks the payload byte cap before it attempts to parse
  the payload as JSON, so an oversized payload never reaches the JSON
  decoder.
- `Corrective` never reads a `*jsonschema.ValidationError`'s
  `Got`/instance-value fields. It reads only the schema-derived
  `KeywordPath`, the structural `InstanceLocation`, and
  `kind.Required`'s declared `Missing` field names. The payload's
  actual failing value never reaches the rendered message.
- `Corrective`'s truncation never splits a UTF-8 rune: it walks
  backward from the byte cap, dropping incomplete trailing rune bytes.

## Cross-references

- [agentloop.md](agentloop.md) — `agentloop` imports `schema` to
  validate a tool call's decoded arguments and to render a corrective
  message back to the model on a schema mismatch.

## Usage

```go
compiled, err := schema.Compile([]byte(`{
    "type": "object",
    "required": ["name"],
    "properties": {"name": {"type": "string"}}
}`))
if err != nil {
    // the schema document failed an admission cap or is not legal JSON Schema
}

err = compiled.Validate([]byte(`{"name": 42}`))
if err != nil {
    msg := schema.Corrective(err)
    // msg names the failing path and kind, never the payload's "42"
}
```
