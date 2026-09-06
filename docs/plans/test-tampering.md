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

`check_test_tampering.py` picks its comparison in this order:

1. `--range REV_RANGE` given: diff that range with `git diff REV_RANGE`.
   The message source is the tip commit's own message
   (`git log -1 --format=%B <tip>`); the trailers already sit in git log.
2. `--message-file PATH` given, and something is staged
   (`git diff --cached --quiet` exits non-zero): diff the index. The
   message source is that file. This is the `commit-msg` hook's path.
   It must audit exactly the snapshot being committed, never the
   working tree around it. When `MERGE_HEAD` exists, this step is
   merge-aware; see "Merge commits" below.
3. A merge is in progress: `MERGE_HEAD` exists, or
   `git diff --name-only --diff-filter=U` prints any path. Print
   `check_test_tampering: merge in progress; skipping` and exit 0.
   Either condition alone is enough. This step has no other trigger; in
   particular it does not test whether the working tree differs from
   `HEAD`. See "Why the merge stand-down is its own step" below.
4. `HEAD` resolves and the working tree differs from `HEAD`
   (`git diff --quiet HEAD` exits non-zero): diff the working tree with
   `git diff HEAD`. One comparison covers staged and unstaged changes
   together. The message source is none.
5. `HEAD` does not resolve (no commit exists yet) and something is
   staged: diff the index with `--cached`. The message source is none.
   A `--message-file` given here is unreachable, because item 2 already
   catches every staged-plus-message-file case.
6. `HEAD` is a merge commit (two or more parents): compare `HEAD`
   against every parent and report only what the merge itself
   introduced. See "Merge commits" below. The message source is
   `HEAD`'s own commit message.
7. `HEAD` has exactly one parent: diff `HEAD~1 HEAD`. The message source
   is `HEAD`'s own commit message. This is the case a clean CI checkout
   hits, so `make verify` in CI checks the tip commit for real, with its
   trailers already final.
8. No `.git` found at all (the `pre-commit` sandbox): print a skip note
   and exit 0. The code tests this first, before every item above,
   because no other item can run without a repository.
9. `HEAD` has no parents, because no commit exists yet or because
   `HEAD` is the root commit, and nothing is staged: print a skip note
   and exit 0. The printed text stays
   `check_test_tampering: no parent commit; skipping`. That wording is
   true for both cases, and the existing end-to-end probe asserts it.

#### Why the working tree comes fourth

Item 4 exists because the gate used to abstain on a dirty tree. Before
this addendum, a bare run with nothing staged fell straight to item 7.
It then audited the previous commit instead of the developer's own
uncommitted work. A working tree that deleted a test reported nothing
and exited 0. AGENTS.md tells every agent to run `make verify` before
reporting done, so the gate stayed silent exactly when it was trusted
most.

The trigger is a conjunction, not a single condition. Each part earns
its place:

- "No `--message-file`" keeps the `commit-msg` hook on item 2. The hook
  must judge the index, because the index is the commit. An unstaged
  edit sitting beside it is not part of that commit. Item order alone
  enforces this; item 4 needs no extra test.
- "The working tree differs from `HEAD`" keeps a clean checkout on items
  6 and 7. A clean CI checkout still audits the tip commit, unchanged.
  A trigger of "no `--message-file`" alone would make item 4 always
  win, and the tip-commit audit would never run again.

`make verify` on a dirty tree can now fail where it previously passed.
That is the point of the change, not a regression. Exit 0 used to mean
"the previous commit was clean". It now means "your work is clean".

#### Why the merge stand-down is its own step

The stand-down must be item 3, a separate step ahead of item 4. It must
not be a conjunct of item 4's trigger. Making it a conjunct leaves a
hole, and the hole is reproduced, not assumed.

Resolve a merge conflict in favour of the first parent, for example with
`git checkout --ours`. The working tree then equals `HEAD`, so
`git diff --quiet HEAD` exits zero and `has_worktree_changes` is false.
A stand-down written as a conjunct of item 4 never fires, because item 4
never fires. Control falls through to item 7, which diffs `HEAD~1 HEAD`
and reports findings from the already-landed pre-merge commit. In the
reproduction that printed `TT01` for a test the trunk commit had dropped
and `TT04` for that commit's assertion change, and exited 1. That is the
wrong comparison, and it is the exact class of error this change exists
to remove. As its own step, item 3 fires on `MERGE_HEAD` alone and
prints the intended skip note.

