# Doc path gate

Status: planned, not yet built.

## Goal

Stop a moved documentation directory from leaving dangling references
behind. The move of the plan directory repointed one gate script and
nothing else, so every cross-reference in the tree kept naming a
directory that no longer exists.

## Scope

Inside: one rule in `scripts/check_docs.py` that fails on the retired
plan directory prefix in any tracked file it can read as text. Its
probes. The mechanical repoint of every existing reference.

Outside: any other stale-path class. The rule names one retired prefix,
not a general link checker. A general checker needs a link parser for
Markdown, Go comments, JSON, and shell, which is a larger change with
its own plan.

## API

No Go surface. `scripts/check_docs.py` gains one internal check
function and two probe functions, matching the shape the package-doc
rule in the same file already uses.

## Tests

The rule's probes are its tests, following the convention every gate in
`scripts/` already follows.

One probe passes a file list holding one planted hit and asserts the
rule reports it. One probe passes a clean file list and asserts the
rule stays silent. Both run under `python3 scripts/check_docs.py
--probe`.

The check function takes the file list as an argument. The probes then
need no git repository, and the enumerator stays a separate concern
that `main` supplies. A probe that built a fixture directory with no
repository would report nothing and pass for the wrong reason.

The rule must fail on the tree as it stands before the repoint. That is
the positive control: a rule that passes on a tree holding 469 hits
across 138 files proves nothing.

## Verification

- `python3 scripts/check_docs.py` fails before the repoint and passes
  after it.
- `python3 scripts/check_docs.py --probe` passes.
- A tree-wide search for the retired prefix returns nothing after the
  repoint.
- `make verify` passes. `check_docs.py` already runs in `verify-fast`,
  so the rule reaches the pre-commit hook and CI with no Makefile
  change.

## Addendum: enumeration and exemptions

Status: planned, not yet built.

The rule enumerates tracked files with `git ls-files`, not a directory
walk. The repository root holds several untracked working copies of
itself under a tooling directory. A directory walk would report
hundreds of hits inside those copies, which belong to no commit.

The tracked set holds files the rule cannot read as text. One tracked
image decodes to no string, and seven tracked symbolic links point at
directories, so reading them raises rather than returning content. The
existing rules never meet either class, because they read only Go files
and the package reference pages.

Four more tracked symbolic links point at files, not directories. They
decode normally and must be scanned: they carry five hits of the
retired prefix between them. The skip is a content test, never a test
on the link mode, so those four stay in scope.

The rule therefore skips any path it cannot read as UTF-8 text. This is
not an exemption. A file that holds no decodable text cannot hold the
retired prefix as text, so skipping it removes no coverage. Without the
skip the gate ends in a stack trace on its first real run.

The enumerator needs one fallback, and the pre-commit hook is why. The
hook runs the gate on a tar extraction of the staged tree, with every
git environment variable cleared, so the stage is not a repository and
the tracked listing fails there.

The fallback is a plain directory walk, and it is exact in that one
place. The archive the hook extracts holds tracked files only, so a
walk of the stage enumerates the same set the tracked listing would.
Both enumerators return 1056 paths on the present tree.

The fallback's trigger must be the missing repository, not any listing
failure. Walk only when no git directory is discoverable at or above
the root, the same test the test-tampering gate already applies. Any
other listing failure is a hard error.

A broader trigger would undo the reason for the tracked listing. The
repository root holds untracked working copies carrying 847 files with
the retired prefix. A walk of the root reports every one of them.

### An older rule has the same enumeration weakness

The doc-comment rule in the same file walks the root for Go files. It
reads 1667 files, and 1437 of them sit inside those untracked working
copies. The rule passes today only because those copies happen to hold
no violation.

The consequence is a false positive waiting to happen. A stale copy
with one undocumented exported symbol fails the gate for a tree that
does not contain the file.

This change does not fix that rule. The fix is a one-line enumerator
swap, but it changes which files an existing gate judges, so it needs
its own before-and-after evidence rather than a ride on this change.
It is recorded here so the next reader finds it.

The rule carries no exemption list. An exemption is a hole, and this
gate exists because a previous repoint left holes.

Two files would otherwise need one, and both are handled by writing
rather than by exempting. The rule's own source builds the search
string from parts, so the source holds no literal to match. This plan
page names the retired directory in prose instead of as a path.

The repoint therefore covers every tracked file with no survivor: Go
comments, Markdown, the policy JSON files, the gate scripts, the
commit-message hook, the agent definitions, and the skill definitions.

One memory file cites the retired path inside a record of a past
event. The citation is a live cross-reference to a document that still
exists at its new location, so it is repointed. The narrative around it
is unchanged, because a memory records what happened and that did not
change.

The example Markdown page is byte-matched to its Go source by the
example-sync gate, so the page and the source change together.

## Addendum: three sibling doc-truth repairs

Status: planned, not yet built.

The same audit found three more places where a document describes
behavior the tree no longer has. None needs Go code.

### The enforcement ladder header

The contributor gate split moved the plan, prose, and label gates out
of the two main make targets and into a maintainer-only target. The
ladder heading in the agent instructions still says every rule below it
runs in the main target. Two of its bullets carry no tier note, while a
third bullet in the same list already carries one. The same file
contradicts the heading further down.

Fix: drop the claim from the heading, and add the tier note to the two
bullets, using the wording the correct bullet already uses.

### Deleted error sentinels in the reference pages

The error sweep consolidated several per-field sentinels into one
per-package options sentinel. Four reference pages still name the
deleted variables and tell a reader to match on them. A reader who
follows the pages writes code that does not compile.

Fix: name the surviving sentinel and its wrapped field text on each
line, read from the current source rather than inferred.

A gate for this class is attractive but needs care. A backticked error
name in a package page must appear in that package's lock, not anywhere
in the lock directory: one surviving name belongs to an unrelated
package and would mask the very drift the rule is for. Land the rule
only if it holds that package scope cleanly. Leave it out rather than
widen its match.

### Delivery agent tool names

The four delivery agent definitions named tool identifiers the harness
does not have, so each one failed to start. The delivery loop could not
run at all. The identifiers are now the harness's own names.

No gate is recommended. The tool vocabulary belongs to the harness, not
to this repository, so a gate here would pin a list this tree cannot
keep true. The failure is loud and immediate, which is the property a
gate would have provided.
