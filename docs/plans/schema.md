# Plan: schema

Status: shipped in commit 7aea007. The user authorized option A of
the third-party dependency decision below: `schema` may import
`github.com/santhosh-tekuri/jsonschema/v6`. This plan builds a new
leaf-shaped package, `schema`, alongside `channel`, `contextbudget`,
`discovery`, `durablefence`, `envelope`, `events`, `hooks`, `provider`,
`tools`, `trace`, and `trigger`. It corresponds to phase 64 in
`docs/plans/agents/PHASES.md`'s numbering. This plan is this
package's sole plan file: no standalone phase 64 plan file remains in
`docs/plans/agents/`. This plan gains a phase 64 entry in `PHASES.md`
once the phase ships, not phase 63.

## Goal

Let a caller check a JSON payload against a JSON Schema document and
get back a bounded, model-facing corrective message on failure. A tool
result or a subagent reply is untyped at the wire: `tools.Out.Value`
and a provider's raw completion text both carry `any` or a string with
no shape guarantee. `schema` answers one question: does this payload
match its declared schema, and if not, what short, safe-to-resend
explanation goes back to the model.

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
document is rejected at admission, not silently ignored.

The admission scan alone cannot catch every out-of-document reference:
it inspects only the `$ref` keyword, so a `$schema` keyword naming an
external URI reaches the compiler unblocked. By default, the
underlying `jsonschema/v6` compiler resolves an unrecognized `$schema`
URI through its own built-in `FileLoader`, which opens the referenced
path from local disk for any `file://`-scheme URL, as part of its
meta-schema and vocabulary resolution. `Compile` calls the compiler's
`UseLoader(nil)` explicitly, which disables that default loader, so
this resolution attempt fails closed with the library's own "no
URLLoader set" error, regardless of what the pre-scan missed. The
admission scan and the disabled-loader compiler are two independent
layers: the scan gives a named `ErrAdmission` reason for the common
case (an out-of-document `$ref`), and the disabled-loader compiler is
the fail-closed backstop for the case the scan's string-level check
cannot see (a `$schema` naming an external URI).

An inner `$id` can rebase resolution scope, so a `$ref` that looks
like an in-document pointer (`#/$defs/...`) resolves against the
rebased scope rather than the document root. Verified against
`jsonschema/v6` v6.0.3: this does not open a second loader vector.
The library resolves a `"#"`-prefixed `$ref` by matching the current
scope's URL against a resource id already collected from the same
document; an `$id` that rebases scope to an external URL is itself
that resource's id, so the match always succeeds in-document, and the
library never calls `Loader.Load` for it. A `$ref` target that does
not exist under the rebased scope fails with the library's own,
unrelated "pointer not found" `ErrCompile`, not a loader failure. The
admission scan's `$ref` check still rejects a directly external
(non-`"#"`-prefixed) `$ref`, whether or not scope has been rebased by
an `$id`.

`MaxSchemaDepth` counts literal object/array nesting only; it does not
count `$ref` resolution hops. A schema built from many shallow,
in-document `$defs`/`$ref` entries chained together (entry A refs
entry B refs entry C, and so on), each entry shallow on its own, can
pass both the in-document `$ref` check and the literal-nesting depth
scan, yet still amplify compiler or validator cost through the chain.
This is an accepted, documented limit for this phase: `schema` adds no
resolved-reference count cap or ref-chain-length cap. A caller that
compiles an untrusted, deeply chained schema document accepts that
cost. A ref-chain cap is a candidate for a later phase, once a real
caller's schema documents show the need.

### Third-party dependency decision (resolved: option A)

JSON Schema validation is a solved problem with real edge cases:
`allOf`/`oneOf`/`anyOf` composition, `$ref` resolution, format
validators, and draft-version differences. A hand-rolled subset
validator would diverge from any real JSON Schema document a caller
already owns, and would diverge from the sibling repo `mivia-agent`'s
own validator, which the integration assessment that opened this phase
named as the reference shape. `mivia-agent`'s `internal/jschema`
package validates structured subagent output with
`github.com/santhosh-tekuri/jsonschema/v6`, a spec-conformant,
dependency-light JSON Schema implementation with no transitive
third-party dependency of its own beyond the standard library.

The user authorized a scoped third-party exception for `schema` to
import `github.com/santhosh-tekuri/jsonschema/v6`, matching the
precedent AGENTS.md already sets for `mcp` (the official MCP Go SDK),
`ledger` (`modernc.org/sqlite`, tag-gated), and `a2aclient` (`a2a-go`
and its gRPC dial dependency). It gives this SDK the same validation
verdict `mivia-agent` already ships in production, on the same schema
documents, with no reimplementation risk.