Item 3 covers two states, both reproduced.

The first is an unresolved conflict. The new side of that file is
conflict-marker text, which `_read_worktree` supplies to the rules. That
text is not a tree any commit will ever hold. It carries both sides'
content at once, so every count and every function set it yields is
meaningless. Judging it is judging a state that never existed. The
cleanly merged files beside it are worse: they hold the other branch's
content, measured against the first parent alone, which is Defect 2.
This is measured. Strip item 3 and the conflicted fixture reports
nothing and exits 0, because the marker text still holds both function
bodies. A cleanly applied path beside the conflict does fire, and item 4
carries no message source, so nothing could waive it. See
`_probe_conflicted_cherry_pick_skips`, where a conflicted cherry-pick
reports `TT01` from the picked commit's own, already-waived deletion.

The second is a merge whose conflicts are resolved but not committed. No
path is unmerged, but `MERGE_HEAD` still exists and the index holds the
merge result. Any comparison available here is a first-parent
comparison, which is Defect 2 again. The same is true while the conflict
is still open, so both states share one reason: no honest comparison
exists until the merge is committed.

`CHERRY_PICK_HEAD` and `REBASE_HEAD` get no blanket skip. Outside a
conflict, their working tree is the developer's own work measured
against `HEAD`, which is exactly what item 4 should audit. Inside a
conflict, they leave unmerged paths, so item 3's second condition
already covers them. That second condition is the only one that fires
there: a conflicted cherry-pick writes `CHERRY_PICK_HEAD` and never
`MERGE_HEAD`. So `has_unmerged_paths` is load-bearing on its own, not a
belt beside a brace. A wider skip would buy nothing and would open a
hiding place.

Skipping loses no coverage. The merge itself still goes through item 2
in the `commit-msg` hook when it is committed.

#### Where the new-side text comes from

A working-tree comparison has no blob for the new side. `git diff --raw
HEAD` prints an all-zero new sha for any unstaged change, not only for
an unmerged path. `_show_blob` returns `None` for that sha, so every
rule that reads `new_text` would see an empty file. Item 4 would then
report every function in every modified file as deleted. This is
measured: without the read, this change's own diff reported three false
`TT14` findings against the checker's own sources.

`test_tampering_diff._read_worktree` closes this. When `build_diff` gets
no blob for the new side, it reads the file from the working tree
instead. The gate stays read-only. It never runs `git add -N`, so it
never writes the developer's index to make a file visible.

The read is guarded by `status != "D"`, and that guard is load-bearing.
It is the only thing that keeps the working tree out of an index
comparison. A staged deletion whose file still sits on disk, as
`git rm --cached` leaves it, has status `D` and an all-zero new sha. An
unguarded read would hand back the file's full text, the deletion would
read as no change, and `TT01` would be lost in silence. That is
under-reporting, the direction this gate must never fail in. The guard
is safe because a non-worktree comparison never produces an all-zero new
sha for any other status. See
`_probe_staged_deletion_ignores_the_worktree_file`.

#### Uncommitted findings cannot be waived

Item 4 carries no message source, so no trailer can waive its findings.
This matches item 2's existing rule: a staged diff with no
`--message-file` can never honor an override. No message exists at
either point. Any finding there blocks. This is deliberate; the real,
override-capable gate is `commit-msg`.

This inverts the usual order of work for one case. AGENTS.md and
`.agents/skills/delivery/SKILL.md` both put `make verify` before the
commit. A change carrying a legitimate, waivable finding can no longer
reach a green `make verify` before it is committed. The only way to
green is to commit with the trailer, then re-run `make verify` on the
now-clean tree.

