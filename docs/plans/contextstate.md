# Plan: contextstate

Status: shipped. One new leaf package plus one unification inside
`envelope`, ported from `mivia-agent`'s `internal/contextstate` and
`internal/contentref` under the phase 65 contract. `contextplan`
plans a session's context fit on these types; `spool` spools
oversized tool output.

## Goal

Hold the durable context contract and the single canonical
content-reference minter. Sessions, checkpoints, commit validation,
retention classes, and volume `Limits` live here. `envelope` and
`memory` reuse the minter, so every ref in this SDK has one form.

## Scope

Inside:

- A `contextstate` leaf importing nothing in this module.
- The canonical minter: `HashPrefix`, `Digest`, `Mint`, `IsRef`.
- The contract types: `ContentRef`, `NewContentRef`, `PayloadRecord`
  with chunk reassembly through `Reassemble`, `SourceID`,
  `SourceRange`, `SourceEvent`, `Revision`, `BindingRevision`,
  `CheckpointID`, `Checkpoint`, `Session`, `RetentionClass`,
  `CommitRequest` with `Validate`, and `Limits` with `Validate`.
- A `MemStore` with `New`, `Put`, `Get`, `Checkpoint`, and `Session`,
  following `ledger.MemStore`'s precedent: mutex-guarded, built
  through a constructor, enforcing volume bounds at write time.
- One delegation in `envelope`: `ContextRef` keeps its signature and
  delegates its body to the minter. `memory` stays code-unchanged and
  delegates transitively through `envelope`.

Outside:

- Worktree machinery: `WorktreeInstance`, worktree states, and every
  worktree catalog interface.
- The session catalog and the TUI picker surface: `SessionCatalog`,
  `SessionCatalogInfo`, title and dir helpers, admission records.
- Audit records: `AuditRecord`, `AuditAction`.
- Import, export, and lifecycle: `DeleteResult`, `ExportResult`,
  `ImportResult`, `LegacyImporter`, `SessionLifecycle`, tombstones.
- `AdvanceRequest` and `EnsureSessionRequest`.
- The sanitized read path: `SanitizedPayload`, `RedactionPolicy`,
  `SanitizeSourcePayload`, and redaction classification.
- `PolicySnapshot`.
- `Principal` and its secret capability. The SDK keeps plain
  validated ownership string fields on `ContentRef`.
- The source's process-wide `SetLimits`, `CurrentLimits`,
  `DefaultLimits`, `Exceeds`, and `PayloadChunkSize`. This SDK owns no
  shared mutable global; see the Limits section.
- `MarshalCanonical`, `UnmarshalCanonical`, and
  `FingerprintCommitRequest`. See the API resolutions.
- `DefaultPayloadChunkBytes` and `Limits.SourceEventBytes`. The
  MemStore stores whole values; an in-memory chunk split would be
  allocation with no row bound. Chunk granularity is a caller choice;
  `Reassemble` is agnostic to it.
- The source's `ref:<kind>:<digest>` form and its `KindOutput`,
  `KindError`, `KindMessage` kinds. The canonical form is the
  existing `envelope` wire form; see the API resolutions.
- Provider-message conversion, planning, elision, and calibration.
  Phase 66 owns those.
- The truncation spool. Phase 67 owns it.
- Any SQLite or file backend. A later phase may add a tagged store.
- The mivia namespace constant `mivia.context.payload.v1`. The caller
  owns the namespace; `ContentRef.Namespace` is a validated field.

## API

The surface lands in `api/contextstate.txt` via `make api-update`.

Resolutions of the phase sketch, binding for the builder:

