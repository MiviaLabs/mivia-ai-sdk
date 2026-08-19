# Phase 64: schema

Status: plan, needs the user's explicit sign-off before a builder
starts. This plan requests a scoped third-party import exception. See
"Third-party dependency decision" below. Ships as one new leaf-shaped
package, `schema`, alongside `tools`, `hooks`, and `trace`.

## Goal

Let a caller check a JSON payload against a JSON Schema document and
get back a bounded, model-facing corrective message on failure. A
tool result or a subagent reply is untyped at the wire: `tools.Out.
Value` and a provider's raw completion text both carry `any` or a
string with no shape guarantee. `schema` answers one question: does
this payload match its declared schema, and if not, what short,
safe-to-resend explanation goes back to the model.

## Scope

Inside: compiling a JSON Schema document, validating a JSON payload
against it, and building a bounded corrective error message. Inside:
admission caps that reject an oversized or too-deeply-nested schema
before compilation runs, so a hostile or malformed schema document
never reaches the compiler. Inside: a compiled-schema type a caller
can validate more than one payload against without recompiling.

Outside: any change to `tools.Tool`, `tools.InOut`, or `tools.Out`.
This phase ships the validator alone; wiring it into `tools.Tool`'s
input or output path is a later phase's integration concern, the same
way `hooks` shipped standalone before `agentrun` wired it into the ack
chain. Outside: schema generation or inference from a Go type.
Reflection-based schema-from-struct is a distinct, larger feature with
no caller in this phase. Outside: any provider-specific structured-
output or forced-tool-call wiring. `provider.ToolDefinition.Schema`
already stores a tool's parameter schema as raw, unparsed bytes;
`schema` reads that shape but adds no field to `provider` and no
provider import. Outside: remote `$ref` resolution. A schema document
validates only against itself; a `$ref` that points outside the
document is rejected at admission, not silently ignored. The
admission scan alone cannot catch every out-of-document reference: an
inner `$id` can rebase resolution scope, so a `$ref` that looks like
an in-document pointer (`#/$defs/...`) can still resolve against an
attacker-chosen external base once scope has shifted. By default, the
underlying `jsonschema/v6` compiler resolves an out-of-scope reference
through its own built-in `FileLoader`, which opens the referenced path
from local disk for any `file://`-scheme URL. `Compile` calls the
compiler's `UseLoader(nil)` explicitly, which disables that default
loader, so every resolution attempt beyond the document, `file://`
schemes included, fails closed with the library's own "no URLLoader
set" error, regardless of what the pre-scan missed. The admission scan
and the disabled-loader compiler are two independent layers: the scan
gives a named `ErrAdmission` reason for the common case, and the
disabled-loader compiler is the fail-closed backstop for the case the
scan's string-level check cannot see.

### Third-party dependency decision

JSON Schema validation is a solved problem with real edge cases:
`allOf`/`oneOf`/`anyOf` composition, `$ref` resolution, format
validators, and the draft-version differences between them. A
hand-rolled subset validator (type, required, properties only) would
diverge from any real JSON Schema document a caller already owns, and
would diverge specifically from the sibling repo `mivia-agent`'s own
validator, which the integration assessment that opened this phase
named as the reference shape. `mivia-agent`'s `internal/jschema`
package validates structured subagent output with
`github.com/santhosh-tekuri/jsonschema/v6`, a spec-conformant,
dependency-light JSON Schema implementation with no transitive
third-party dependency of its own beyond the standard library.

This plan requests a scoped third-party exception for `schema` to
import `github.com/santhosh-tekuri/jsonschema/v6`, matching the
precedent AGENTS.md already sets for `mcp` (the official MCP Go SDK)
and `ledger` (`modernc.org/sqlite`, tag-gated). The alternative,
option B, is a stdlib-only structural validator covering only type,
required, and properties, with `$ref`, `allOf`/`oneOf`/`anyOf`, and
format validators explicitly out of scope until a full-spec exception
is granted in a future phase. Option B ships a validator whose pass
verdict on a real-world schema (one with `enum`, `pattern`, or
`oneOf`) can disagree with `mivia-agent`'s own verdict on the same
schema and the same payload: this SDK would accept a payload
`mivia-agent` rejects, or the reverse. That divergence defeats the
stated goal of a validation seam a caller can trust across both
codebases.