If the trailer is forgotten, `git commit --amend` alone does not fix it.
With nothing staged, item 2 does not apply, and the tip-commit step
reads `HEAD`'s old message, so the finding still blocks. That step is
item 7 for an ordinary commit and item 6 when `HEAD` is a merge; the
outcome is the same either way. The remedy is to re-stage the
change and amend, or to reset and re-commit, so item 2 sees a staged
diff beside the new message. State this to any agent that hits the
block. An agent that does not know it will reach for the gate file
instead of the commit message, which AGENTS.md forbids.

#### Accepted limit: untracked files

`git diff HEAD` does not see an untracked file. A brand-new,
never-staged `_test.go` file is invisible to item 4. This is an accepted
limit, not a gap the plan closes.

Closing it would need `git add -N` (intent-to-add), which writes to the
index. A gate must never mutate the developer's index. The gate is
read-only and stays read-only.

The limit costs little. An untracked file can only add tests, never
remove one. The moment it is staged, item 2 sees it, and no commit
lands without passing item 2 in the `commit-msg` hook.

### Merge commits

A merge commit used to re-judge the whole merged branch.
`git diff HEAD~1 HEAD` on a merge compares the first parent's tree
against the merge tree. That comparison is the entire branch, not the
merge. A branch commit's override trailer does not carry to the merge
commit, so already-waived content fired again. This made `make verify`
red on a clean checkout of `main`.

A merge introduces exactly the content that differs from every parent.
Content from any one parent was already gated by that parent's own
`commit-msg` run. So the gate runs the rules once per parent and
reports the intersection.

#### Merge handling belongs in item 2, not only in item 6

`git merge` runs the `commit-msg` hook. This is measured, not assumed:
a probe hook logged four runs for three ordinary commits plus one
`git merge --no-ff`, and `MERGE_HEAD` existed during the fourth run.

So the gate meets a merge at item 2, not at item 6. At that moment
`HEAD` is still the first parent, and `git diff --cached` compares
`HEAD` to the index. That is the first-parent comparison, which is
Defect 2 exactly. An item 6 that only handles a finished merge commit
never runs at merge creation, and the merge stays blocked.

Item 2 is therefore merge-aware. When `MERGE_HEAD` exists, it compares
the index against every parent and intersects:

- `git diff --cached` compares the index to `HEAD`, the first parent.
- `git diff --cached MERGE_HEAD` compares the index to the second
  parent. `MERGE_HEAD` holds one revision per remaining parent, one per
  line, so an octopus merge adds one comparison per line.

The message source is unchanged: the drafted merge message. A merge
that introduces nothing then reports nothing, subject to the aggregate
limit below.

Item 6 still exists, and it is not redundant. It covers a merge commit
that already exists: a clean CI checkout whose `HEAD` is a merge, a
`--range` ending on a merge, or a merge made on a machine without the
hooks installed.

#### Squash merges are not merges here

`git merge --squash` sets no `MERGE_HEAD`. It writes `SQUASH_MSG` and
stages the branch's whole content against `HEAD`. Item 2 is therefore
not merge-aware for a squash, and the squash commit is judged against
the entire branch. This is reproduced: a squashed branch that dropped a
test reports `TT01` and `TT04`.

This is correct, not a defect. A squash commit really does introduce all
of that content as one new commit, and no parent link records the branch
that produced it.

The trailers usually survive anyway, and the reason is worth stating.
`git merge --squash` copies every squashed commit message into
`SQUASH_MSG`, indented four spaces. `parse_trailers` matches each line
after `line.strip()`, so the indentation does not hide a trailer. An
author who accepts the default `SQUASH_MSG` therefore carries the
branch's trailers into the squash commit, and the findings clear. This
is measured: two `Allow-Test-Change` trailers parsed from `SQUASH_MSG`
cleared both findings on the squashed index.

The obligation appears only when the author rewrites the message. A
`git commit -m` on a squash, or an edited `SQUASH_MSG` with the commit
log stripped out, drops the trailers, and every finding returns
unresolved. So the rule is conditional, not absolute: a squash commit
keeps the branch trailers only if the author keeps the default message,
and an author who rewrites the message must repeat every trailer. Say so
wherever the merge advice appears.

#### The intersection key

