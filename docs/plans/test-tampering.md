# Plan: test-tampering

## Goal

Add `scripts/check_test_tampering.py`, a diff-only gate that detects when
a change makes the test suite ask less, instead of making the code pass
more. Close the gap the 2026-08 audit found: gates enforce shape, not
logic, and an agent that cannot fix a test can silence it instead.

## Scope

Inside: the gate script and its helper modules under `scripts/`, the
override-trailer scheme, the `--probe` self-test suite, wiring
instructions for `Makefile` and `.githooks/`, an `AGENTS.md`
enforcement-ladder entry, and this plan.

Outside: semantic weak-test detection (the mutation gate's job), an
LLM-judge step, read-only test trees, and any change to the coverage
floor. The planner does not edit `Makefile`, `AGENTS.md`, `.githooks/`,
or `scripts/`; the sections below are instructions for the builder.

### Why this needs two git hooks, not one

`.githooks/pre-commit` runs `make verify-fast` inside a `git archive`
export with no `.git` directory. A diff-only gate cannot compute a diff
there; it has no repository to diff against.

Git also runs `pre-commit` before it captures the commit message, even
with `git commit -m`. A hook that decides overrides needs both the diff
and the message at once. Only `commit-msg` has both: git passes it a
path to the drafted message, and the index has not changed since
`pre-commit` ran.

The design follows from these two facts:

- `pre-commit` (existing, unchanged) keeps running `verify-fast` in its
  sandboxed export. `check_test_tampering.py` runs there too, but finds
  no `.git` and skips itself with a printed note. This is not a bypass;
  the same commit still goes through `commit-msg` before it exists.
- A new `.githooks/commit-msg` hook is the real local gate. It runs
  `check_test_tampering.py --message-file "$1"` against the real
  repository, where the staged diff and the drafted message both exist.
  `make install-hooks` needs no change; git already discovers any file
  under the configured `core.hooksPath`.
- `make verify-fast` also gets a direct call to the script (see
  Verification), so a plain `make verify-fast` or `make verify` from a
  real checkout (CI, or a developer's own repo root) still runs the
  check even outside a commit.

### Diff resolution order

`check_test_tampering.py` picks its diff in this order:

1. `--range REV_RANGE` given: diff that range with `git diff REV_RANGE`.
   The message source is the tip commit's own message
   (`git log -1 --format=%B <tip>`); the trailers already sit in git log.
2. No `--range`, and something is staged (`git diff --cached --quiet`
   exits non-zero): diff the staged tree. The message source is
   `--message-file PATH` if given, else none.
3. No `--range`, and nothing is staged: diff `HEAD~1...HEAD`. The
   message source is `HEAD`'s own commit message. This is the case a
   clean CI checkout hits, so `make verify` in CI checks the tip commit
   for real, with its trailers already final.
4. No `.git` found at all (the `pre-commit` sandbox): print a skip note
   and exit 0.
5. No parent commit (`HEAD~1` does not resolve, the first commit in the
   repository): print a skip note and exit 0.

A staged diff with no `--message-file` can never honor an override; no
message exists yet at that point. Any finding there blocks. This is
deliberate: the real, override-capable gate is `commit-msg`.

The `HEAD~1...HEAD` fallback checks the tip commit against its
immediate parent only. A multi-commit push can carry a tampering
finding in an earlier commit and land a clean tip; this fallback would
miss it. This is an accepted limit, not a gap the plan closes: CI has
no branch protection yet and stays informational, per the CI note
under Verification. Every commit in the push already went through its
own `commit-msg` check locally, one commit at a time, when it was made
on a machine with these hooks installed.

### Detections and finding IDs

Every finding carries a stable two-letter, two-digit ID: `TT01`
through `TT14`. The prefix keeps the ID out of the audit-finding-label
pattern (a single letter A through G followed by a digit); `TT` starts
with a letter outside that range and the whole token has no internal
word boundary the label pattern could match.

Test-file detections, `*_test.go` only:

- `TT01`, function moved or dropped. Build a normalized-body hash for
  every removed and added `Test`/`Benchmark`/`Fuzz`/`Example` function.
  A removed body whose hash reappears anywhere in the diff is a move,
  not a violation. A removed body with no matching hash, or one whose
  name reappears without matching the collection-name regex
  (`^(Test|Benchmark|Fuzz|Example)([A-Z0-9_]|$)`), is `TT01`.
- `TT02`, a skip added: a new `t.Skip(`, `t.SkipNow(`, `t.Skipf(`, or a
  new `testing.Short()` guard, with no matching removal in the same
  hunk.
- `TT03`, a `//go:build` line added to a test file that had none, or an
  existing `//go:build` line's expression changed.
- `TT04`, net decrease in assertion sites (`t.Error(`, `t.Errorf(`,
  `t.Fatal(`, `t.Fatalf(`) counted across the whole diff. Suppressed
  when the diff also adds a new test function, or a new call ending in
  `Cases(`, `Suite(`, or `RunAll(`, or a call starting with `assert` —
  each is a helper-extraction signal. Tuned to miss rather than
  false-positive, per the directive.
- `TT05`, a removed comparison against a non-error operand
  (`if x != y` or `if x == y` where neither side is `err`), replaced in
  the same hunk by nothing but a bare `if err != nil` or `if err == nil`.
- `TT06`, a call whose result a removed line captured and checked
  (`v, err := f()` plus an `err` check) now discarded with `_ = f()` or
  `_, _ = f()` in the same hunk.
- `TT07`, a numeric literal near `time.Sleep(`, `time.After(`,
  `Timeout`, `Retries`, `Tolerance`, or `Delta` that is strictly larger
  in the added line than the removed line at the same position.
- `TT08`, a removed `t.Parallel()` call with no matching addition in the
  same function.

`testdata/vectors/` detections, scoped to `envelope/`, `machine/`,
`a2a/`, and `mcp/`, matching where conformance vectors live today:

- `TT09`, a file under a `testdata/vectors/` directory deleted.
- `TT10`, a file under a `testdata/vectors/` directory modified in
  place. A new file added alongside an unmodified old one is not a
  finding.

Gate-infrastructure detections, tree-wide:

- `TT11`, the self-reference guard. Any change under `scripts/`,
  `Makefile`, `.githooks/`, `policy/`, `semgrep/`, or
  `.github/workflows/`, present in the same diff as a change to any
  other, non-doc-companion file, fires `TT11`. The doc-companion
  exception is a later addendum; see "TT11 doc companions" below.
  "Any other file" means any path outside the infra list, of any
  kind: a `_test.go` file, a `testdata/` vector, a non-test `*.go`
  file, or a doc outside the doc-companion set. The attack this guard
  defends against lands in `_test.go` and `testdata/vectors/`, the
  same places `TT01`–`TT10` look; a rule that only paired gate-infra
  changes with non-test Go changes would miss the exact attack the
  plan exists to catch. `TT11` fires even when nothing else does,
  except the doc-companion-only case below.
- `TT12`, a new entry added to a `scripts/mutation_denylist/*.json`
  file's `denylist` array, or a new file added under that directory.
- `TT13`, a weakened floor: `COVERAGE_FLOOR` in `Makefile` lowered, a
  `floor` value in a `scripts/mutation_denylist/*.json` file lowered,
  or a `python3 scripts/check_*.py` invocation line removed from the
  `verify-fast` or `verify` recipes in `Makefile`. The removed-line
  check fires on its own, without needing a co-occurring change to any
  other file; deleting a gate's invocation line permanently uninstalls
  it, which is a stronger act than lowering a number and needs no
  paired file to be worth flagging. This makes `TT13` a backstop for
  the one case `TT11` cannot reach on its own: a diff that touches only
  `Makefile` and nothing else.
- `TT14`, self-preservation of the hook and the checker. `TT11` only
  fires when a gate-infra change is paired with a change to some other
  file. A single-purpose commit that touches only
  `.githooks/commit-msg`, `scripts/check_test_tampering.py`, or a
  `scripts/test_tampering_*.py` file, and nothing else, would clear
  `TT11` while disabling the mechanism that catches every later
  tampering commit. `TT14` closes that path with the same
  standalone-backstop pattern `TT13` already uses for the Makefile
  invocation line, generalized to the hook and to the checker's own
  source. It fires, with no co-occurring file required, on any of:
  - `.githooks/commit-msg` deleted, or its line invoking
    `check_test_tampering.py --message-file` removed, commented out, or
    changed to name a different script or drop the flag.
  - `scripts/check_test_tampering.py` or any `scripts/test_tampering_*.py`
    file deleted.
  - A `TTnn` ID string literal present in the old blob of one of those
    files and absent from the new blob of the same file set: a rule's
    own ID vanished from the source responsible for emitting it.
  - Unreachable code after a `return`, found in the new blob alone,
    inside a rule function's `def` block: a `return` statement
    followed by one or more non-blank, non-comment lines at the same
    or a deeper indent level of the same block, with no intervening
    line that opens a new branch (`if`, `elif`, or `else`) immediately
    after the `return`. The check needs no notion of "old" or
    "changed"; a line is unreachable the moment a `return` sits above
    it at the same block level, whether that line is brand new or
    untouched since the last commit. This is the same idea as `go
    vet`'s unreachable-code check, applied to the checker's own Python
    sources by indentation and keyword matching, not by an AST. It
    replaces an earlier line-survival design that compared the new
    blob against the old one: an attacker could keep that rule silent
    by pairing the early `return` with a trivial edit below it
    (reformatting, a renamed local, a split expression), since no old
    line would then match byte-for-byte. Reading the new blob alone
    closes that path, because reformatted dead code is still dead
    code. It also stops firing on a legitimate rewrite, since deleting
    or restructuring the lines after a new early return, into a live
    branch or out of the function, leaves nothing unreachable to flag.
  `TT14` reuses `TT11`'s reasoning past the infra-plus-other-file case:
  a change that only ever touches the enforcement mechanism itself, and
  nothing it enforces against, is exactly the diff a self-reference
  guard exists to catch, whether or not another file rides along.

### TT11 doc companions (addendum)

AGENTS.md's own enforcement ladder requires several gate-infra changes
to land with a matching doc update in the same commit: "Do not land a
package without `docs/plans/<pkg>.md` ... same change," and similar
rules elsewhere. Before this addendum, that mandated pairing always
tripped `TT11` on itself, forcing a manual `Allow-Gate-Change`
override on the single most common legitimate gate-infra change: a
script fix landed with its required plan-doc update. This addendum is
a deliberate, user-approved narrowing of the guard. It targets one
specific laundering path. It does not weaken `TT11` against gate
infra paired with real code.

A doc companion is one of three paths: `AGENTS.md` exactly, a `.md`
file under `docs/plans/`, or a `.md` file under `docs/packages/`.
`check_self_reference_guard` gains a helper, `_is_doc_companion(path)`,
returning true for exactly these three cases. The `AGENTS.md`-exact
branch stays an exact string match, already as narrow as possible.
The `docs/plans/` and `docs/packages/` branches each require both the
path prefix and `path.endswith(".md")`; a `.go` file placed under
either tree fails the extension check and is not a doc companion. The
rule then computes two sets from the diff's non-infra paths: `other`,
every non-infra path as before, and `non_doc_other`, the subset of
`other` excluding doc companions. `TT11` fires only when both `infra`
and `non_doc_other` are non-empty.

This keeps three behaviors distinct:

- Gate infra paired only with `AGENTS.md`: silent. `non_doc_other` is
  empty.
- Gate infra paired only with one or more `docs/plans/*.md` or
  `docs/packages/*.md` files: silent. Same reason.
- Gate infra paired with a real code file — any path
  `_is_doc_companion` does not match, including a `.go` file anywhere,
  a `.md` file outside `docs/plans/` and `docs/packages/`, or a `.go`
  file placed inside `docs/plans/` or `docs/packages/` — whether or
  not a doc companion also rides along in the same diff: fires. The
  `non_doc_other` split is path-only: a path either matches one of
  `_is_doc_companion`'s three cases or it counts as code. There is no
  separate extension-based classification beyond the `.md` check
  already inside `_is_doc_companion`. A doc companion never launders a
  real code change; `non_doc_other` still contains the code file, so
  `TT11` still fires.

`scripts/test_tampering_rules_infra.py` must record this reasoning as
a comment next to `_is_gate_infra` and `_is_doc_companion`, per
AGENTS.md's own instruction to record an approved gate exception
directly in the gate file. The comment states the narrowing is
user-approved and names the laundering path it closes, so a future
reader does not mistake the exception for an oversight.

### Override mechanism

A commit message trailer waives one finding. No env var, no CLI flag,
ever waives a finding; only a trailer does, because a trailer is
permanent in `git log`.

- `Allow-Test-Change: <ID> <reason>` waives one `TT01`–`TT10`,
  `TT12`, or `TT13` finding. `<reason>` needs six words or more after
  stripping a short boilerplate list (`fix`, `cleanup`, `refactor`,
  `wip`, `misc`, `temp`, `n/a`, `na`, `ok`, `done`, case-insensitive).
  A reason that is empty, or that is only boilerplate words, is
  rejected; the finding stays unresolved.
- `Allow-Gate-Change: <ID> <reason>` is the only way to waive `TT11` or
  `TT14`. It needs fifteen words or more, not six, and it is not
  accepted alone. Every other finding present in the same diff also
  needs its own explicit `Allow-Test-Change` trailer; `Allow-Gate-Change`
  never covers a sibling finding by itself. `TT11` and `TT14` share the
  hardest override because both guard against rewriting every other
  rule at once: `TT11` catches an infra change riding beside another
  file, `TT14` catches deleting or hollowing the rule functions that
  same override protects. A six-word `Allow-Test-Change` reason must
  never be enough to waive a finding that can silence every other
  finding.
- One trailer line covers exactly one finding ID. The first trailer for
  a given ID wins; a duplicate later in the message is ignored.
- A malformed trailer (bad ID, boilerplate-only reason, wrong minimum
  word count) is not a usage error. It leaves the finding unresolved,
  the same as no trailer at all.

The recovery flow for a legitimate test change: stage the change,
then run `git commit -m "$(cat <<'EOF' ... EOF)"` with the trailer in
the message body. `pre-commit` runs first, in its sandbox with no
`.git`, and skips the tampering check there without judging it.
`commit-msg` runs next against the real repository, reads the drafted
message, resolves the trailer, and clears the finding. A plain
`make verify-fast` run by hand, with the change staged and no
`--message-file`, still reports the finding and exits 1; that is
expected, not a bug — a hand run has no message to read an override
from, the same limit `commit-msg` exists to remove.

### Rejected alternatives

- A single override trailer name for every finding: rejected. Item 11
  needs a harder bar than the rest; one name cannot carry two policies.
- Enforcing overrides inside `pre-commit`: rejected. Git runs
  `pre-commit` before the commit message exists, confirmed by testing
  `git commit -m` against a probe hook; no message is readable there.
- A `--allow` CLI flag or an environment variable: rejected outright by
  the directive. A flag never lands in `git log`; only a trailer does.
- Verifying that a trailer's reason is true: rejected. The boilerplate
  and word-count checks catch a lazy or absent reason; they cannot
  tell a genuine reason from a fabricated one that meets the same
  shape. That is an inherent limit of a mechanical, diff-only gate
  with no intent judge, matching the directive's no-LLM-judge rule.
  The trailer's value is forensic: a permanent, attributable line in
  `git log`, not a guarantee the reason is honest. Review remains the
  backstop for a dishonest but well-formed trailer.

## API

Command-line surface, `scripts/check_test_tampering.py`:

```text
check_test_tampering.py [--range REV_RANGE] [--message-file PATH] [--probe]

--range REV_RANGE    Diff this range instead of the staged tree. Passed
                      to `git diff` unchanged, e.g. origin/main...HEAD.
--message-file PATH  Read override trailers from this file. Rejected
                      together with --range; a range diff already reads
                      its message from the tip commit.
--probe              Run the self-test suite in synthetic temp repos
                      and exit; ignores --range and --message-file.
```

Exit codes:

- `0`: no findings, every finding overridden, or the diff-resolution
  skip cases (no `.git`, no parent commit).
- `1`: one or more findings remain unresolved after override
  resolution.
- `2`: usage error (`--range` and `--message-file` given together, or
  `git diff` itself fails on a bad range).

Output, one line per unresolved finding, matching the other `check_*.py`
gates: `<path>:<line>: <ID> <message>`. An overridden finding prints one
informational line naming the ID and the trailer that waived it, then
is excluded from the exit-code decision.

Module layout, mirroring `check_mutation.py` plus `mutation_tokenize.py`:

- `scripts/check_test_tampering.py` — CLI parsing, diff-source
  resolution, orchestration, `--probe` dispatch, output, exit code.
- `scripts/test_tampering_diff.py` — git plumbing: builds the `FileDiff`
  and hunk model for the chosen range, staged tree, or fallback commit.
- `scripts/test_tampering_rules.py` — the fourteen detection functions,
  each returning zero or more `Finding` values from the diff model.
  Split into two files by concern (test-file rules, gate-infra and
  vector rules) if either nears the 500-line file limit.
- `scripts/test_tampering_override.py` — trailer parsing, boilerplate
  rejection, word-count checks, and the `TT11`/`TT14`
  sibling-coverage rule.
- `scripts/test_tampering_probes.py` — the `--probe` entry point:
  imports and runs every probe module below, following
  `check_mutation.py`'s `run_probe` shape.

The probe suite carries roughly forty synthetic-repo cases: two per
detection, four extra for `TT14`'s sub-cases, plus the diff-resolution
and override cases. That is well past what one 500-line file holds
comfortably. The plan splits the probes by category up front, instead
of leaving the split as a reaction to a size gate that does not exist
for Python files:

- `scripts/test_tampering_probes_testfile.py` — `TT01`–`TT08`.
- `scripts/test_tampering_probes_vectors.py` — `TT09`–`TT10`.
- `scripts/test_tampering_probes_infra.py` — `TT11`–`TT14`.
- `scripts/test_tampering_probes_diffoverride.py` — diff-resolution
  fallbacks and override-trailer resolution, including the `TT11` and
  `TT14` hardness cases.

`policy/layers.json` needs no new row. `check_deps.py`'s
`package_dirs()` already excludes `scripts/`; this change adds no Go
package and no Go import edge. `api/` needs no update; the change
exports no Go symbol.

## Tests

`--probe` builds a fresh temporary git repository per case (one
initial commit, then a second commit or staged change carrying the
violation), following `check_mutation.py`'s `_probe_*` shape: no fixture
lives as a checked-in file.

For every ID `TT01` through `TT14`:

- Plant the violation; assert `check_test_tampering.py` reports that ID.
- Plant the clean equivalent (a move for `TT01`, a helper extraction for
  `TT04`, an added vector file for `TT10`, and so on); assert silence.

Additional probes:

- Diff resolution: a repo with a staged change picks the staged diff; a
  clean repo with a parent commit falls back to `HEAD~1...HEAD`; a
  directory with no `.git` skips with exit 0; a single-commit repo with
  no parent skips with exit 0.
- Override resolution: a valid `Allow-Test-Change` trailer waives its
  ID and only its ID; a boilerplate-only reason leaves the finding
  unresolved; a six-word reason passes, a five-word reason fails.
- `TT11`/`TT14` hardness: an `Allow-Gate-Change` trailer alone, with a
  sibling `TT0x` finding present and no matching `Allow-Test-Change`
  trailer, leaves every finding unresolved; adding the sibling trailer
  clears both. A six-word `Allow-Test-Change` trailer naming `TT14`
  must never clear it; only `Allow-Gate-Change` with fifteen words or
  more does.
- `--range` mode reads the tip commit's own message and honors its
  trailers without a `--message-file`.
- `--range` combined with `--message-file` exits 2.

`TT11` doc-companion probes (addendum), eight new probe functions
added to `scripts/test_tampering_probes_infra.py`, one function per
case below. Each probe is a genuine discriminator: it plants a diff,
runs `check_self_reference_guard` directly, and asserts a specific
presence or absence of `TT11`, not just that the call returns without
an exception.

- Gate infra plus `AGENTS.md` alone, nothing else in the diff: assert
  no `TT11` finding.
- Gate infra plus one `docs/plans/*.md` file alone, nothing else in
  the diff: assert no `TT11` finding.
- Gate infra plus one `docs/packages/*.md` file alone, nothing else
  in the diff: assert no `TT11` finding. `docs/packages/` gets the
  same silence grant as `docs/plans/` and needs its own case; a probe
  set that only exercises `docs/plans/` never proves the
  `docs/packages/` branch works.
- Gate infra plus one real `.go` file at an ordinary path (for
  example `foo/bar.go`, outside `docs/plans/` and `docs/packages/`),
  nothing else: assert `TT11` still fires, unchanged from before the
  addendum. This case never reaches `_is_doc_companion`'s `.md`
  extension check; the path-prefix check alone already rejects it.
- Gate infra plus one `.go` file placed at `docs/plans/malicious.go`,
  nothing else: assert `TT11` still fires. The path prefix matches
  `docs/plans/`, so this case forces execution of the
  `path.endswith(".md")` branch inside `_is_doc_companion` and proves
  the extension check, not the prefix alone, rejects the file.
- Gate infra plus one `.go` file placed at
  `docs/packages/malicious.go`, nothing else: assert `TT11` still
  fires. Same reasoning as the `docs/plans/` case, for the
  `docs/packages/` prefix.
- Gate infra plus one real `.go` file at an ordinary path and one
  `docs/plans/*.md` companion together, in the same diff: assert
  `TT11` still fires. This proves the doc companion does not launder
  a real code change riding beside it.
- Gate infra plus one real `.go` file at an ordinary path and one
  `docs/packages/*.md` companion together, in the same diff: assert
  `TT11` still fires. Same reasoning as the `docs/plans/` companion
  case, for the `docs/packages/` companion path. Together the last
  two cases rule out an implementation that treats any doc companion
  in the diff as a free pass for a code file riding along.

The builder registers all eight probe functions above in
`run_infra_probes`'s dispatch tuple
(`scripts/test_tampering_probes_infra.py`, the block that already
invokes `_probe_tt11_clean_infra_only` and the other `TT11` probes).
An unregistered probe function is well-formed but never runs under
`--probe`; registration in the tuple is mandatory, not optional.

## Verification

Builder-executed wiring, outside the planner's own edits:

- `Makefile`, `verify-fast` target: add
  `python3 scripts/check_test_tampering.py` after the other
  `check_*.py` lines, before the Semgrep scan.
- `Makefile`, `verify` target: add
  `python3 scripts/check_test_tampering.py --probe` beside the other
  `--probe` runs (`check_semgrep_probes.py`, `check_mutation.py
  --probe`, `check_orphan_packages.py --probe`).
- New file `.githooks/commit-msg`, matching `.githooks/pre-commit`'s
  header style: fail closed, no `--no-verify` bypass, runs
  `python3 scripts/check_test_tampering.py --message-file "$1"` from
  the repository root.
- `AGENTS.md` enforcement ladder: add a prohibition next to the
  coverage-floor entry: do not delete, skip, rename out of collection,
  or weaken a test instead of fixing the code behind it, gated by
  `scripts/check_test_tampering.py`.
- `AGENTS.md` coverage-floor entry: replace "Mutation testing is future
  work" with a sentence naming `scripts/check_mutation.py` and its
  per-package floors in `scripts/mutation_denylist/`, since that work
  already landed.
- `.github/workflows/ci.yml` needs no change. It already runs
  `make verify`, which now includes this gate. CI has no branch
  protection yet, so a failing check stays informational there; the
  hooks above are the enforcement that blocks a local commit.

Gates this plan itself must pass: `python3 scripts/check_plan.py`,
`python3 scripts/check_prose.py`, `python3 scripts/check_deps.py`,
`python3 scripts/check_labels.py`.

Gates the finished gate must pass: `make verify-fast` (gofmt, vet,
tests, the python gates including this one, Semgrep, the marker scan)
and `make verify` (adds the coverage floor and every `--probe` suite,
including this gate's own).

`check_structure.py` and `check_names.py` scan `*.go` files only; they
do not check the new Python modules. The module split above follows
the same file- and function-size discipline by convention, not by a
mechanical gate, since no Python structure gate exists in this repo.

### Verification for the TT11 doc-companion addendum

No new gate, no `Makefile` change, no `policy/layers.json` row: the
addendum only narrows an existing rule inside
`scripts/test_tampering_rules_infra.py` and its probe module. Same
gates as above cover it:

- `make verify-fast` runs `check_test_tampering.py` itself, which
  exercises `check_self_reference_guard` against the real repository
  diff.
- `make verify` runs `check_test_tampering.py --probe`, which must
  include the eight new discriminator probe functions from the Tests
  section above, registered in `run_infra_probes`'s dispatch tuple and
  passing green alongside every existing TT01-TT14 probe.
- `python3 scripts/check_prose.py`, `python3 scripts/check_plan.py`,
  and `python3 scripts/check_labels.py` must pass against this plan
  file itself.