**Recommendation: option A**, the scoped third-party exception naming
`github.com/santhosh-tekuri/jsonschema/v6`. It gives this SDK the same
validation verdict `mivia-agent` already ships in production, on the
same schema documents, with no reimplementation risk. This decision
needs the user's explicit authorization before a builder starts,
per AGENTS.md's rule that no package beyond `a2aclient`, `mcp`, and
`ledger` may add a third-party import without its own plan review.
Until that authorization lands, this plan builds nothing.

Unlike `mcp` and `ledger`, `schema` ships with no caller in this same
phase; Scope excludes wiring into `tools.Tool` or `subagent` on
purpose. The user should still accept this dependency risk now,
before a caller exists, because the value this phase buys is
cross-repo verdict parity: `mivia-agent`'s `internal/jschema` already
validates structured subagent output with this exact library, and
every phase this SDK delays the import, a caller in this SDK that
needs schema-checked tool output has no seam to hold against, and the
two codebases' agents risk silently disagreeing on well-formed input
the moment either one grows its own validator first. Paying the
dependency cost one phase early, with a stdlib-scoped Semgrep rule
pinning the one allowed import path, keeps that risk at zero for as
long as `schema` stays unwired.

### No compiled-schema cache

`Compile` returns a `*Compiled` value the caller holds and reuses
across many `Validate` calls; that reuse is the caller's own loop, not
a cache this package keeps for it. `schema` adds no keyed cache
mapping a schema's bytes to a `*Compiled` value. A caller that compiles
the same schema bytes across many calls, for example once per tool
invocation, already avoids recompiling by holding the one `*Compiled`
value it built at startup, matching how a caller holds one
`tools.Registry` rather than rebuilding it per call. A hash-keyed
cache adds a map, a hash function, and an eviction policy with no
caller in this phase asking for one; the smallest correct shape stays
"the caller holds the value it already built."

## API

- `type Compiled struct` — a schema ready for repeated `Validate`
  calls. Unexported fields. Built only through `Compile`.
- `func Compile(schema []byte) (*Compiled, error)` — admits and
  compiles a JSON Schema document. Rejects a document over
  `MaxSchemaBytes`, over `MaxSchemaDepth` nested objects or arrays, or
  carrying a `$ref` outside the document, all before compilation runs.
  Returns `ErrAdmission`, wrapped with the specific reason, for any of
  the three. Calls the underlying compiler's `UseLoader(nil)` before
  compiling, disabling the library's default `FileLoader`, so any
  resolution attempt the admission scan missed (including one that
  rebases to a `file://` URL) fails closed instead of reading from
  local disk. Returns `ErrCompile`, wrapped with the compiler's own
  reason, when the document is not a legal JSON Schema.
- `(*Compiled) Validate(payload []byte) error` — validates `payload`
  as JSON against the compiled schema. Rejects `payload` over
  `MaxPayloadBytes` with `ErrAdmission`, before any unmarshal attempt;
  this mirrors `Compile`'s byte cap on the schema document, since raw
  tool output and raw model completion text are this design's stated
  adversarial input. Returns `ErrMalformedPayload` when an admitted
  `payload` does not parse as JSON; the standard library's `json`
  decoder bounds its own recursion, so `Validate` adds no separate
  depth cap on `payload`. Returns `ErrValidation`, wrapped with the
  failing instance paths, when parsed JSON does not match the schema.
  Returns nil on a match. Safe for concurrent use: many goroutines may
  call `Validate` on one shared `*Compiled` value, matching this SDK's
  `flow` panel members, which run concurrently and may share one
  compiled schema across waves.
- `func Corrective(err error) string` — builds a bounded, plain-text
  corrective message from a `Validate` error, safe to resend to a
  model. Returns "" for a nil err. Truncates at `MaxCorrectiveBytes`,
  never splitting a UTF-8 rune. A non-`ErrValidation` error (a
  malformed-payload error, for example) still renders a bounded
  message naming the failure kind, not the raw error text, so a
  caller-supplied payload byte stream can never inject arbitrary text
  into the corrective message.
- `const MaxSchemaBytes = 16 << 10` — the admission cap on a schema
  document's byte length, checked before `Compile` parses it.
- `const MaxSchemaDepth = 32` — the admission cap on a schema
  document's object/array nesting depth, checked before `Compile`
  parses it.