The key answers one question: is this the same finding against every
parent? The answer differs for per-file rules and for whole-diff
aggregate rules, so the key differs too.

Per-file rules (`TT01`-`TT03`, `TT05`-`TT10`, `TT12`, `TT13`, `TT14`)
key on the ID, the path, and the message with every digit run replaced
by a single `#`. The line is excluded.

The line must be excluded. On a deletion the line refers to the old
side, which differs per parent. A branch that shifted a test function
down reports that same deletion at a different line against each
parent. A key holding the line would fail to match, and a real
merge-introduced finding would go unreported.

The digits must be normalized. `TT07` and `TT13` embed numbers in their
messages, and those numbers are relative to the parent being compared.
A key holding raw digits would fail to match the same finding across
two parents.

`TT13` is a per-file rule and must keep the per-file key.
`check_weakened_floor` emits one `Finding("TT13", d.path, ...)` per
changed file and can emit several from one diff. Its path is the file
actually changed, either `Makefile` or a named
`scripts/mutation_denylist/*.json`, so it never depends on diff order.
Treating `TT13` as an aggregate reintroduces Defect 2 for it: a benign
merge whose branch lowered a mutation floor and whose trunk lowered
`COVERAGE_FLOOR`, both already waived, reports `TT13` under an ID-only
key and stays silent under the per-file key. Digit normalization already
absorbs the parent-relative floor numbers.

Aggregate rules are exactly `TT04` and `TT11`. They key on the ID alone.
Neither reports per file. `TT04` counts assertion sites across the whole
diff and reports at `first_path`, the first test file in diff order
(`test_tampering_rules.py`, `check_assertion_decrease`). `TT11` reports
at `infra[0]`, the first gate-infra file in diff order
(`test_tampering_rules_infra.py`, `check_self_reference_guard`).

Diff order changes with the parent, so the reported path changes with
the parent, and the aggregate count changes with the parent. A key
holding either would empty the intersection and drop a real finding.
Two reproductions pin this:

- A branch removes two assertions from `a_test.go`, already waived. The
  merge additionally removes two from `z_test.go`, which both parents
  kept. Against the first parent the gate reports
  `TT04 a_test.go (4 removed, 0 added)`. Against the second it reports
  `TT04 z_test.go (2 removed, 0 added)`. A path-bearing key matches
  neither, so the finding vanishes. The old code reported it. The
  ID-only key reports it.
- A branch changes `scripts/a_check.py` beside a code file, already
  waived. The merge additionally changes `scripts/z_check.py` beside a
  different code file. `TT11` reports at `scripts/a_check.py` against
  one parent and `scripts/z_check.py` against the other. Same outcome.

An aggregate finding that survives the intersection prints the first
parent's own message. Its counts are relative to that parent, so they
can overstate what the merge alone introduced. In the `TT04`
reproduction above the printed message says four removed, while the
merge introduced two. The count is a signal, not a measurement of the
merge. The reader must open the diff.

Normalization and the ID-only key cannot lose a finding. The gate
reports the first parent's own `Finding` values, filtered by whether
their key appears in every parent's key set. Two distinct findings that
share one key are still two entries in that list, and both print. A
shared key can only make the gate report one finding too many. It can
never make the gate report one too few.

#### Known limit: independent aggregate findings on two parents

The ID-only key costs precision, and the cost is a real, reproduced
exception to the headline promise. State it plainly everywhere the
promise appears.

A merge that introduces nothing normally needs no trailer. The exception
is an aggregate ID, `TT04` or `TT11`, that two or more parents carry
independently. The ID-only key then matches across every parent, the
finding survives the intersection, and the benign merge blocks.

Reproduced on an ordinary two-parent merge. The branch dropped one test
and the trunk dropped a different test, each already waived on its own
commit. The merge introduced nothing. `TT01` correctly went silent for
both, because the per-file key separated them. `TT04` survived and the
merge failed with `Not committing merge`. The octopus equivalent behaves
the same way.

This limit is not engineered away, deliberately. Over-reporting fails
closed, and a merge that blocks is recoverable by a trailer. The
alternative direction, under-reporting, would let a real finding through
and is not recoverable at all. `TT13` shows the same trade-off resolved
the other way: it keeps the per-file key, because it gains nothing from
the ID-only key and loses precision to it.

