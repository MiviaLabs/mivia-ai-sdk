# Package reference: envfile

`envfile` loads a dotenv file into a map without ever leaking parsed
values into an error message. It is a leaf package: no I/O beyond
reading the one file, and no internal import. The exported surface
below mirrors `api/envfile.txt`.

## Functions

- `Load(path string) (map[string]string, error)` — reads the dotenv
  file at `path` and returns its `KEY=VALUE` pairs as a map. A blank
  line and a line starting with `#` (after leading whitespace) skip.
  A value may be unquoted, single-quoted, or double-quoted; a
  double-quoted value decodes `\n`, `\t`, `\\`, and `\"`. Only the
  first `=` on a line splits key from value, so a literal `=` in a
  value passes through unchanged. A duplicate key keeps its last
  value. `Load` returns a wrapped `os.ErrNotExist` when `path` does
  not exist.

## Invariants

- A key must match `[A-Za-z_][A-Za-z0-9_]*`; any other key is a parse
  error.
- A CRLF line ending parses the same as its LF equivalent; each
  line's trailing `\r` strips before parsing.
- No returned error contains a parsed value, quoted or unquoted. An
  error names only a line number and, where relevant, a key.

## Failure modes

Use `errors.Is` to test the missing-file case; the other cases return
plain errors whose text carries no parsed value.

- Missing file — `Load` wraps `os.ErrNotExist`. Pinned by
  `envfile/envfile_test/envfile_test.go`.
- Invalid key — a key outside `[A-Za-z_][A-Za-z0-9_]*` fails with a
  message naming the line only. Pinned by
  `envfile/envfile_test/envfile_test.go`.
- Unterminated quote — a quoted value with no matching close quote
  fails with a message naming the line only. Pinned by
  `envfile/envfile_test/envfile_test.go`.

## Cross-references

None. `envfile` declares no internal import edge and has no caller
inside this module yet.

## Wire contract

`envfile` parses a text file format, not the envelope wire form; no
conformance vector applies.

## Usage

```go
package main

import (
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/envfile"
)

func main() {
    values, err := envfile.Load(".env")
    if err != nil {
        panic(err)
    }
    fmt.Println(values["API_KEY"])
}
```

### What the program shows

`Load` reads `.env`, parses each `KEY=VALUE` line, and returns the
result as a map. The caller decides what to do with the values;
`envfile` never writes to `os.Environ` and never interprets a value
as a secret.
