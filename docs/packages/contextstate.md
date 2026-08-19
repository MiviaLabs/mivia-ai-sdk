# Package reference: contextstate

`contextstate` holds the durable context contract and the single
canonical content-reference minter. The minter turns bytes into a
`sha256:`-prefixed ref. The contract types describe sessions,
checkpoints, commits, and payloads. The `MemStore` applies commits in
memory under caller-owned volume bounds. The exported surface below
mirrors `api/contextstate.txt`.

## The minter

- `HashPrefix` — the `"sha256:"` prefix of every canonical ref. The
  literal appears here only; Semgrep enforces this.
- `Digest(chunks ...[]byte) string` — the SHA-256 of the ordered
  concatenation of the chunks, as 64 lowercase hex characters.
- `Mint(chunks ...[]byte) string` — `HashPrefix` plus `Digest`. This
  is the canonical ref string. Minting over the concatenation of
  chunks equals minting over the concatenated bytes.
- `IsRef(ref string) bool` — reports whether `ref` is the canonical
  form: `HashPrefix`, then exactly 64 lowercase hex characters, and
  nothing else.

`envelope.ContextRef` delegates to `Mint`, and `memory` delegates
transitively through `envelope`. Every ref in this SDK therefore has
one form. Pin this with a conformance test before you change the
minter.

## Types

- `ContentRef` — the address of one shared blob. `Ref` is the
  address a caller hands around. `SHA256` is the bare digest.
  `Size` is the whole-payload byte count. `Namespace` and the three
  owner strings are caller-owned validated identifiers.
- `NewContentRef` — mints `Ref`, `SHA256`, and `Size` over the
  concatenated chunks, then validates the result.
- `PayloadRecord` — one payload under its `ContentRef`, with a
  `RetentionClass` and an optional `Data`.
- `RetentionClass` — labels how long a payload is kept.
  `RetentionSession` and `RetentionCompliance` ship. Any non-empty
  class validates, so a caller may define its own.
- `Reassemble` — concatenates chunks in order under one ref and
  returns the record with `Data` set. It fails closed on a size or
  digest mismatch.
- `SourceID`, `SourceRange`, `SourceEvent` — the session's event log:
  one id, an inclusive span of one session, and one event.
- `Revision`, `BindingRevision` — the session's counters and its
  provider-model binding.
- `CheckpointID`, `Checkpoint` — one committed state and its identity.
- `Session` — the read model: revision, binding, active checkpoint,
  and the event log.
- `CommitRequest` — one atomic advance: events, payloads, and the new
  checkpoint under an idempotent `OperationID`.
- `NewCommitRequest` — sets `OperationID` from the checkpoint's
  `IdempotencyKey` and derives the new revision fields, so the
  request is complete at construction.
- `Limits` — the volume bounds: `CheckpointBytes`, `CommitEvents`,
  `CommitEventBytes`.

## Methods

- `ContentRef.Validate` — enforces `IsRef(Ref)`, that `Ref` equals
  `HashPrefix` plus `SHA256`, the identifier bounds, and a
  non-negative `Size`.
- `PayloadRecord.Validate` — enforces a valid ref and a non-empty
  retention class. When `Data` is present, its length must equal
  `Ref.Size` and its digest must equal `Ref.SHA256`.
- `CommitRequest.Validate` — enforces shape only, in order:
  identity, revision, events, payloads, checkpoint.
- `Limits.Validate` — rejects a negative field and names it. Zero
  means uncapped.
- `MemStore.New(limits)` — builds the store and validates `limits`.
  The zero value is not usable.
- `MemStore.Put` — validates and stores a copy under `Ref.Ref`.
  Content-addressed, so a repeat `Put` overwrites in place.
- `MemStore.Get` — returns a copy; an unknown ref wraps
  `ErrPayloadNotFound`.
- `MemStore.Checkpoint` — answers the operation key first. A reused
  `OperationID` with an equal request is a no-op success. A different
  request wraps `ErrCheckpointConflict` before any other check. A new
  key then validates, enforces the volume bounds, and checks the
  stale guards.
- `MemStore.Session` — returns the revision, the binding, the active
  checkpoint, and a copy of the events.

## Invariants

- The store detects a reused operation key by comparing requests
  structurally, field by field.
- An unknown session commits only against a zero `Expected`.
- Volume bounds reject at write time and store nothing. A zero bound
  is uncapped.
- Shape bounds stay compiled in: identifiers at
  `MaxIdentifierBytes`, payload references at
  `MaxPayloadReferenceBytes`, source ranges at
  `MaxSourceRangeEvents`.

## Failure modes

Match these with `errors.Is`.

- `ErrInvalidRecord` — wraps every validation failure. A
  `ValidationError` names the offending field and unwraps into this
  sentinel.
- `ErrSessionNotFound` — `Session` read of an unknown id.
- `ErrStaleRevision` — a commit against a moved revision, or a first
  commit with a non-zero `Expected`.
- `ErrStaleBinding` — a commit against a moved binding.
- `ErrCheckpointConflict` — a reused `OperationID` with a different
  request.
- `ErrPayloadNotFound` — `Get` of an unknown ref.
- `ErrOverLimit` — a commit that breaks an enabled volume bound.

## Cross-references

- [envelope.md](envelope.md) — `ContextRef` delegates to `Mint`.
- [memory.md](memory.md) — the store's `Put` mints refs through
  `envelope.ContextRef`.
- [contextbudget.md](contextbudget.md) — the same zero-means-uncapped
  limits rule, for one model call's context.

## Usage

```go
ref, err := contextstate.NewContentRef("app.namespace", "ws", "session", "subject", data)
if err != nil {
    // a validation failure
}
store, err := contextstate.New(contextstate.Limits{CommitEvents: 100})
if err != nil {
    // a negative limit
}
req, err := contextstate.NewCommitRequest(session, expected, binding, events, payloads, checkpoint, newBinding, turn)
if err != nil {
    // a validation failure
}
if err := store.Checkpoint(req); err != nil {
    // conflicts, stale guards, or volume bounds
}
snapshot, err := store.Session(session)
```