The recovery is one trailer on the merge commit, naming the aggregate
ID. This is the one case where a merge does repeat a trailer. Read the
diff before writing it, exactly as for any other finding.

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

Internal contract, changed by the working-tree and merge addendum:

- `resolve_diff_source(range_arg, message_file, root)` returns
  `(comparisons, message, skip)`. `comparisons` is a list of diff-arg
  lists, one entry per comparison. Every case returns exactly one
  comparison, except the two merge cases, which return one per parent.
- `test_tampering_diff.has_head_commit(root)` reports whether `HEAD`
  resolves. It replaces `has_parent_commit`, which becomes unused and
  is removed in the same change.
- `test_tampering_diff.has_worktree_changes(root)` reports whether the
  working tree differs from `HEAD`. It runs `git diff --quiet HEAD` and
  returns true only for exit code 1. Exit code 128 means `HEAD` does
  not resolve, which is not a difference; the helper must return false
  there rather than treating any non-zero code as a difference. Item 4
  also guards on `has_head_commit`, but the helper must not depend on
  its caller doing so.
- `test_tampering_diff.has_unmerged_paths(root)` reports whether
  `git diff --name-only --diff-filter=U` prints anything.
- `test_tampering_diff.merge_head_revs(root)` reads `MERGE_HEAD` from
  the git directory and returns its revisions, one per line. It returns
  an empty list when no merge is in progress. `git rev-parse --git-dir`
  locates the file; do not assume `.git` is a directory, because it is
  a file in a worktree or a submodule. `git merge --squash` writes
  `SQUASH_MSG` and no `MERGE_HEAD`, so this helper correctly reports no
  merge for a squash.
- `test_tampering_diff.head_parents(root)` returns `HEAD`'s parent
  revisions in git's own order. It returns an empty list when `HEAD`
  does not resolve and when `HEAD` is the root commit.
- `check_test_tampering.AGGREGATE_IDS` is the frozen set
  `{"TT04", "TT11"}`: the two rules that report once per diff, at a path
  that depends on diff order. `TT13` is not a member; see "The
  intersection key".
- `check_test_tampering.finding_key(f)` returns `(f.id,)` for an
  aggregate ID. For every other ID it returns the ID, the path, and the
  message with every digit run replaced by a single `#`. The line is
  never part of the key.
- `check_test_tampering.introduced_findings(root, comparisons)` runs
  the rules over each comparison. With one comparison it returns those
  findings unchanged. With more it returns the first comparison's
  findings whose key appears in every comparison's key set.

Removing `has_parent_commit` does not trip `TT14`. That rule fires on a
vanished `TTnn` ID literal, not on a removed helper name.

`TT14` does constrain the ID literals in the checker's own sources.
Before this change `check_test_tampering.py` held two, `TT01` and
`TT14`, both inside the module docstring's phrase "Fourteen stable
finding IDs, TT01-TT14". Adding `AGGREGATE_IDS` raises that to four,
because `TT04` and `TT11` now appear as literals in the source. Adding
literals never fires `_check_id_vanished`; only losing one does. Two
consequences follow. Keep the module docstring's `TT01-TT14` phrase
while rewriting `resolve_diff_source`'s docstring. Treat the two new
literals as load-bearing: a later change that drops an ID from
`AGGREGATE_IDS` fires `TT14` against the checker's own source and needs
an `Allow-Gate-Change` trailer. Relocating the set does not escape this;
`_is_checker_source` covers `scripts/test_tampering_*.py` as well as
`check_test_tampering.py`.

These are Python helpers inside `scripts/`. No exported Go symbol
changes, so `api/` needs no update. No Go import edge changes, so
`policy/layers.json` needs no new row.

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

- Diff resolution: a repo with a dirty working tree picks the working
  tree; a clean repo with a parent commit falls back to `HEAD~1...HEAD`;
  a directory with no `.git` skips with exit 0; a single-commit repo
  with a clean tree skips with exit 0.
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

