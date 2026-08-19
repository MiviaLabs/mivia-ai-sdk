# secretpath plan

## Goal

`secretpath` matches a filesystem path against a configured list of
glob-style secret path patterns, so a caller can decide whether a path
holds sensitive content before it reads, writes, or logs it.

## Scope

Inside:

- `NewMatcher(patterns []string) (*Matcher, error)`: compiles a
  pattern list once. An invalid glob returns an error naming the bad
  pattern's index.
- `(*Matcher) Matches(path string) bool`: reports whether `path`
  matches the compiled pattern set.
- Pattern syntax: `path.Match`-style globs (`*`, `?`, `[...]`) plus
  two repo conventions: a pattern ending in `/` matches the directory
  and everything under it; a pattern starting with `!` negates a
  later match, so a path matching a negation pattern that sorts after
  its positive match returns `false`. Patterns apply in list order;
  the last matching pattern (positive or negated) wins.
- Path normalization: `Matches` cleans the input with `path.Clean`
  and treats `\` the same as `/` before matching, so the caller need
  not normalize first.

Outside:

- Reading the filesystem. `Matches` is a pure string decision; it
  never opens or stats a path.
- Redaction or masking of matched content. That is app-side policy,
  per the phase doc.
- Default pattern lists. The caller supplies every pattern; the
  package ships no built-in secret list.

## API

- `type Matcher struct { ... }` (fields unexported)
- `func NewMatcher(patterns []string) (*Matcher, error)`
- `func (m *Matcher) Matches(path string) bool`

## Tests

- Table-driven glob cases: exact file match, `*` wildcard, `?`
  wildcard, character class, no match.
- Directory-pattern cases: a pattern ending in `/` matches a file two
  levels under that directory and does not match a sibling directory
  with a shared prefix.
- Negation cases: a positive pattern followed by a `!`-negated
  pattern for one file inside it flips that one file back to
  non-secret; a negation pattern before its positive counterpart in
  list order has no effect, proving order matters; a directory
  pattern ending in `/` followed by a `!`-negated pattern for one file
  under it flips that one file back to non-secret while its siblings
  stay secret; a four-pattern list (positive directory, negated file,
  positive file, negated same file) proves the last matching pattern
  wins across repeated flips.
- Normalization cases: a path with `./`, `..`-free redundant
  separators, and backslash separators all match the same as their
  cleaned form.
- Invalid-pattern case: `NewMatcher` with an unbalanced `[` returns an
  error naming the pattern's index, and `Matches` is never called on
  a `nil` `*Matcher`.

## Verification

- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, and probes.
- Coverage for `secretpath` reaches the 85 percent floor.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass against this plan and the new `policy/layers.json` row.
