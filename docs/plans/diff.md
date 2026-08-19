# diff plan

## Goal

`diff` produces a bounded unified line diff between two byte slices,
so tool output that shows a file change never grows past a caller's
line budget.

## Scope

Inside:

- `Unified(name string, a, b []byte, maxLines int) (string, error)`:
  computes a line-level diff between `a` and `b`, formatted as a
  standard unified diff (`--- name`, `+++ name`, `@@` hunk headers,
  ` `/`-`/`+` line prefixes).
- Line splitting treats `\n` as the line terminator; a final line
  without a trailing `\n` is a valid, unterminated last line, and the
  diff notes it the way `diff -u` does.
- Bound enforcement: if the rendered diff would exceed `maxLines`
  lines (header lines and hunk lines together), `Unified` returns
  `("", ErrTooLarge)` instead of a truncated result. `maxLines <= 0`
  means no bound; `Unified` never fails closed in that case.
- Identical input (`a` equal to `b`) returns an empty string and a
  nil error.
- Algorithm: a longest-common-subsequence line match, computed with a
  dynamic-programming table over the two line slices, no third-party
  diff library. Each maximal contiguous run of changed lines, plus up
  to three lines of unchanged context on each side (`N = 3`, matching
  GNU diff's default), becomes one hunk; hunks within `2*N` lines of
  each other merge into one, matching GNU diff's default grouping.
- `Unified` has no input-size bound; a caller bounds `len(a)` and
  `len(b)` before calling, the same division of labor
  `workspace.ReadFile` documents. `maxLines` bounds only rendered
  output, not the LCS table computation.

Outside:

- Word-level or character-level diffs; the grain is always a line.
- Binary-content detection or refusal; a caller that hands binary
  data gets a diff of its bytes as if they were lines.
- Colorized or terminal-styled output; `Unified`'s output is plain
  text.

## API

- `var ErrTooLarge = errors.New("diff: exceeds max lines")`
- `func Unified(name string, a, b []byte, maxLines int) (string, error)`

## Tests

- Identical-input case returns `("", nil)`.
- Simple-change cases: an appended line, a removed line, a changed
  line in the middle, each checked against an exact expected unified
  diff string.
- No-trailing-newline case on one or both inputs, matching `diff -u`'s
  "no newline at end of file" convention.
- Bound cases: a diff that fits exactly at `maxLines` succeeds; one
  line over `maxLines` returns `ErrTooLarge`, checked with
  `errors.Is`; `maxLines` of zero and of a negative number both mean
  unbounded.
- Empty-input cases: both empty, one empty, and one from a single
  line to many lines.

## Verification

- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, and probes.
- Coverage for `diff` reaches the 85 percent floor.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass against this plan and the new `policy/layers.json` row.
