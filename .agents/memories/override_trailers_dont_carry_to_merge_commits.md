---
id: override_trailers_dont_carry_to_merge_commits
title: A merge used to re-judge the whole branch; the gate now reports only what the merge introduced
content: A merge re-fired every finding a branch commit had already waived, because git runs the commit-msg hook on a merge and the gate compared the index to the first parent only. The gate now compares against every parent and intersects. A merge introducing nothing needs no trailer, except when two or more parents independently carry the same TT04 or TT11 finding. A squash commit is not a merge: it is judged against the whole branch, and it keeps the branch trailers only if the author keeps the default SQUASH_MSG.
importance: medium
tags: gates, test-tampering, git, merge
---

## What was true

`git merge` runs the `commit-msg` hook. The gate therefore judged a
merge at creation time, while `HEAD` was still the first parent. It
compared the index to `HEAD` alone, so it saw the whole merged branch
rather than the merge. A branch commit's `Allow-Test-Change` or
`Allow-Gate-Change` trailer does not carry to the merge commit, so
already-waived content fired again and the merge would not commit.

The same first-parent mistake appeared after the fact. A clean checkout
whose `HEAD` was a merge fell back to `git diff HEAD~1 HEAD`, which on
a merge is again the whole branch.

This happened at least three times in one session. A
nested-package-visibility change merged with `Allow-Gate-Change: TT11`
re-fired. The `Calibrated.Observe` pairing fix's merge re-fired `TT01`
twice and `TT05`. Merge 79ada64 made `make verify` red on a clean
checkout of `main`.

The advice then was to re-verify the merge diff and re-issue the same
trailer on the merge commit. That advice is obsolete for a true merge.

## What changed

The gate is merge-aware in both places.

At merge creation, when `MERGE_HEAD` exists, it compares the index
against every parent and reports only the intersection.
`git diff --cached` gives the first parent. `git diff --cached
MERGE_HEAD` gives the rest, one per line of that file.

On a merge commit that already exists, it compares `HEAD` against every
parent and intersects the same way. A merge introduces exactly the
content that differs from every parent. Content from any one parent was
already gated by that parent's own `commit-msg` run.

The gate also stands down while a merge is unfinished. `MERGE_HEAD`
present, or any unmerged path, means skip and exit 0. Either condition
alone is enough, and the unmerged-path half is the only one a conflicted
cherry-pick trips. No honest comparison exists until the merge is
committed. A conflicted file holds marker text, which is not a tree any
commit will hold, and every file beside it can only be measured against
the first parent.

The same change made the gate audit the working tree. A bare run with a
dirty tree used to audit the previous commit and exit 0. It now runs
`git diff HEAD`. See `docs/history/test-tampering.md`, "Diff resolution
order" and "Merge commits".

## What to do now

A merge that introduces nothing needs no trailer. Do not repeat a
branch commit's override trailer on a merge. Do not reach for a hook
bypass flag such as `--no-verify` when a merge is blocked.

There is one exception, and it is measured, not theoretical. `TT04` and
`TT11` report once per whole diff, at a path that depends on diff order,
so the gate matches them across parents by ID alone. When two or more
parents independently carry the same one of those two IDs, it survives
the intersection and a benign merge blocks. Reproduced: a branch dropped
one test and the trunk dropped a different one, each already waived;
`TT01` went silent for both, `TT04` did not, and the merge failed. The
fix is one trailer on the merge naming that ID. Read the diff first.
This over-reports rather than under-reports, which is the safe
direction, so it is accepted rather than engineered away.

A squash commit is not a merge. `git merge --squash` sets no
`MERGE_HEAD`, so the gate judges the squash commit against the whole
branch, and that is correct: the squash really does introduce all of
that content as one commit with no parent link to the branch.

The trailers usually survive anyway. Git copies every squashed commit
message into `SQUASH_MSG`, indented four spaces, and the trailer parser
strips each line before matching, so the branch's trailers still apply
if you accept the default message. Measured: two `Allow-Test-Change`
trailers read from `SQUASH_MSG` cleared both findings.

So the obligation is conditional. Keep the default `SQUASH_MSG` and you
need add nothing. Rewrite the message, or use `git commit -m`, and you
must repeat every trailer the squashed commits carried, because
rewriting drops them all.

If the gate fires on a true merge outside the aggregate exception, the
merge itself introduced that content. That is an evil merge: a change
present in no parent. Read the diff before you write any trailer.

Read an aggregate finding's numbers with care. `TT04` and `TT11` print
the first parent's counts and path, which can overstate what the merge
alone introduced. They say a real finding survived, not how large it is.

`make verify` on a dirty tree can now fail where it once passed. That
is the gate working, not a regression. Exit 0 used to mean "the
previous commit was clean". It now means "your work is clean". A
waivable finding on uncommitted work cannot be waived, because trailers
live in commit messages. Commit with the trailer, then re-run
`make verify`. A bare `git commit --amend` will not add a forgotten
trailer, because nothing is staged; re-stage the change and amend, or
reset and re-commit.

See [[positive_control_reachability]] for the general principle that an
override must be independently verified, never taken on the builder's
word.
