# Plan: test-tampering

Status: shipped, then collapsed. The original fourteen-rule,
thirteen-file design shipped in full; its detailed sections live in
git history. This file is the current design snapshot.

## Goal

Detect a change that makes the test suite ask less instead of making
the code pass more: a dropped test, a new skip, a net assertion
decrease, or a deleted conformance vector.

## Scope

Inside: `scripts/check_test_tampering.py` alone, its wiring in
`Makefile` and `.githooks/commit-msg`, and this plan. The gate is
diff-only. `.githooks/pre-commit` runs it in a sandboxed export with
no `.git`; there it prints a skip note. `.githooks/commit-msg` is the
real local gate: it runs `check_test_tampering.py --message-file
"$1"` against the real checkout, where the staged diff and the
drafted message both exist.

Outside: semantic weak-test detection (the mutation gate's job), an
LLM-judge step, read-only test trees, and any change to the coverage
floor.

Status: shipped.

## API

Command line:

```
check_test_tampering.py [--range REV_RANGE] [--message-file PATH] [--probe]
```

Four rules, chosen by waiver evidence over the commit history:

- `TT01` — a removed `Test`/`Benchmark`/`Fuzz`/`Example` function
  whose body hash reappears nowhere in the diff.
- `TT02` — a new `t.Skip`/`t.SkipNow`/`t.Skipf`/`testing.Short` call
  with no matching removal in the same hunk.
- `TT04` — a net assertion-site decrease, suppressed by a new test
  function or a helper-extraction signal.
- `TT09` — a conformance vector under a scoped `testdata/vectors/`
  directory (`envelope/`, `machine/`, `a2a/`, `mcp/`) deleted.

The former rules TT03, TT05-TT08, and TT10-TT14 are gone. TT06,
TT07, TT08, and TT10 never fired outside their own probes. TT05 and
TT03 fired rarely and review catches both. TT11-TT14 duplicated the
deps, api, and thirdparty gates, which already fail on policy drift.

One `Allow-Test-Change: TTxx <reason>` commit-message trailer waives
one finding. The reason has no word minimum, but a reason of only
boilerplate filler words waives nothing. No CLI flag or env var ever
waives a finding.

Diff resolution order: `--range`; the staged tree (plus merge
parents); a merge stand-down skip; the working tree against `HEAD`;
the first staged commit on a parentless repo; one comparison per
merge parent; then `HEAD~1 HEAD`.

Status: shipped.

## Tests

The gate's `--probe` self-test fires every rule on a violating diff
and requires silence on its clean counterpart: a dropped test, a
true move, a new skip, an assertion decrease and increase, a deleted
and a kept vector, and the trailer waiver paths including the
boilerplate-only rejection. `make verify` runs the probe.

Status: shipped.

## Verification

- `make verify-fast` runs the gate; `make verify` runs `--probe`.
- `.githooks/commit-msg` runs `--message-file` on every commit.
- The AGENTS.md enforcement-ladder entry names this gate and its
  four-rule scope.

Status: shipped.