Unlike `mcp` and `ledger`, `schema` ships with no caller in this same
phase; Scope excludes wiring into `tools.Tool` or `subagent` on
purpose. The value this phase buys is cross-repo verdict parity:
`mivia-agent`'s `internal/jschema` already validates structured
subagent output with this exact library, and a caller in this SDK that
later needs schema-checked tool output has a seam to hold against from
day one.

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

The surface below lands in `api/schema.txt` via `make api-update`.

```go
package schema

// Compiled is a schema ready for repeated Validate calls. Unexported
// fields. Built only through Compile.
type Compiled struct {
	// unexported: the compiled *jsonschema.Schema.
}

// Compile admits and compiles a JSON Schema document. Rejects a
// document over MaxSchemaBytes, over MaxSchemaDepth nested objects or
// arrays, or carrying a $ref outside the document, all before
// compilation runs. Returns ErrAdmission, wrapped with the specific
// reason, for any of the three. Calls the underlying compiler's
// UseLoader(nil) before compiling, disabling the library's default
// FileLoader, so any resolution attempt the admission scan missed
// (including one that rebases to a file:// URL) fails closed instead
// of reading from local disk. Returns ErrCompile, wrapped with the
// compiler's own reason, when the document is not a legal JSON
// Schema.
func Compile(schema []byte) (*Compiled, error)

// Validate validates payload as JSON against the compiled schema.
// Rejects payload over MaxPayloadBytes with ErrAdmission, before any
// unmarshal attempt; this mirrors Compile's byte cap on the schema
// document, since raw tool output and raw model completion text are
// this design's stated adversarial input. Returns ErrMalformedPayload
// when an admitted payload does not parse as JSON; the standard
// library's json decoder bounds its own recursion, so Validate adds
// no separate depth cap on payload. Returns ErrValidation, wrapped
// with the failing instance paths, when parsed JSON does not match
// the schema. Returns nil on a match. Safe for concurrent use: many
// goroutines may call Validate on one shared *Compiled value,
// matching this SDK's flow panel members, which run concurrently and
// may share one compiled schema across waves.
func (c *Compiled) Validate(payload []byte) error

// Corrective builds a bounded, plain-text corrective message from a
// Validate error, safe to resend to a model. Returns "" for a nil
// err. Truncates at MaxCorrectiveBytes, never splitting a UTF-8 rune.
// A non-ErrValidation error (a malformed-payload error, for example)
// still renders a bounded message naming the failure kind, not the
// raw error text, so a caller-supplied payload byte stream can never
// inject arbitrary text into the corrective message. For
// ErrValidation, renders only the failing schema path and kind, never
// the payload's failing instance value.
func Corrective(err error) string

// MaxSchemaBytes is the admission cap on a schema document's byte
// length, checked before Compile parses it.
const MaxSchemaBytes = 16 << 10

// MaxSchemaDepth is the admission cap on a schema document's
// object/array nesting depth, checked before Compile parses it.
const MaxSchemaDepth = 32

// MaxPayloadBytes is the admission cap on a Validate payload's byte
// length, checked before Validate unmarshals it. Symmetric to
// MaxSchemaBytes: the payload is this design's stated adversarial
// input (raw tool output, a raw model completion), so it gets the
// same fail-closed treatment as the schema document.
const MaxPayloadBytes = 64 << 10

// MaxCorrectiveBytes is the byte cap Corrective truncates its output
// to.
const MaxCorrectiveBytes = 1024

// ErrAdmission is Compile's error when a schema document fails an
// admission cap or carries an out-of-document $ref, before
// compilation runs. It is also Validate's error when payload exceeds
// MaxPayloadBytes, before any unmarshal attempt. Test with errors.Is.
var ErrAdmission error

// ErrCompile is Compile's error when an admitted document is not a
// legal JSON Schema. Test with errors.Is.
var ErrCompile error

// ErrMalformedPayload is Validate's error when payload is not
// parseable JSON. Test with errors.Is.
var ErrMalformedPayload error

// ErrValidation is Validate's error when parsed JSON does not match
// the compiled schema. Test with errors.Is.
var ErrValidation error
```

### Byte-oriented signatures, not `map[string]any`

