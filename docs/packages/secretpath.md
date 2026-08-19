# Package reference: secretpath

`secretpath` matches a filesystem path against a configured list of
glob-style secret path patterns, so a caller can decide whether a
path holds sensitive content before it reads, writes, or logs it. It
is a leaf package: `Matches` is a pure string decision and never
touches the filesystem. The exported surface below mirrors
`api/secretpath.txt`.

## Types

- `Matcher` — holds a compiled, ordered list of secret path patterns.
  Fields are unexported; build one with `NewMatcher`.

## Functions and methods

- `NewMatcher(patterns []string) (*Matcher, error)` — compiles
  `patterns` once. An invalid glob returns an error naming the
  pattern's index.
- `Matcher.Matches(path string) bool` — reports whether `path`
  matches the compiled pattern set. A nil `*Matcher` holds no pattern,
  so it matches nothing and returns false.

## Invariants

- A pattern follows `path.Match`-style glob syntax (`*`, `?`,
  `[...]`), plus two repo conventions: a pattern ending in `/` matches
  the directory and everything under it, and a pattern starting with
  `!` negates a later match.
- Patterns apply in list order; the last matching pattern, positive or
  negated, decides the result.
- `Matches` cleans the input with `path.Clean` and treats `\` the
  same as `/` before matching, so the caller need not normalize
  first.
- `Matches` on a nil `*Matcher` returns false and does not panic. A
  nil `Matcher` matching nothing is what a nil `workspace.Options.Deny`
  already means.

## Failure modes

`NewMatcher` returns a plain error; a caller cannot match it with
`errors.Is`.

- Invalid pattern — an unbalanced glob, for example an unclosed `[`,
  fails with a message naming the pattern's index. Pinned by
  `secretpath/secretpath_test/secretpath_test.go`.

## Cross-references

- `workspace` — `workspace.Options.Deny` is a `*secretpath.Matcher`.
  `workspace` consults it before every filesystem call and returns
  `workspace.ErrSecretPath` on a match.

`secretpath` declares no internal import edge of its own.

## Wire contract

`secretpath` defines no wire format; it matches plain strings. No
conformance vector applies.

## Usage

```go
package main

import (
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/secretpath"
)

func main() {
    m, err := secretpath.NewMatcher([]string{"secrets/", "!secrets/readme.txt"})
    if err != nil {
        panic(err)
    }
    fmt.Println(m.Matches("secrets/api.key"))
    fmt.Println(m.Matches("secrets/readme.txt"))
}
```

### What the program shows

The first pattern marks everything under `secrets/` as sensitive; the
second negates one file back to non-sensitive. The program prints
`true` for `secrets/api.key` and `false` for `secrets/readme.txt`.
