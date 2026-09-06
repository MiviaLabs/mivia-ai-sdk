#!/usr/bin/env python3
"""--probe cases for the dirty working tree, item 4 of the plan's diff
resolution order. Split out of test_tampering_probes_diffoverride.py to
keep both modules near the project's file-size discipline. See
docs/plans/test-tampering.md, "Why the working tree comes fourth". Each
case builds its own throwaway git repo and runs the real gate as a
subprocess inside it."""
import subprocess
import sys
from pathlib import Path

from test_tampering_diff import commit_all, init_probe_repo

_CHECKER = Path(__file__).resolve().parent / "check_test_tampering.py"


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


# Item 4 of the plan's resolution order: a bare run audits the working
# tree, never the previous commit. See docs/plans/test-tampering.md,
# "Why the working tree comes fourth".

_KEEP_ME = (
    'package a\n\nimport "testing"\n\n'
    "func TestKeepMe(t *testing.T) {\n"
    '\tt.Fatal("one")\n'
    '\tt.Fatal("two")\n'
    "\tdoWork()\n"
    "}\n"
)

_ADDED_LATER = _KEEP_ME + (
    "\nfunc TestAddedLater(t *testing.T) {\n"
    '\tt.Fatal("three")\n'
    "}\n"
)


def _run_checker(repo, args=()):
    """_run_checker runs the gate as a subprocess inside repo."""
    return subprocess.run([sys.executable, str(_CHECKER), *args], cwd=repo, capture_output=True, text=True)


def _repo_with_added_test(tmp, name, second_message="add a test"):
    """_repo_with_added_test builds a repo whose second commit adds a
    test function beside an existing one."""
    repo = _new_repo(tmp, name)
    (repo / "a_test.go").write_text(_KEEP_ME)
    commit_all(repo, "base")
    (repo / "a_test.go").write_text(_ADDED_LATER)
    commit_all(repo, second_message)
    return repo


def _probe_dirty_worktree_is_audited(tmp):
    """An unstaged deletion must be audited. Before the working-tree
    step existed this printed nothing and exited 0."""
    repo = _repo_with_added_test(tmp, "worktree_dirty")
    (repo / "a_test.go").unlink()
    result = _run_checker(repo)
    if result.returncode != 1 or "TT01" not in result.stdout or "TestAddedLater" not in result.stdout:
        return [f"dirty worktree: expected exit 1 with a TT01 line, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_dirty_worktree_unstaged_edit_is_audited(tmp):
    """An unstaged edit, not only an unstaged deletion, must be read.
    Removing two t.Fatal( lines and adding nothing is a net assertion
    decrease with no helper signal, so TT04 must fire."""
    repo = _new_repo(tmp, "worktree_unstaged_edit")
    (repo / "a_test.go").write_text(_KEEP_ME)
    commit_all(repo, "base")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\nfunc TestKeepMe(t *testing.T) {\n\tdoWork()\n}\n'
    )
    result = _run_checker(repo)
    if result.returncode != 1 or "TT04" not in result.stdout:
        return [f"dirty worktree edit: expected TT04 from the unstaged edit, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_dirty_worktree_finding_cannot_be_waived(tmp):
    """The working-tree step carries no message source, so a trailer in
    the tip commit's message cannot waive an uncommitted finding."""
    reason = "removed a duplicated test the refactor made obsolete"
    repo = _repo_with_added_test(tmp, "worktree_no_waiver", f"add a test\n\nAllow-Test-Change: TT01 {reason}\n")
    (repo / "a_test.go").unlink()
    result = _run_checker(repo)
    if result.returncode != 1 or "TT01" not in result.stdout or "overridden" in result.stdout:
        return [f"dirty worktree waiver: expected the finding to block, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_untracked_test_file_is_an_accepted_limit(tmp):
    """An untracked file is invisible to `git diff HEAD`. The gate is
    read-only and never writes the index to see it, so this limit is
    accepted. See "Accepted limit: untracked files"."""
    repo = _new_repo(tmp, "worktree_untracked")
    (repo / "a_test.go").write_text(_KEEP_ME)
    commit_all(repo, "base")
    (repo / "b_test.go").write_text(
        'package a\n\nimport "testing"\n\nfunc TestNew(t *testing.T) {\n\tt.Skip("slow")\n}\n'
    )
    result = _run_checker(repo)
    if result.returncode != 0:
        return [f"untracked limit: expected exit 0, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_clean_tree_still_audits_tip_commit(tmp):
    """The working-tree step must not displace the tip-commit step. A
    clean checkout still audits HEAD against its parent."""
    repo = _new_repo(tmp, "worktree_clean_tip")
    (repo / "a_test.go").write_text(_KEEP_ME)
    commit_all(repo, "base")
    (repo / "a_test.go").unlink()
    commit_all(repo, "drop the test")
    result = _run_checker(repo)
    if result.returncode != 1 or "TT01" not in result.stdout or "TestKeepMe" not in result.stdout:
        return [f"clean tree tip commit: expected the tip commit's TT01, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_dirty_worktree_modified_file_reads_new_text(tmp):
    """A working-tree comparison has no blob for the new side, so the
    new text must come from the file. Without that read every function
    in a modified file looks removed and TT01 fires on a benign edit."""
    repo = _new_repo(tmp, "worktree_new_text")
    (repo / "a_test.go").write_text(_KEEP_ME)
    commit_all(repo, "base")
    (repo / "a_test.go").write_text(
        _KEEP_ME + '\nfunc TestExtra(t *testing.T) {\n\tt.Fatal("extra")\n}\n'
    )
    result = _run_checker(repo)
    if result.returncode != 0 or "TT01" in result.stdout:
        return [f"worktree new text: expected silence on a benign edit, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_staged_deletion_ignores_the_worktree_file(tmp):
    """A staged deletion whose file still sits on disk must still read
    as a deletion. The `status != "D"` guard keeps _read_worktree out of
    an index comparison; without it the new text comes back full and
    TT01 is silently lost."""
    repo = _new_repo(tmp, "staged_deletion_on_disk")
    (repo / "a_test.go").write_text(_KEEP_ME)
    commit_all(repo, "base")
    subprocess.run(["git", "rm", "-q", "--cached", "a_test.go"], cwd=repo, check=True)
    msg_file = tmp / "staged_deletion_on_disk_msg.txt"
    msg_file.write_text("drop a test file\n")
    result = _run_checker(repo, ["--message-file", str(msg_file)])
    if result.returncode != 1 or "TT01" not in result.stdout:
        return [f"staged deletion on disk: expected TT01, got {result.returncode} {result.stdout!r}"]
    return []


def run_worktree_probes(tmp) -> list:
    """run_worktree_probes runs every dirty-working-tree case."""
    problems = []
    for fn in (
        _probe_dirty_worktree_is_audited,
        _probe_dirty_worktree_unstaged_edit_is_audited,
        _probe_dirty_worktree_finding_cannot_be_waived,
        _probe_untracked_test_file_is_an_accepted_limit,
        _probe_clean_tree_still_audits_tip_commit,
        _probe_dirty_worktree_modified_file_reads_new_text,
        _probe_staged_deletion_ignores_the_worktree_file,
    ):
        problems.extend(fn(tmp))
    return problems