- The sketch's `func ContentRef(namespace string, chunks ...[]byte)
  ContentRef` is illegal Go: a function and a type cannot share one
  name. The constructor is `NewContentRef`; the low-level minter is
  `Mint` plus `Digest`; the form check is `IsRef`.
- The digest form is `envelope`'s existing form, not the source's:
  `sha256:` followed by 64 lowercase hex characters. The wire form
  stays stable; the fork ends on `envelope`'s side.
- Minting over the concatenation of chunks equals minting over the
  concatenated bytes: `Mint(a, b)` equals `Mint(ab)`. The digest never
  mixes in the namespace or the owner fields.
- Minting over zero total bytes returns the digest of the empty input,
  matching `envelope.ContextRef("")` today. The source's empty-string
  return for empty data does not port.
- The commit fingerprint does not port. Its only consumer is the
  durable store's retry disambiguation, which stays app-side. The
  MemStore detects a reused operation key by comparing requests
  structurally. This drops `CommitRequest.BaseDigest`,
  `CommitRequest.Fingerprint`, `FingerprintCommitRequest`, and the
  canonical JSON encoder they require.

### The minter

- `const HashPrefix = "sha256:"` — the one place the literal appears.
  The Semgrep allowlist moves here; see Verification.
- `func Digest(chunks ...[]byte) string` — SHA-256 over the ordered
  concatenation, as 64 lowercase hex characters.
- `func Mint(chunks ...[]byte) string` — `HashPrefix` plus `Digest`.
  This is the canonical ref string; `envelope.ContextRef` delegates to
  it.
- `func IsRef(ref string) bool` — reports the canonical form:
  `HashPrefix`, then exactly 64 lowercase hex characters. No
  surrounding whitespace is tolerated.

### Contract types

- `type ContentRef struct` keeps all seven source fields: `Ref`,
  `Namespace`, `SHA256`, `WorkspaceID`, `SessionID`, `SubjectID`,
  `Size`. `Ref` is the address a caller hands around; `SHA256` is the
  bare digest the payload checks compare against; the three owner
  strings replace `Principal`; `Size` is the whole-payload byte count.
- `(ContentRef) Validate` enforces: `IsRef(Ref)`; `Ref` equals
  `HashPrefix` plus `SHA256`, which implies `SHA256` is 64 lowercase
  hex characters; `Namespace` and the three owner strings pass the
  identifier bound; `Size` is not negative. `Namespace` is never
  compared with any SDK constant.
- `func NewContentRef(namespace string, workspaceID string, sessionID
  string, subjectID string, chunks ...[]byte) (ContentRef, error)` —
  mints `Ref`, `SHA256`, and `Size` over the concatenation, fills the
  given namespace and owner fields, validates the result, and wraps
  `ErrInvalidRecord` on failure.
- `type RetentionClass string` with `RetentionSession` and
  `RetentionCompliance`. `PayloadRecord.Validate` requires a non-empty
  class and accepts any non-empty value, so a caller may define its
  own classes.
- `type PayloadRecord struct` keeps `Ref`, `Retention`, `Revoked`, and
  `Data`. `Validate` enforces: `Ref` validates; `Retention` is
  non-empty; when `Data` is present, its length equals `Ref.Size` and
  its digest equals `Ref.SHA256`.
- `func Reassemble(ref ContentRef, retention RetentionClass, chunks
  ...[]byte) (PayloadRecord, error)` — concatenates the chunks in
  order under one ref, fails closed on a size or digest mismatch, and
  returns the record with `Data` set. The whole-payload digest is the
  contract; chunk boundaries are storage granularity.
- `type SourceID struct` keeps `SessionID` and `Sequence`; `Validate`
  bounds the identifier.
- `type SourceRange struct` keeps `Start` and `End`; `Validate`
  enforces one session, an ordered span, and a span under
  `MaxSourceRangeEvents`.
- `type SourceEvent struct` keeps `ID`, `Kind`, `Role`, `ToolCallID`,
  `PayloadRef`, `Provenance`, `RedactionStatus`, and `Size`. `Validate`
  bounds the four required text fields at 256 bytes, the two optional
  fields when set, and rejects a negative `Size`. `PayloadRef` stays a
  bounded string, not a forced canonical form, so app-side key schemes
  stay legal.
- `type Revision struct` keeps `Session`, `Durable`, `Source`. It
  carries no `Validate`; the source's was a no-op.
- `type BindingRevision struct` keeps `Provider`, `Model`,
  `Generation`; `Validate` bounds both identifiers and requires a
  positive generation.
- `type CheckpointID struct` keeps `SessionID`, `SourceRange`,
  `Algorithm`, `SchemaVersion`, `IdempotencyKey`. Dropped:
  `SummaryModel`, whose only consumer is the summarizer, which does
  not port. `Validate` enforces the identifier bounds, a valid
  same-session `SourceRange`, an `Algorithm` bounded at 64 bytes, a
  positive `SchemaVersion`, and a bounded `IdempotencyKey`.
- `type Checkpoint struct` is `CheckpointRecord` slimmed. Kept: `ID`,
  `Revision`, `Binding`, `ActiveContext`, `TurnID`. Dropped:
  `SummaryMetadata`, whose consumer does not port; the record-level
  `SourceRange`, a duplicate of `ID.SourceRange`; and `Complete`, an
  always-true field the constructor set and the validator demanded.
  `Validate` enforces: `ID` validates; `Binding` validates;
  `ActiveContext` is non-empty; `TurnID` is positive.
- `type Session struct` is `Snapshot` slimmed. Kept: `Revision`,
  `Binding`, `Active Checkpoint`, `Source []SourceEvent`. Dropped:
  `Tombstoned`, which belongs to the lifecycle surface that does not
  port. `Session` is a read model; it carries no `Validate`, because
  every part carries its own.
- Constructors that only wrap a struct literal do not port
  (`NewSourceID`, `NewSourceRange`, `NewBindingRevision`,
  `NewCheckpointID`, `NewRevision`, `NewPrincipal`). Constructors that
  compute do port: `NewContentRef`, `Reassemble`, `NewCommitRequest`.

### CommitRequest

- `type CommitRequest struct` keeps: `OperationID`, `SessionID`,
  `Expected`, `ExpectedBinding`, `NewSourceEvents`, `Payloads`,
  `Checkpoint`, `NewSession`, `NewDurable`, `NewSourceSequence`,
  `NewBinding`, `TurnID`. Dropped: `Principal`, `WorktreeInstance`,
  and the `ActiveContext` duplicate, plus `BaseDigest` and
  `Fingerprint` per the resolutions.
- `func NewCommitRequest(sessionID string, expected Revision,
  expectedBinding BindingRevision, events []SourceEvent, payloads
  []PayloadRecord, checkpoint Checkpoint, newBinding BindingRevision,
  turnID uint64) (CommitRequest, error)` — sets `OperationID` from
  `checkpoint.ID.IdempotencyKey`, derives the three new revision
  fields from `expected` and `len(events)`, validates, and returns the
  request. Payloads are a parameter, not an afterthought: the request
  is complete at construction.
- `func (r CommitRequest) Validate() error` enforces shape only, in
  this order:
  - Identity: `SessionID` and `OperationID` pass the identifier bound;
    `ExpectedBinding` and `NewBinding` validate.
  - Revision: `NewSession` is `Expected.Session` plus one;
    `NewDurable` is `Expected.Durable` plus one; `NewSourceSequence`
    is `Expected.Source` plus the event count.
  - Events: every event validates; every event belongs to `SessionID`;
    sequences run contiguously from `Expected.Source` plus one.
  - Payloads: every payload validates; every payload
    `Ref.SessionID` equals `SessionID`. The workspace and subject
    cross-checks drop with `Principal`.
  - Checkpoint: `Checkpoint` validates; `Checkpoint.Revision` equals
    the three new revision fields; `Checkpoint.Binding` equals
    `NewBinding`; `TurnID` equals `Checkpoint.TurnID` and is positive;
    the checkpoint range covers the new events, or ends at
    `Expected.Source` when the commit carries no events.

### Limits

- `type Limits struct` keeps three fields: `CheckpointBytes`,
  `CommitEvents`, `CommitEventBytes`. Dropped with their dead
  consumers: `SourceEventBytes` and `SessionStateBytes` — the latter
  bounded the same active-context bytes `CheckpointBytes` bounds, so
  one bound survives — plus `ExportBytes`, `SummaryMetadataBytes`, and
  `CheckpointMetadataBytes`.
- `(Limits) Validate` rejects a negative field and names it, checking
  `CheckpointBytes` first, then `CommitEvents`, then
  `CommitEventBytes`. Zero or positive passes; zero means uncapped,
  matching `contextbudget.Limits`.
- Enforcement shape: `Validate` stays shape-only, and the `MemStore`
  enforces volume at write time. The phase pins
  `func (c CommitRequest) Validate() error` with no limits argument.
  A caller-owned value reaches the one write choke point, the same
  pattern `contextbudget` set against globals and `ledger.MemStore`
  set at the store. A second `ValidateWithin` method would duplicate
  the path and invite a caller to skip it.
- At `Checkpoint` write time the store rejects, wrapping
  `ErrOverLimit` and storing nothing: an event count over
  `CommitEvents`; an event `Size` total over `CommitEventBytes`; an
  `ActiveContext` length over `CheckpointBytes`. A zero field means
  uncapped.

### MemStore

- `func New(limits Limits) (*MemStore, error)` — validates `limits`
  and wraps `ErrInvalidRecord` on a negative field. Mutex-guarded,
  safe for concurrent use; the zero value is not usable.
- `(MemStore) Put(record PayloadRecord) error` — validates the record
  and stores a copy under `record.Ref.Ref`. Content-addressed, so a
  repeat `Put` of equal bytes overwrites in place.
- `(MemStore) Get(ref ContentRef) (PayloadRecord, error)` — returns a
  copy of the record stored under `ref.Ref`; an unknown ref wraps
  `ErrPayloadNotFound`.
- `(MemStore) Checkpoint(req CommitRequest) error` — answers the
  operation key first: a reused `OperationID` with an equal request is
  a no-op success, and with a different request wraps
  `ErrCheckpointConflict`, before any other check. A new key then runs
  `req.Validate`, the volume bounds, and the stale guards: a stored
  revision unequal to `req.Expected` wraps `ErrStaleRevision`; a
  stored binding unequal to `req.ExpectedBinding` wraps
  `ErrStaleBinding`. Equality compares fields; slices compare
  element-wise and byte slices through `bytes.Equal`. On success the
  store keeps every payload, appends the events, sets the active
  checkpoint, and advances revision and binding. An unknown session
  commits only against a zero `Expected`.
- `(MemStore) Session(id string) (Session, error)` — returns the
  session's revision, binding, active checkpoint, and a copy of its
  events; an unknown id wraps `ErrSessionNotFound`.

### Sentinels and bounds

- `var ErrInvalidRecord` wraps every validation failure;
  `type ValidationError struct { Field, Reason string }` carries the
  offending field, implements `Error` and `Unwrap` into the sentinel.
- `var ErrSessionNotFound`, `ErrStaleRevision`, `ErrStaleBinding`,
  `ErrCheckpointConflict`, `ErrPayloadNotFound`, `ErrOverLimit`. Each
  has a producer in this package.
- Ported shape bounds: `MaxIdentifierBytes` at 128,
  `MaxPayloadReferenceBytes` at 256, `MaxSourceRangeEvents` at
  `100_000`. Not ported: `Namespace`, `DefaultMaxCheckpointMetadata`,
  `DefaultMaxSummaryMetadata`, `MaxAuditBytes`.

### The envelope delegation

- `envelope/message.go`: `ContextRef` keeps its signature; its body
  becomes `return contextstate.Mint([]byte(content))`. The `hashPrefix`
  const becomes the alias `const hashPrefix = contextstate.HashPrefix`.
  `isHashRef` and `Message.Hash` keep using the alias; no other line
  changes.
- No exported `envelope` symbol changes. `api/envelope.txt` must show
  no diff. `memory` changes no line.

The expected lock content:

```text
package contextstate
  const HashPrefix = "sha256:"
  const MaxIdentifierBytes = 128
  const MaxPayloadReferenceBytes = 256
  const MaxSourceRangeEvents = 100_000
  const RetentionCompliance = "compliance"
  const RetentionSession = "session"
  func (b BindingRevision) Validate() (error)
  func (c Checkpoint) Validate() (error)
  func (c CheckpointID) Validate() (error)
  func (e *ValidationError) Error() (string)
  func (e *ValidationError) Unwrap() (error)
  func (e SourceEvent) Validate() (error)
  func (id SourceID) Validate() (error)
  func (l Limits) Validate() (error)
  func (m *MemStore) Checkpoint(req CommitRequest) (error)
  func (m *MemStore) Get(ref ContentRef) (PayloadRecord, error)
  func (m *MemStore) Put(record PayloadRecord) (error)
  func (m *MemStore) Session(id string) (Session, error)
  func (p PayloadRecord) Validate() (error)
  func (r CommitRequest) Validate() (error)
  func (r ContentRef) Validate() (error)
  func (r SourceRange) Validate() (error)
  func Digest(chunks ...[]byte) (string)
  func IsRef(ref string) (bool)
  func Mint(chunks ...[]byte) (string)
  func New(limits Limits) (*MemStore, error)
  func NewCommitRequest(sessionID string, expected Revision, expectedBinding BindingRevision, events []SourceEvent, payloads []PayloadRecord, checkpoint Checkpoint, newBinding BindingRevision, turnID uint64) (CommitRequest, error)
  func NewContentRef(namespace string, workspaceID string, sessionID string, subjectID string, chunks ...[]byte) (ContentRef, error)
  func Reassemble(ref ContentRef, retention RetentionClass, chunks ...[]byte) (PayloadRecord, error)
  type BindingRevision struct {
  Provider string `json:"provider"`
  Model string `json:"model"`
  Generation uint64 `json:"generation"`
}
  type Checkpoint struct {
  ID CheckpointID `json:"id"`
  Revision Revision `json:"revision"`
  Binding BindingRevision `json:"binding"`
  ActiveContext []byte `json:"active_context"`
  TurnID uint64 `json:"turn_id"`
}
  type CheckpointID struct {
  SessionID string `json:"session_id"`
  SourceRange SourceRange `json:"source_range"`
  Algorithm string `json:"algorithm"`
  SchemaVersion uint32 `json:"schema_version"`
  IdempotencyKey string `json:"idempotency_key"`
}
  type CommitRequest struct {
  OperationID string `json:"operation_id"`
  SessionID string `json:"session_id"`
  Expected Revision `json:"expected"`
  ExpectedBinding BindingRevision `json:"expected_binding"`
  NewSourceEvents []SourceEvent `json:"new_source_events"`
  Payloads []PayloadRecord `json:"payloads,omitempty"`
  Checkpoint Checkpoint `json:"checkpoint"`
  NewSession uint64 `json:"new_session"`
  NewDurable uint64 `json:"new_durable"`
  NewSourceSequence uint64 `json:"new_source_sequence"`
  NewBinding BindingRevision `json:"new_binding"`
  TurnID uint64 `json:"turn_id"`
}
  type ContentRef struct {
  Ref string `json:"ref"`
  Namespace string `json:"namespace"`
  SHA256 string `json:"sha256"`
  WorkspaceID string `json:"workspace_id"`
  SessionID string `json:"session_id"`
  SubjectID string `json:"subject_id"`
  Size int `json:"size"`
}
  type Limits struct {
  CheckpointBytes int
  CommitEvents int
  CommitEventBytes int
}
  type MemStore struct {
}
  type PayloadRecord struct {
  Ref ContentRef `json:"ref"`
  Retention RetentionClass `json:"retention"`
  Revoked bool `json:"revoked"`
  Data []byte `json:"data,omitempty"`
}
  type RetentionClass string
  type Revision struct {
  Session uint64 `json:"session"`
  Durable uint64 `json:"durable"`
  Source uint64 `json:"source"`
}
  type Session struct {
  Revision Revision `json:"revision"`
  Binding BindingRevision `json:"binding"`
  Active Checkpoint `json:"active"`
  Source []SourceEvent `json:"source"`
}
  type SourceEvent struct {
  ID SourceID `json:"id"`
  Kind string `json:"kind"`
  Role string `json:"role"`
  ToolCallID string `json:"tool_call_id,omitempty"`
  PayloadRef string `json:"payload_ref,omitempty"`
  Provenance string `json:"provenance"`
  RedactionStatus string `json:"redaction_status"`
  Size int `json:"size"`
}
  type SourceID struct {
  SessionID string `json:"session_id"`
  Sequence uint64 `json:"sequence"`
}
  type SourceRange struct {
  Start SourceID `json:"start"`
  End SourceID `json:"end"`
}
  type ValidationError struct {
  Field string
  Reason string
}
  var ErrCheckpointConflict
  var ErrInvalidRecord
  var ErrOverLimit
  var ErrPayloadNotFound
  var ErrSessionNotFound
  var ErrStaleBinding
  var ErrStaleRevision