`Compile` takes `[]byte`, not `map[string]any`. `provider.
ToolDefinition.Schema` already stores a tool's schema as raw, unparsed
bytes; a byte-oriented `Compile` signature reads that field directly
with no intermediate unmarshal step at the call site. The same
reasoning applies to `Validate`: a tool's raw output, or a model's raw
completion text, arrives as bytes or a string a caller converts to
bytes, not as a pre-decoded `any`. `schema` unmarshals both
internally, once, and reports `ErrMalformedPayload` for `payload`,
`ErrAdmission` for an unparseable or oversized `schema` document.

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
  - A schema exactly at `MaxSchemaBytes` compiles with no error, and a
    schema one byte over `MaxSchemaBytes` is rejected with
    `ErrAdmission`, before any parse attempt; a probe on the compiler's
    parse path (a seam swapped in the test) proves it was never
    called for the over-cap case.
  - A schema nested exactly to `MaxSchemaDepth` compiles with no
    error, and a schema nested one level past `MaxSchemaDepth` is
    rejected with `ErrAdmission`.
  - A schema carrying a `$ref` outside the document (an absolute URL
    or a relative file path) is rejected with `ErrAdmission`. A
    schema carrying an in-document `$ref` (`#/$defs/...`) compiles.
  - A schema whose inner `$id` rebases resolution scope to a `file://`
    URL (a temp file the test creates and controls), with a `$ref`
    that reads as an in-document pointer (`#/$defs/...`) but targets a
    fragment that does not exist under the rebased scope, is rejected
    with `ErrCompile`. This proves the rebase does not smuggle a
    resolvable-looking `$ref` past compilation; it does not exercise
    `UseLoader(nil)`, since `jsonschema/v6` v6.0.3 resolves a
    `"#"`-prefixed `$ref` against the document's own already-collected
    resources and never calls the loader for it, verified by
    mutation-testing this library version directly.
  - A schema whose `$schema` keyword names a `file://` URL pointing at
    a temp file the test creates and controls (holding a distinctive
    marker payload) is rejected with `ErrCompile`, naming the
    library's own "no URLLoader set" reason, and the marker content
    never appears in the compiled schema or the returned error. This
    is the proven vector for the disabled-loader backstop: the
    admission scan's string-level check inspects only `$ref`, so a
    `$schema` naming an external URI reaches the compiler unblocked,
    and `UseLoader(nil)` is what stops the resulting `Loader.Load`
    call from reading local disk. Mutation-tested: with
    `UseLoader(nil)` removed, this same fixture compiles and the
    external file's content is read into the schema document.
  - Malformed JSON as the schema document itself (not valid JSON at
    all) is rejected with `ErrAdmission`, not `ErrCompile`.
  - A syntactically valid JSON document that is not a legal JSON
    Schema (for example, `"type": "not-a-real-type"`) is rejected
    with `ErrCompile`.
  - A schema built from a long chain of shallow, in-document
    `$defs`/`$ref` entries (entry A refs entry B refs entry C, and so
    on, each entry one level deep) compiles with no error. This case
    proves, and names in the test comment, the documented limit from
    Scope: `MaxSchemaDepth` counts literal nesting only, so a
    ref-chain amplification schema passes admission and compiles,
    by design, not by omission.
- `validate_test.go` — red-green cases for `Compiled.Validate`.
  - A payload matching a simple object schema (`type: object`, one
    required string property) returns nil.
  - A payload missing a required field returns `ErrValidation`,
    wrapped, and `Corrective(err)` names the missing field and stays
    at or under `MaxCorrectiveBytes`.
  - A payload that is not valid JSON returns `ErrMalformedPayload`.
  - A payload matching every property but carrying an
    `additionalProperties: false` violation returns `ErrValidation`.
  - A payload exactly at `MaxPayloadBytes` (matching the schema)
    returns nil, and a payload one byte over `MaxPayloadBytes` returns
    `ErrAdmission`, before any unmarshal attempt; a probe on the
    unmarshal path (a seam swapped in the test) proves it was never
    called for the over-cap case, mirroring the `Compile`-side
    oversized-schema case.
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
    `ErrMalformedPayload` case) still returns a bounded message naming
    the failure kind, not the raw payload bytes.
  - `Corrective` on an `ErrAdmission`-wrapped error, taken from an
    oversized-schema `Compile` failure and separately from an
    oversized-payload `Validate` failure, still returns a bounded
    message naming the failure kind. The output stays at or under
    `MaxCorrectiveBytes` and never contains the raw oversized input
    bytes, proving `Corrective` bounds the admission path the same way
    it bounds the malformed-payload path.
  - `Corrective` on an `ErrValidation` error whose failing value is a
    long, attacker-shaped string (for example, a `pattern` or `enum`
    mismatch on a payload value that embeds prompt-injection-style
    text, "ignore previous instructions and ...") asserts the returned
    string does not contain that literal instance value. The
    underlying `jsonschema/v6` library's `ValidationError` often
    quotes the offending instance value in its own message text;
    `Corrective` must render only the schema path and the failure
    kind (for example, "pattern mismatch at /field"), never the
    instance value itself. This case fails if `Corrective` ever
    formats the validator's raw error string directly.
