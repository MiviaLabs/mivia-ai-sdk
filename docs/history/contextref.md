# Plan: contextref

Status: superseded by `docs/history/context/ref.md`. The rename pass
moves the package to `context/ref`. This file is a historical record
of the shipped work.

## Goal

Provide the single canonical content reference minter and parser for the SDK.

## Scope

Inside:

- One new leaf package: `contextref`.
- Canonical hash prefix constant: `HashPrefix`.
- Canonical digest and ref minting functions: `Digest` and `Mint`.
- Canonical reference form check: `IsRef`.
- Standard library dependencies only (`crypto/sha256`, `encoding/hex`, `strings`).

Outside:

- Any payload storage or retrieval. `contextstate` owns payload records and `MemStore`.
- Any session or checkpoint lifecycle models. `contextsession` owns them.
- Any token budget compaction or elision logic. `contextplan` owns token budgeting.
- Any model provider messaging types. `envelope` and `provider` own message structures.

## API

The exported surface will land in `api/contextref.txt` via `make api-update`:

```go
package contextref

// HashPrefix prefixes every canonical content address.
const HashPrefix = "sha256:"

// Digest returns the SHA-256 of the concatenated chunks as 64 lowercase hex characters.
func Digest(chunks ...[]byte) string

// Mint returns HashPrefix plus Digest over the concatenated chunks.
func Mint(chunks ...[]byte) string

// IsRef reports whether ref matches HashPrefix followed by 64 lowercase hex characters.
func IsRef(ref string) bool
```

Design rationale:

- The minter requires no knowledge of sessions, records, or stores.
- Placing the minter in a leaf package removes heavy dependencies from callers that only need content addressing.
- `envelope`, `contextstate`, and `contextplan` can depend on `contextref` directly without cycles.

## Tests

Tests will live in `contextref/contextref_test/`:

- `ref_test.go`:
  - `TestMintDeterminism`: assert repeated `Mint` calls on equal input produce identical strings.
  - `TestMintChunkConcatenation`: assert `Mint(a, b)` equals `Mint(append(a, b...))`.
  - `TestDigestEmptyInput`: assert `Digest()` produces the known SHA-256 digest of empty bytes.
  - `TestIsRefValidationTable`: table-driven cases testing acceptance of valid canonical refs and rejection of invalid inputs.
- `ref_fuzz_test.go`:
  - `FuzzIsRef`: fuzz input strings to ensure `IsRef` never panics and rejects malformed formats.

## Verification

- `policy/layers.json` gains `"contextref": []`.
- `api/contextref.txt` is generated via `make api-update`.
- `python3 scripts/check_plan.py` passes.
- `python3 scripts/check_deps.py` passes.
- `python3 scripts/check_prose.py docs/history/contextref.md` passes.
- `python3 scripts/check_labels.py` passes.
- Unit tests pass with `go test -race ./contextref/...`.
- Test coverage for `contextref` reaches at least 85 percent.

### Semgrep alignment

The rule `sdk.go.hash-prefix-centralized` in `semgrep/sdk-standards.yml` will update its message to name `contextref/ref.go`.

## Addendum: export IsLowerHex

Status: shipped.

### Addendum goal

`envelope` carried its own private copy of the lowercase-hex scan
`IsRef` already uses internally, to validate `Signer` and `Signature`
fields (64 and 128 hex characters, not the 64-character content
digest `IsRef` checks). Export the existing scan instead of leaving
two copies in the tree.

### Addendum scope

Inside:

- Rename the unexported `isLowerHex` to `IsLowerHex` in
  `contextref/ref.go`; `IsRef` calls the exported form.
- Delete `envelope/message.go`'s private copy; `Message.Validate`
  calls `contextref.IsLowerHex` for the `Signer` and `Signature`
  checks.
- `api/contextref.txt` gains `func IsLowerHex(s string, n int) bool`
  through `make api-update`.

Outside:

- No change to `IsRef`'s behavior or wire form.

### Addendum tests

`contextref/contextref_test/ref_test.go`'s existing `IsRef` cases
exercise `IsLowerHex` transitively. `envelope`'s existing Signer and
Signature validation tests keep passing unchanged; they exercise the
same rule through the new call.

### Addendum verification

- `grep -rn "func isLowerHex" --include='*.go'` over the tree returns
  zero: one function, one name, one home.
- `make verify` passes.