Working-tree and merge probes (addendum), added to
`scripts/test_tampering_probes_diffoverride.py`. Each probe below is
labelled discriminator, pin, or limit pin. A discriminator fails against
the pre-addendum code. A pin passes both before and after, and exists to
stop a later change from silently moving the behavior. A limit pin
records a known, accepted shortcoming so the next agent does not
rediscover it while debugging.

Fixture rule for every benign-merge probe: at most one parent may carry
an aggregate finding (`TT04` or `TT11`). Two parents carrying the same
aggregate ID independently makes a benign merge block, by design. See
"Known limit: independent aggregate findings on two parents". A fixture
that ignores this rule makes its own probe red.

Existing probes to change, because the resolution order changed:

- `_probe_staged_picked`: rename to `_probe_worktree_picked_over_staged`.
  Stage a change, then assert `resolve_diff_source(None, None, repo)`
  returns `[["HEAD"]]`, not `[["--cached"]]`. `git diff HEAD` is a
  superset of the staged diff, so this asserts more than the old case,
  not less.
- `_probe_staged_no_message_file_is_none`: keep the assertion that the
  message is `None`, and update the expected comparison to `[["HEAD"]]`.
  Uncommitted work still cannot be waived.
- `_probe_single_commit_skips` and
  `_probe_single_commit_skips_end_to_end`: the root-commit case now
  reaches item 9 by way of "`HEAD` has no parents", not "`HEAD~1` does
  not resolve". Both probes keep their assertions, including the
  literal `no parent commit` text, which item 9 keeps.
- `_probe_staged_reads_message_file`: unchanged in intent. It still
  asserts `[["--cached"]]` and the file's text, because
  `--message-file` keeps the `commit-msg` hook on item 2.
- `_probe_fallback_to_parent`, `_probe_no_git_skips`,
  `_probe_range_reads_tip_message`: unchanged in intent. Each asserts a
  list of one comparison now, so `["HEAD~1", "HEAD"]` becomes
  `[["HEAD~1", "HEAD"]]`.

New probes for the dirty working tree:

- `_probe_dirty_worktree_is_audited` (discriminator): build a repo whose
  second commit adds a test function. Delete that file in the working
  tree and do not stage it. Run the checker as a subprocess. Assert exit
  1 and a `TT01` line naming the removed function. Before the addendum
  this printed nothing and exited 0.
- `_probe_dirty_worktree_unstaged_edit_is_audited` (discriminator):
  same repo, no file deletion. Edit `TestKeepMe` in place and leave the
  edit unstaged. Remove its two `t.Fatal(` assertion lines and add
  nothing. That is a net assertion decrease with no added test function
  and no added line containing `assert*(`, `Cases(`, `Suite(`, or
  `RunAll(`, so no helper signal suppresses it. Assert `TT04` is
  reported. This proves item 4 reads unstaged content, not the index.
- `_probe_dirty_worktree_finding_cannot_be_waived` (discriminator):
  leave the same unstaged deletion in the tree and put a valid
  `Allow-Test-Change` trailer in the tip commit's message. Assert the
  finding still blocks. Item 4 carries no message source.
- `_probe_untracked_test_file_is_an_accepted_limit` (limit pin): write a
  new `_test.go` file and leave it untracked. Assert exit 0.
- `_probe_clean_tree_still_audits_tip_commit` (pin): commit the
  deletion, so the tree is clean. Assert the checker still reports the
  `TT01` finding from the tip commit. This proves item 4 did not
  displace item 7.

New probes for the merge stand-down, item 3:

- `_probe_conflicted_worktree_skips` (discriminator): create a content
  conflict in a `_test.go` file and stop at `UU`. Assert exit 0 and the
  printed `merge in progress; skipping` note. Without item 3,
  `git diff HEAD` reads the unmerged path's new blob as absent and
  `TT01` fires on a file nobody deleted.
