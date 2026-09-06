# Package: contextref

`contextref` provides the canonical content-reference minter and parser for the SDK.

## Purpose

The package offers a single definition of the content address format across the SDK.
It hashes byte slices using SHA-256 and validates formatted string addresses.
Callers like `contextstate`, `envelope`, and `contextplan` rely on `contextref`.

## API

```go
const HashPrefix = "sha256:"

func Digest(chunks ...[]byte) string
func Mint(chunks ...[]byte) string
func IsRef(ref string) bool
```

- `HashPrefix`: The canonical prefix string for references.
- `Digest`: Computes the SHA-256 digest over concatenated chunks.
- `Mint`: Returns `HashPrefix` concatenated with the hex digest.
- `IsRef`: Checks if a string conforms to `sha256:<64 lowercase hex characters>`.
