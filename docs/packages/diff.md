# Package reference: diff

`diff` produces a bounded unified line diff between two byte slices,
so tool output that shows a file change never grows past a caller's
line budget. It is a leaf package: no third-party diff library, and
no internal import. The exported surface below mirrors
`api/diff.txt`.

## Functions

- `Unified(name string, a, b []byte, maxLines int) (string, error)` —
  computes a line-level diff between `a` and `b`, formatted as a
  standard unified diff (`--- name`, `+++ name`, `@@` hunk headers,
  ` `/`-`/`+` line prefixes). Identical input returns an empty string
  and a nil error.

## Invariants

- Line splitting treats `\n` as the line terminator; a final line
  without a trailing `\n` is a valid, unterminated last line, noted
  the way `diff -u` notes it.
- `maxLines` bounds the rendered output's line count, header lines
  and hunk lines together. `maxLines <= 0` means no bound; `Unified`
  never fails closed in that case. When the bound is exceeded,
  `Unified` returns `ErrTooLarge` instead of a truncated result.
- The match is a longest-common-subsequence line match, computed with
  a dynamic-programming table. Each maximal contiguous run of changed
  lines, plus up to three lines of unchanged context on each side
  (`N = 3`, matching GNU diff's default), becomes one hunk; hunks
  within `2*N` lines of each other merge into one.
- `Unified` has no input-size bound; a caller bounds `len(a)` and
  `len(b)` before calling, the division of labor
  `workspace.ReadFile` documents. `maxLines` bounds only rendered
  output, not the LCS table computation.

## Failure modes

Use `errors.Is` to test this case.

- `ErrTooLarge` ("diff: exceeds max lines") — `Unified` returns it,
  with an empty result string, when the rendered diff would exceed
  `maxLines`. Pinned by `diff/diff_test/diff_test.go`.

## Cross-references

None. `diff` declares no internal import edge and has no caller
inside this module yet.

## Wire contract

`Unified`'s output is the unified diff text format, not the envelope
wire form; no conformance vector applies.

## Usage

```go
package main

import (
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/diff"
)

func main() {
    a := []byte("one\ntwo\nthree\n")
    b := []byte("one\ntwo\nthree\nfour\n")

    out, err := diff.Unified("notes.txt", a, b, 50)
    if err != nil {
        panic(err)
    }
    fmt.Print(out)
}
```

### What the program shows

`Unified` diffs the two byte slices and prints a standard unified
diff: two header lines, one `@@` hunk header, three lines of
unchanged context, and one added line (`+four`).