- `_probe_resolved_merge_worktree_skips` (discriminator): the repo's
  trunk commit must itself carry a finding, so that `HEAD~1 HEAD`
  reports something. Create the conflict, then resolve it in favour of
  the first parent with `git checkout --ours` plus `git add`, leaving no
  unmerged path, `MERGE_HEAD` present, and the working tree equal to
  `HEAD`. Assert exit 0 **and** the printed
  `merge in progress; skipping` note. Asserting exit 0 alone does not
  discriminate: a fall-through to item 7 on a repo whose previous commit
  is clean also exits 0. The note and the dirty previous commit are both
  required for this probe to fail against a stand-down written as a
  conjunct of item 4.
- `_probe_merge_commit_msg_hook_benign_merge_commits`
  (discriminator, end to end): install the real checker as
  `.git/hooks/commit-msg` in the throwaway repo. Base commit holds a
  test. A branch commit deletes it, carrying valid `TT01` and `TT04`
  trailers. The trunk gains an unrelated commit that carries no
  aggregate finding, per the fixture rule above. Run
  `git merge --no-ff`. Assert the merge commits and that `HEAD` has two
  parents. Before the addendum this failed with
  `Not committing merge`. Set `core.hooksPath` nowhere; write the hook
  into the repository's own `.git/hooks/` directory, which the project
  hook guard permits.
- `_probe_merge_commit_msg_hook_evil_merge_blocks` (discriminator, end
  to end): same repo and hook, but the merge tree also drops a test
  both parents kept. Assert the merge fails and the output names
  `TT01`.
- `_probe_squash_merge_is_judged_whole` (pin): run
  `git merge --squash` on a branch whose commit carries valid `TT01` and
  `TT04` trailers. Assert `MERGE_HEAD` does not exist and that the rules
  report `TT01` and `TT04` against the squashed index, because a squash
  is judged against the whole branch. Then assert both directions of the
  message rule. With `.git/SQUASH_MSG` as the message, both findings
  resolve, because git indents the squashed commit log four spaces and
  `parse_trailers` strips each line before matching. With a rewritten
  message that carries no trailer, both findings stay unresolved. The
  third assertion is the one that fails if the plan's conditional
  wording is ever flattened back into an absolute rule.

New probes for a finished merge commit, item 6:

- `_probe_merge_introducing_nothing_is_silent` (discriminator): the
  benign merge above, already committed. Assert exit 0 and no finding.
  Before the addendum this reported `TT01` and `TT04`. Keep the fixture
  inside the rule above: only the branch parent may carry an aggregate
  finding.
- `_probe_evil_merge_still_fires` (pin against a wrong key): the branch
  adds a second test function above the first, shifting it down. The
  merge tree drops the function both parents kept. Assert `TT01` is
  reported. The old code also reported this, so the probe does not
  discriminate against the old code. It discriminates against a key
  that includes the line, which reports nothing here.
- `_probe_evil_merge_beside_waived_branch_removal` (discriminator): the
  branch removes one test function, and the merge removes a second one
  that both parents kept. Assert the merge-removed function is reported
  and the branch-removed one is not.
- `_probe_merge_aggregate_tt04_survives` (discriminator): the branch
  removes two assertions from `a_test.go`. The merge additionally
  removes two from `z_test.go`, which both parents kept. Assert `TT04`
  is reported. A path-bearing key reports nothing here, while the old
  code reported the finding, so this probe pins the fix against its own
  regression.
- `_probe_merge_aggregate_tt11_survives` (discriminator): the branch
  changes `scripts/a_check.py` beside a code file. The merge
  additionally changes `scripts/z_check.py` beside a different code
  file. Assert `TT11` is reported. The two infra paths must differ; a
  fixture where both parents report the same infra path passes under
  the per-file key too and proves nothing.
- `_probe_merge_tt13_is_not_aggregate` (discriminator): a benign merge
  introducing nothing, where the branch lowered a floor in
  `scripts/mutation_denylist/a.json` and the trunk lowered
  `COVERAGE_FLOOR` in `Makefile`, both already waived. Assert no `TT13`
  is reported. An `AGGREGATE_IDS` set containing `TT13` blocks this
  merge.
- `_probe_merge_independent_aggregate_blocks` (limit pin): the branch
  drops one test and the trunk drops a different test, each waived on
  its own commit. The merge introduces nothing. Assert `TT01` is silent
  and `TT04` is still reported. This records the known limit and its
  exact shape, so a later reader sees it is accepted, not broken.