- `const MaxPayloadBytes = 64 << 10` — the admission cap on a
  `Validate` payload's byte length, checked before `Validate`
  unmarshals it. Symmetric to `MaxSchemaBytes`: the payload is this
  design's stated adversarial input (raw tool output, a raw model
  completion), so it gets the same fail-closed treatment as the
  schema document.
- `const MaxCorrectiveBytes = 1024` — the byte cap `Corrective`
  truncates its output to.
- `var ErrAdmission` — `Compile`'s error when a schema document fails
  an admission cap or carries an out-of-document `$ref`, before
  compilation runs. `Validate`'s error when `payload` exceeds
  `MaxPayloadBytes`, before any unmarshal attempt. Test with
  `errors.Is`.
- `var ErrCompile` — `Compile`'s error when an admitted document is
  not a legal JSON Schema. Test with `errors.Is`.
- `var ErrMalformedPayload` — `Validate`'s error when `payload` is not
  parseable JSON. Test with `errors.Is`.
- `var ErrValidation` — `Validate`'s error when parsed JSON does not
  match the compiled schema. Test with `errors.Is`.

### Byte-oriented signatures, not `map[string]any`

`Compile` takes `[]byte`, not `map[string]any`. `provider.
ToolDefinition.Schema` already stores a tool's schema as raw,
unparsed bytes; a byte-oriented `Compile` signature reads that field
directly with no intermediate unmarshal step at the call site. The
same reasoning applies to `Validate`: a tool's raw output, or a
model's raw completion text, arrives as bytes or a string a caller
converts to bytes, not as a pre-decoded `any`. `schema` unmarshals
both internally, once, and reports `ErrMalformedPayload` for
`payload`, `ErrAdmission` for an unparseable or oversized `schema`
document.

### Errors carry the sentinel, not the raw error string

`Validate` and `Compile` wrap their sentinel with `fmt.Errorf("%w: ...
")`, so a caller matches the failure kind with `errors.Is` and reads
the detail with `err.Error()`. `Corrective` never repeats a caller's
own payload bytes back into its output; it summarizes the failing
schema paths from the validator's own structured error, bounded to
`MaxCorrectiveBytes`.

## Tests

Test files live in `schema/schema_test/`, an external test package.

- `compile_test.go` — red-green cases for `Compile`.
  - A well-formed object schema (`type`, `properties`, `required`)
    compiles with no error.
  - A schema over `MaxSchemaBytes` is rejected with `ErrAdmission`,
    before any parse attempt; a probe on the compiler's parse path
    (a seam swapped in the test) proves it was never called.
  - A schema nested past `MaxSchemaDepth` is rejected with
    `ErrAdmission`.
  - A schema carrying a `$ref` outside the document (an absolute URL
    or a relative file path) is rejected with `ErrAdmission`. A
    schema carrying an in-document `$ref` (`#/$defs/...`) compiles.
  - A schema whose inner `$id` rebases resolution scope to a
    `file://` URL (for example, a temp file the test creates and
    controls, or `file:///etc/hostname`), so a `$ref` that reads as
    an in-document pointer (`#/$defs/...`) would resolve against that
    external file if scope shifted, is still rejected: either
    `ErrAdmission` from the scan or `ErrCompile` from the
    `UseLoader(nil)` compiler's own "no URLLoader set" resolution
    failure. The `file://` scheme is deliberate: the underlying
    library's default `FileLoader` would serve this exact scheme, so
    an `http(s)://` rebase target would let a compiler that never
    called `UseLoader(nil)` pass this test for the wrong reason. This
    case proves the disabled-loader backstop holds even when the
    string-level admission scan cannot see the rebase, and that the
    test file (or `/etc/hostname`) is never read into the compiled
    schema.
  - Malformed JSON as the schema document itself (not valid JSON at
    all) is rejected with `ErrAdmission`, not `ErrCompile`.
  - A syntactically valid JSON document that is not a legal JSON
    Schema (for example, `"type": "not-a-real-type"`) is rejected
    with `ErrCompile`.