- `schema_integration_test.go` — compiles a realistic tool-output
  schema shaped like a structured review verdict: `type: object`,
  `required: ["verdict", "findings"]`, `verdict` an enum of two
  values, `findings` an array of strings. Validates a matching payload
  (nil error), a payload with a bad enum value (`ErrValidation`,
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

`policy/layers.json` already carries the `schema` row
(`"schema": []`, since `schema` imports no other package in this
module). That row only pins internal import direction and grants no
third-party exception on its own. The third-party exception itself
lands through the doc and gate edits below, all in the same change as
the code:

- AGENTS.md's third-party-dependency Rules-section sentence gains a
  named exception clause for `schema`, alongside the existing
  `a2aclient`, `mcp`, and `ledger` clauses:
  `` `schema` may import `github.com/santhosh-tekuri/jsonschema/v6` ``.
- AGENTS.md's Layout section gains one `schema/` bullet, naming
  `Compiled`, `Compile`, `Validate`, `Corrective`, the four `Max*`
  constants, and the four `Err*` sentinels, and stating it imports the
  `github.com/santhosh-tekuri/jsonschema/v6` library externally and no
  internal package.
- `semgrep/sdk-standards.yml` excludes `/schema/*.go` from
  `sdk.go.stdlib-only-imports`'s `paths.exclude` list, alongside the
  existing `a2aclient`, `mcp`, and `ledger` exclusions.
- `semgrep/sdk-standards.yml` gains a new rule,
  `sdk.go.schema-scoped-third-party-import`, `severity: ERROR`, scoped
  to `paths.include: ["/schema/*.go"]`, following the
  `sdk.go.mcp-scoped-third-party-import` pattern exactly: the same
  dotted-domain `pattern-regex`, the module-path
  `pattern-not-regex` exemption, and one further `pattern-not-regex`
  permitting only
  `"github\.com/santhosh-tekuri/jsonschema/v6(/[^"\n]*)?"`. Any other
  third-party import inside `schema/*.go` still fires this rule.
- `scripts/check_semgrep_probes.py` gains a matching probe case,
  parallel to the existing `mcp`-scoped block: a `schema/` subdirectory
  under the probe's temp root, holding `viol_other_import.go`
  (importing an unrelated third-party path) and
  `clean_jsonschema_import.go` (importing
  `github.com/santhosh-tekuri/jsonschema/v6`). Both basenames register
  in `expected` against `sdk.go.schema-scoped-third-party-import`, and
  an explicit assertion block proves: the new rule fires on
  `viol_other_import.go` and stays silent on
  `clean_jsonschema_import.go`; `sdk.go.stdlib-only-imports` fires on
  neither, proving the scoped exclude took effect; and an
  outside-the-directory probe file still proves
  `sdk.go.stdlib-only-imports` fires normally outside every scoped
  directory.
- `go.mod` gains a `require` line for
  `github.com/santhosh-tekuri/jsonschema/v6`, plus any indirect lines
  `go mod tidy` adds beneath it. `scripts/check_gomod.py`'s
  `ALLOWED_MODULES` set gains the resolved module paths that import
  chain actually adds, reconciled against real `go mod tidy` output
  the same way `docs/plans/mcp.md`'s equivalent section records for
  its own allowlist. `check_gomod.py`'s module docstring gains one
  sentence naming the `schema` exception.
- `docs/architecture.md`'s Package map section gains a `schema/`
  package-map entry, listing its empty internal-import row and its
  third-party dependency on `github.com/santhosh-tekuri/jsonschema/v6`.
  The section's opening sentence names "thirty-one packages"; it
  becomes "thirty-two packages". The leaf-enumerating sentence
  currently names `channel`, `contextbudget`, `discovery`,
  `durablefence`, `envelope`, `events`, `hooks`, `provider`, `tools`,
  `trace`, and `trigger`; `schema` joins that list. The mermaid
  diagram gains one per-leaf node line, `schema[schema]`, alongside
  the existing `contextbudget[contextbudget]` line, so the diagram
  does not go stale.
- `make verify` passes: gofmt, vet, tests (including `go test -race
  ./schema/...` for the concurrency claim on `Validate`), the doc
  gate, the structure gate, the Semgrep scan (including the two rule
  changes above), and the probes.
- The coverage floor for `schema` holds at or above 85 percent.
- `api/schema.txt` lands via `make api-update` and locks `Compiled`,
  `Compile`, `Validate`, `Corrective`, `MaxSchemaBytes`,
  `MaxSchemaDepth`, `MaxPayloadBytes`, `MaxCorrectiveBytes`,
  `ErrAdmission`, `ErrCompile`, `ErrMalformedPayload`, and
  `ErrValidation`.
- `docs/plans/agents/PHASES.md` gains a phase 64 entry once the phase
  ships, following the phase 62 entry's pattern. This is a Stage 5
  concern for the delivery loop, not a change this plan itself makes.
