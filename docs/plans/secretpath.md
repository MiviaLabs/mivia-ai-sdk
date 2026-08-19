# secretpath plan

## Goal

`secretpath` matches a filesystem path against a configured list of
glob-style secret path patterns, so a caller can decide whether a path
holds sensitive content before it reads, writes, or logs it.

The open change fixes one fail-open defect: a directory pattern is
matched as a literal string prefix, so its glob metacharacters are
ignored. `NewMatcher([]string{"secret*/"})` does not match
`secrets/key.pem` today, though the plan and the package reference
both promise glob syntax for every pattern.

## Scope

Inside:

- `NewMatcher(patterns []string) (*Matcher, error)`: compiles a
  pattern list once. An invalid glob returns an error naming the bad
  pattern's index.
- `(*Matcher) Matches(path string) bool`: reports whether `path`
  matches the compiled pattern set.
- Pattern syntax: `path.Match`-style globs (`*`, `?`, `[...]`, `\`) plus
  two repo conventions: a pattern ending in `/` matches the directory
  and everything under it; a pattern starting with `!` negates a
  later match, so a path matching a negation pattern that sorts after
  its positive match returns `false`. Patterns apply in list order;
  the last matching pattern (positive or negated) wins.
- Glob syntax applies to a directory pattern's body too. The two
  conventions compose; neither one disables the other.
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
- A `**` operator that spans path separators. `path.Match` has none,
  and this package adds none.
- Any-depth matching. A pattern anchors at the start of the cleaned
  path. It is not a gitignore-style any-depth pattern, so `secrets/`
  does not deny `vendor/secrets/key.pem`. This limit is unchanged by
  the fix and stays out of scope.

### The decision

Two options existed for the defect.

- Option A, chosen: make a directory pattern glob-aware, so the code
  matches the documented contract.
- Option B, rejected: document a directory pattern as literal, and
  correct both the plan and the package reference.

Option A wins for three reasons. The docs already promise glob syntax
for every pattern. A caller who denies `secret*/` reasonably believes
the whole tree is protected. Fail-open is the wrong default in a
secret matcher, and Option B keeps the hole.

Option A carries no ambiguity, because the rules below pin every case.
The rule set reuses `path.Match` and adds no new operator.

### Directory pattern semantics

Let `body` be the pattern text after the leading `!` is stripped and
the trailing `/` is trimmed. Let `clean` be the input after
`Matches` normalizes it.

The ancestor list of `clean` is every prefix `clean[:i]` where `i` is
greater than zero, and where `i` equals `len(clean)` or `clean[i]` is
`/`. The ancestor list of `a/b/c` is `a`, `a/b`, `a/b/c`. The
ancestor list of `/etc/passwd` is `/etc` and `/etc/passwd`.

The index `i` starts at one, never at zero. A start of zero adds an
empty ancestor for an absolute path, and `path.Match("*", "")` is
true. That mutant makes `*/` match every absolute path, which is
fail-open under negation. Two planned tests kill it.

A directory pattern matches `clean` when `path.Match(body, ancestor)`
reports true for at least one ancestor. The builder walks the
ancestor list and returns true on the first match.

The walk indexes `clean` in place. It must not build a `[]string` of
ancestors, because `workspace.Options.Deny` will call `Matches` on
every file operation.

The predicate inside the walk has this exact shape, one statement and
no dead branch:

```go
if ok, err := path.Match(body, clean[:i]); err == nil && ok {
    return true
}
```

A `path.Match` error is therefore no match, and the walk continues.
This mirrors the non-directory branch. `compilePattern` already
rejects a bad body, so the error is unreachable in practice. An
explicit error branch would be an uncoverable statement, so the plan
forbids one.

The consequences, each one binding:

- `*` never crosses `/`, because `path.Match` never crosses `/`. This
  matches the non-directory branch, so one rule covers the package.
- `secret*/` matches `secrets`, `secrets/key.pem`, and
  `secrets/a/b.txt`. It does not match `public/secrets/key.pem`.
- `a/*/keys/` matches `a/x/keys` and `a/x/keys/deep/file.txt`. It
  does not match `a/keys`, and it does not match `a/x/y/keys/f`.
- A directory pattern still matches the directory itself, because the
  full `clean` is its own last ancestor. `secrets/` matches `secrets`
  and `secrets/anything`, as it does today.
- `*/` has the body `*`. It matches every relative path with at least
  one segment, and it matches the first segment of a deeper path. It
  does not match an absolute path, because the first ancestor of
  `/etc/passwd` is `/etc`, and `*` never crosses `/`.
- `*/` and `?/` now match the empty input, because `path.Clean("")`
  returns `.`, and `.` is its own ancestor. The old promise that no
  path matches an empty input was pattern-dependent already; it is now
  false for a one-character glob body.
- `secret*/` now matches the sibling directory `secrets-other/f`.
  That is correct glob behavior, not a defect. A metacharacter-free
  body still needs a segment boundary, so `secrets/` still does not
  match `secrets-other/f`.
- `/` has the empty body. It keeps its current meaning: it matches
  every path that starts with `/`. The builder special-cases an empty
  body before the ancestor walk, because no ancestor is ever empty.
- A `\` in a body is a `path.Match` escape character. A directory body
  holding `\` changes from never-match to escape-aware match. For
  example, `sec\rets/` matches nothing today and matches
  `secrets/key.pem` after the fix. This widens denial only.
- Matching runs against ancestor prefixes of the whole cleaned path,
  not against single segments. A multi-segment body therefore anchors
  at the start of the path, as a non-directory pattern does.
- `../secrets/key.pem` still does not match `secrets/`, because no
  ancestor of it equals `secrets`.

The negation and order rules are unchanged. `Matches` keeps its
loop: it walks the compiled list in order, and the last matching
pattern decides. Only the per-pattern predicate changes, so `!` and
last-match-wins compose with the new directory rule without a change.
`!secret*/` now clears a whole tree, which is the mirror of the fix.

`NewMatcher` validation needs no change. `compilePattern` already
validates `body` with `path.Match` after it trims the trailing `/`,
so a directory pattern's body is already checked as a glob. The plan
adds a test that pins this, because the property is now load-bearing.

For a body with no `path.Match` metacharacter the new rule is equal to
the old prefix test. The metacharacter set is `*`, `?`, `[`, and `\`.
Such a body matches an ancestor only when the two strings are equal.
That ancestor exists only when the body is a path prefix on a segment
boundary. Every existing test uses a metacharacter-free directory
body, so no existing verdict changes.

## API

- `type Matcher struct { ... }` (fields unexported)
- `func NewMatcher(patterns []string) (*Matcher, error)`
- `func (m *Matcher) Matches(path string) bool`

The exported surface does not change. `api/secretpath.txt` stays as
it is, and `make api-update` must produce no diff.

`policy/layers.json` does not change. The row `"secretpath": []`
already exists, and the package stays a leaf with no internal import.

## Tests

Existing cases stay. New cases go in
`secretpath/secretpath_test/secretpath_test.go`.

- Reproduction case: patterns `["secret*/"]`, path `secrets/key.pem`,
  want true. Kills the literal-prefix mutation in the directory
  branch.
- Directory-itself case: patterns `["secret*/"]`, path `secrets`, want
  true. Kills a mutation that drops the full path from the ancestor
  list.
- Multi-segment case: patterns `["a/*/keys/"]`. Want true for
  `a/x/keys` and `a/x/keys/deep/file.txt`. Want false for `a/keys`.
  Kills a mutation that matches a body against one segment only.
- Separator case: patterns `["a/*/keys/"]`, path `a/x/y/keys/f`, want
  false. Kills a mutation that lets `*` cross `/`.
- Prefix-anchor case: patterns `["secret*/"]`, path
  `public/secrets/key.pem`, want false. Kills a mutation that scans
  suffixes as well as prefixes.
- Sibling case, already present: patterns `["secrets/"]`, path
  `secrets-other/file.txt`, want false. Kills a mutation that
  compares raw string prefixes without a segment boundary. Reword its
  comment: a metacharacter-free body still needs a segment boundary.
  The old wording claimed a general shared-prefix guarantee, and
  `secret*/` no longer keeps one.
- Root-pattern case: patterns `["/"]`, path `/etc/passwd`, want true.
  Kills a mutation that drops the empty-body special case.
- Star-directory case: patterns `["*/"]`, paths `a` and `a/b/c`, want
  true. Kills a mutation that requires two or more segments.
- Absolute over-match case: patterns `["*/"]`, path `/etc/passwd`,
  want false. Kills the ancestor off-by-one, where the walk starts at
  index zero and adds an empty first ancestor.
- Absolute negation case: patterns `["/etc/", "!*/"]`, path
  `/etc/passwd`, want true. Kills the same off-by-one on its
  fail-open side, where the negation clears every absolute path.
- Absolute walk case: patterns `["/etc*/"]`. Want true for
  `/etc/passwd` and `/etcx`, and false for `/opt/etc/passwd`. Kills a
  mutation that skips the walk for a `/`-rooted input. The `/etcx`
  verdict is true by the directory-itself rule, because the glob body
  matches the directory name.
- Escape case: patterns `["sec\\rets/"]`, path `secrets/key.pem`,
  want true. This pins the documented widening, because the pattern
  matches nothing before the fix. It does not kill the strip mutation,
  because stripping `\` from `sec\rets` yields the same verdict. The
  discriminating input is patterns `["sec\\*ets/"]` with path
  `secXets/f`, want false: an escape-aware body reads `\*` as a
  literal asterisk, and the strip mutation reads it as a wildcard. A
  third input, path `sec*ets/f`, want true, pins the literal side.
- Negation case: patterns `["secret*/", "!secret*/public.txt"]`. Want
  false for `secrets/public.txt`, and true for `secrets/key.pem`.
  Kills a mutation that applies the new rule to positive patterns
  only.
- Invalid directory glob case: `NewMatcher([]string{"bad[/"})`
  returns an error naming pattern index zero. Kills a mutation that
  skips glob validation when the pattern ends in `/`.

Fuzz: `FuzzMatches` stays. Add five seed corpus entries, so the
corpus covers the new branch: (`secret*/`, `secrets/key.pem`),
(`a/*/keys/`, `a/x/keys/f`), (`*/`, `a/b`), (`/`, `/etc/passwd`), and
(`*/`, `/etc/passwd`).

The new code path cannot panic. It calls `path.Match` and string
slicing only. `path.Match` returns `ErrBadPattern` for a malformed
pattern and never panics on any input. The ancestor walk slices the
cleaned path at byte offsets it found in that same string, so no
index is out of range.

## Blast radius

- No production caller of `secretpath` exists in this module. The
  grep over `*.go` finds the package itself and its test only.
- `secretpath/secretpath_test/` is the only affected code. Every
  existing case keeps its result, because each directory pattern in
  it has a metacharacter-free body.
- `docs/packages/secretpath.md` needs no wording change. Its
  Invariants section already states the contract this fix delivers.
- `docs/plans/workspace.md` change two wires `secretpath` into
  `Workspace` as a deny policy, through `Options.Deny`. That wiring
  is not built yet. This fix must land before it, so the fail-open
  hole does not reach the file tools.
- `docs/plans/secrets.md` states a `secretpath` delta of none. That
  stays true, because the exported surface is unchanged.

## Verification

- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, and probes.
- Coverage for `secretpath` reaches the 85 percent floor.
- `make api-update` produces no diff in `api/secretpath.txt`.
- `python3 scripts/check_plan.py`, `python3 scripts/check_deps.py`,
  `python3 scripts/check_prose.py`, and
  `python3 scripts/check_labels.py` pass.
- `go test -race ./secretpath/...` passes.
- `python3 scripts/check_mutation.py --pkg secretpath` runs, and the
  kill rate is 100 percent. The Tests section justifies each case by
  the mutation it kills, so the kit must score the claim. The package
  has no denylist file, so the run is exploratory until the builder
  adds `scripts/mutation_denylist/secretpath.json` with an empty
  denylist and a floor of 100. Add that file in the same change.
- `go test -run FuzzMatches ./secretpath/secretpath_test/` passes on
  the extended seed corpus.