- `validate_test.go` — red-green cases for `Compiled.Validate`.
  - A payload matching a simple object schema (`type: object`, one
    required string property) returns nil.
  - A payload missing a required field returns `ErrValidation`,
    wrapped, and `Corrective(err)` names the missing field and stays
    at or under `MaxCorrectiveBytes`.
  - A payload that is not valid JSON returns `ErrMalformedPayload`.
  - A payload matching every property but carrying an
    `additionalProperties: false` violation returns `ErrValidation`.
  - A payload over `MaxPayloadBytes` returns `ErrAdmission`, before
    any unmarshal attempt; a probe on the unmarshal path (a seam
    swapped in the test) proves it was never called, mirroring the
    `Compile`-side oversized-schema case.
- `validate_concurrent_test.go` — runs `go test -race` against many
  goroutines calling `Validate` on one shared `*Compiled` value built
  once, each with its own payload (a mix of matching and
  schema-violating payloads). No data race and no incorrect verdict
  under `-race`, proving the concurrent-use claim in the API section.
- `corrective_test.go` — red-green cases for `Corrective`.
  - `Corrective(nil)` returns "".
  - An oversized validation-error detail truncates to
    `MaxCorrectiveBytes` and never splits a UTF-8 rune (mirrors the
    rune-boundary case `mivia-agent`'s own corrective formatter
    guards).
  - `Corrective` on a non-nil, non-`ErrValidation` error (an
    `ErrMalformedPayload` case) still returns a bounded message
    naming the failure kind, not the raw payload bytes.
  - `Corrective` on an `ErrAdmission`-wrapped error, taken from an
    oversized-schema `Compile` failure and separately from an
    oversized-payload `Validate` failure, still returns a bounded
    message naming the failure kind. The output stays at or under
    `MaxCorrectiveBytes` and never contains the raw oversized input
    bytes, proving `Corrective` bounds the admission path the same
    way it bounds the malformed-payload path.
- `schema_integration_test.go` — compiles a realistic tool-output
  schema shaped like a structured review verdict: `type: object`,
  `required: ["verdict", "findings"]`, `verdict` an enum of two
  values, `findings` an array of strings. Validates a matching
  payload (nil error), a payload with a bad enum value (`ErrValidation`,
  `Corrective` names the failing path), and a payload missing
  `findings` (`ErrValidation`, `Corrective` names the missing field).
  Proves one `*Compiled` value validates more than one payload with no
  recompile.
- `validate_bench_test.go` — benchmark `Compiled.Validate` on the
  integration test's realistic schema, one call per iteration on a
  matching payload. States the allocation budget for one `Validate`
  call. Records the measured baseline in this plan once the phase
  ships.

## Verification

This plan builds nothing until the user authorizes the
`github.com/santhosh-tekuri/jsonschema/v6` exception named above.
`policy/layers.json` already carries the `schema` row this plan adds
(`[]`, since `schema` imports no other package in this module); that
row only pins internal import direction and grants no third-party
exception on its own. Once the user authorizes the exception:

- AGENTS.md's third-party-dependency rule gains a named exception row
  for `schema`, alongside the existing `a2aclient`, `mcp`, and
  `ledger` rows, in the same change as the code.
- `semgrep/sdk-standards.yml` excludes `/schema/*.go` from
  `sdk.go.stdlib-only-imports` and gains a
  `sdk.go.schema-scoped-third-party-import` rule pinning
  `github.com/santhosh-tekuri/jsonschema/v6` as the only allowed
  import, following the `sdk.go.mcp-scoped-third-party-import`
  pattern. `scripts/check_semgrep_probes.py` gains a matching
  violation/clean fixture pair for the new rule ID, in the same
  change as the code. Without this, `make verify-fast`'s Semgrep scan
  fails on the first import line in `schema`.
- `make verify` passes: gofmt, vet, tests (including `go test -race`
  for the concurrency claim on `Validate`), the doc gate, the
  structure gate, the Semgrep scan, and the probes.
- The coverage floor for `schema` holds at or above 85 percent.
- `api/schema.txt` lands via `make api-update` and locks `Compiled`,
  `Compile`, `Validate`, `Corrective`, `MaxSchemaBytes`,
  `MaxSchemaDepth`, `MaxPayloadBytes`, `MaxCorrectiveBytes`,
  `ErrAdmission`, `ErrCompile`, `ErrMalformedPayload`, and
  `ErrValidation`.
- `docs/plans/agents/PHASES.md` gains a phase 63 entry once the phase
  ships, following the phase 62 entry's pattern.
