# Plan: contextref

Status: shipped.

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
- `python3 scripts/check_prose.py docs/plans/contextref.md` passes.
- `python3 scripts/check_labels.py` passes.
- Unit tests pass with `go test -race ./contextref/...`.
- Test coverage for `contextref` reaches at least 85 percent.

### Semgrep alignment

The rule `sdk.go.hash-prefix-centralized` in `semgrep/sdk-standards.yml` will update its message to name `contextref/ref.go`.