```

## File layout

- `contextstate/doc.go` — package doc and file map.
- `contextstate/ref.go` — `HashPrefix`, `Digest`, `Mint`, `IsRef`.
- `contextstate/contracts.go` — shape bounds, sentinels,
  `ValidationError`, the bounded-text helpers, `ContentRef`,
  `NewContentRef`, `RetentionClass`, `PayloadRecord`, `Reassemble`.
- `contextstate/checkpoint.go` — `SourceID`, `SourceRange`,
  `SourceEvent`, `Revision`, `BindingRevision`, `CheckpointID`,
  `Checkpoint`, `Session`, and their validators.
- `contextstate/commit.go` — `CommitRequest`, `NewCommitRequest`,
  `Validate`, and one helper per validation section.
- `contextstate/limits.go` — `Limits` and `Validate`.
- `contextstate/store.go` — `MemStore`, `New`, `Put`, `Get`,
  `Checkpoint`, `Session`, and the request-equality helper.

Every file stays at or below 500 lines; every function stays at or
below 80 lines. Stdlib only.

## Tests

Tests live in `contextstate/contextstate_test/`, an external test
package. The builder writes each file's assertions first, records the
red run, then writes the code.

- `ref_test.go` — mint determinism; `Mint(a, b)` equals `Mint(ab)`
  across byte-aligned and rune-splitting splits; the zero-chunk and
  empty-chunk digests match the known empty-input digest; `IsRef`
  acceptance and rejection table, adapted from the source corpus:
  empty, bare digest, 16-hex truncated, 63-hex, 65-hex, oversized,
  uppercase, non-hex, whitespace, extra colon, canonical.
- `payload_test.go` — `PayloadRecord.Validate` rejections: invalid
  ref, empty retention, size mismatch, digest mismatch; acceptance
  with the session and compliance classes plus one caller-defined
  class. `Reassemble`: ordered concatenation under one ref; single
  chunk equals the whole; wrong total size fails closed; digest
  mismatch fails closed; invalid ref fails closed.
- `checkpoint_test.go` — the ported contract cases:
  `SourceRange` cross-session, reversed, boundary at
  `MaxSourceRangeEvents`, and one over it; `SourceID`, `SourceEvent`,
  `BindingRevision`, `CheckpointID`, and `Checkpoint` validators,
  including the same-session range rule and the empty-context and
  zero-turn rejections.
- `commit_test.go` — `NewCommitRequest` round trip; the ported
  rejection table, one case per `Validate` rule: wrong session id,
  blank operation id, non-next revisions, sequence mismatch with the
  event count, an event from another session, a non-contiguous
  sequence, a payload from another session, checkpoint revision and
  binding mismatches, turn mismatch, a range that misses the new
  events, and an empty-commit range that misses the source head.
- `limits_test.go` — zero-value passes; each positive passes; each
  negative alone fails; both negative proves the check order.
- `store_test.go` — `New` rejects a negative `Limits`; `Put`/`Get`
  round trip and `ErrPayloadNotFound`; a first commit against a zero
  `Expected` creates the session; `Session` returns the applied
  revision, binding, checkpoint, and events, and wraps
  `ErrSessionNotFound` for an unknown id; `ErrStaleRevision` and
  `ErrStaleBinding` on a wrong expected value; a retried equal request
  is a no-op success; each volume bound rejects at write
  time with `ErrOverLimit` and stores nothing; zero-value `Limits`
  admits an arbitrarily large commit; concurrent `Put`, `Get`,
  `Checkpoint`, and `Session` under `-race`.
  - Conflict table: commit one request, then retry under its
    `OperationID`, varying exactly one field per row. Rows cover
    `NewSession`, `NewDurable`, `NewSourceSequence`, `Expected`,
    `ExpectedBinding`, `NewBinding`, one `SourceEvent` field, one
    payload `Data` byte, `Checkpoint.ActiveContext`, `TurnID`, and
    the `Checkpoint.ID.IdempotencyKey` content that `OperationID`
    mirrors, varied with the reused `OperationID` held fixed. Every
    row wraps `ErrCheckpointConflict`, so a comparator that omits one
    field fails a row.
  - Every rejection case asserts `errors.Is` against its sentinel.
    The `New`, `Put`, and `Checkpoint` validation failures assert
    `errors.Is` against `ErrInvalidRecord`, covering
    `ValidationError.Unwrap`.
- `ref_fuzz_test.go` — `FuzzIsRef` seeds carry the rejection classes
  above plus canonical refs; the target asserts no panic and
  soundness: `IsRef` accepts exactly the canonical form. A reassembly
  target partitions random bytes into random chunks, mints over the
  whole, and asserts `Reassemble` succeeds; flipping one byte in one
  chunk must fail closed.
- `minter_conformance_integration_test.go` — the delegation and
  conformance proofs. One case per minter pins byte-identical refs
  against the minter, with exact digest literals written in the file:
  `envelope.ContextRef`, and `memory.Store.Put` through a real store.
  Both must equal `Mint` for the pinned inputs, for the empty input,
  and for a multi-chunk split. Any diff in `memory/` fails the design,
  not the test.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `contextstate` and for the total.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass against the policy rows `"contextstate": []` and
  `"envelope": ["contextstate"]`.
- `api/contextstate.txt` lands through `make api-update` and matches
  the API section. `api/envelope.txt` must show no diff; no other
  lock file changes.
- The `envelope` conformance vectors do not change. The delegation
  changes no byte on the wire, so `envelope/testdata/vectors/` stays
  untouched.
- The Semgrep allowlist move is deliberate and lands with the code.
  The builder edits `semgrep/sdk-standards.yml`: rule
  `sdk.go.hash-prefix-centralized` allowlists `HashPrefix` in
  `contextstate/ref.go` instead of `hashPrefix` in
  `envelope/message.go`, and the rule message names the new site. The
  builder also updates that rule's clean probe snippet in
  `scripts/check_semgrep_probes.py` to the new constant, so the probe
  still proves the rule fires on a violation and stays silent on the
  allowlisted definition. `envelope` carries no literal after the
  alias change, so the rule stays silent there.
- `make mutation PKG=contextstate` runs; the builder commits
  `scripts/mutation_denylist/contextstate.json` holding the measured
  kill-rate floor with an empty denylist. Any denylist entry needs a
  one-line reason in the change. The `envelope` floor of 95 stays.
- Docs land with the code, builder-edited:
  `docs/packages/contextstate.md`, linked from `docs/README.md`;
  `docs/architecture.md` gains the `contextstate/` module-map bullet
  and updates the `envelope/` bullet for the delegation; `AGENTS.md`
  gains the `contextstate/` layout bullet.
- `go test -race ./contextstate/... ./envelope/... ./memory/...`
  passes, covering the store's concurrent paths and both delegated
  minters.