- `_probe_octopus_merge_introducing_nothing_is_silent`
  (discriminator): merge two branches into the trunk in one commit,
  changing nothing else. Assert no finding. Apply the fixture rule: at
  most one parent may carry an aggregate finding.
- `_probe_octopus_merge_evil_still_fires` (pin): build the same
  three-parent merge with a tree that drops a test every parent kept.
  Assert `TT01` is reported. Build the commit with `git commit-tree`,
  so the probe needs no interactive merge resolution.
- `_probe_merge_key_normalizes_digits` (pin): call `finding_key` on two
  `TT01` findings whose messages differ only in digits. Assert the keys
  are equal.
- `_probe_merge_key_aggregate_ids_use_id_alone` (discriminator): call
  `finding_key` on two `TT04` findings with different paths and
  different counts. Assert the keys are equal. Assert the same for two
  `TT11` findings with different paths. Assert that two `TT13` findings
  with different paths produce different keys.
- `_probe_merge_key_keeps_distinct_findings_distinct` (pin): call
  `finding_key` on two `TT01` findings naming different functions in
  one file. Assert the keys differ.

The builder registers every new probe function in
`run_diffoverride_probes`'s dispatch tuple. An unregistered probe
function is well-formed but never runs under `--probe`.

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

### Verification for the working-tree and merge addendum

No new gate, no `Makefile` change, no `.githooks/` change, no `api/`
lock update, and no `policy/layers.json` row. The addendum changes
diff-source resolution inside `scripts/check_test_tampering.py` and
`scripts/test_tampering_diff.py`, plus the probe module
`scripts/test_tampering_probes_diffoverride.py`.

Gates the change must pass:

- `python3 scripts/check_test_tampering.py --probe`, with every probe
  named in the Tests section registered and green, beside every
  existing `TT01`-`TT14` probe.
- `make verify-fast`, which now runs the gate against the working tree
  rather than the previous commit.
- `make verify`, which adds the coverage floor and every `--probe`
  suite.
- `python3 scripts/check_prose.py`, `python3 scripts/check_plan.py`,
  and `python3 scripts/check_labels.py` against this plan file.
- `python3 scripts/check_deps.py`, which reads Go packages only and is
  unaffected by this change. It is listed to show the import policy was
  checked, not because it reads this file.

Ordering note for the builder. AGENTS.md and
`.agents/skills/delivery/SKILL.md` both put `make verify` before the
commit. After this change a diff carrying a waivable finding cannot
reach a green `make verify` before it is committed, because item 4 has
no message source. Commit with the trailer first, then re-run
`make verify` on the clean tree. If the trailer is forgotten, a bare
`git commit --amend` does not fix it: with nothing staged, item 2 does
not apply and the tip-commit step, item 7 or item 6 when `HEAD` is a
merge, reads the old message. Re-stage the change and amend, or reset
and re-commit. Do not edit the gate to get green.

Findings this change triggers against itself:

- `TT11` fires. The diff touches `scripts/`, which
  `_GATE_INFRA_PREFIXES` matches, together with
  `.agents/memories/override_trailers_dont_carry_to_merge_commits.md`.
  That memory file is not a doc companion. `_is_doc_companion` accepts
  only `AGENTS.md`, `docs/plans/*.md`, and `docs/packages/*.md`.
- `TT11` needs an `Allow-Gate-Change` trailer with fifteen or more
  significant words. `Allow-Test-Change` never waives `TT11`, and its
  six-word floor does not apply. Both floors sit in
  `scripts/test_tampering_override.py` as `_TEST_CHANGE_MIN_WORDS` and
  `_GATE_CHANGE_MIN_WORDS`.
- `TT14` does not fire. The change adds ID literals and removes none,
  provided the module docstring keeps its `TT01-TT14` phrase. See the
  API section.
- The planner does not author the trailer. The orchestrator verifies
  the diff and writes it.
- The memory file is written in the post-change tense. It must land in
  the same commit as the code, never ahead of it.
