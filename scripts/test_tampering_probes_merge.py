#!/usr/bin/env python3
"""--probe cases for the merge stand-down, merge-aware comparison, and
the intersection key. Split out of test_tampering_probes_diffoverride.py
to keep both modules near the project's file-size discipline. See
docs/plans/test-tampering.md, "Merge commits" and "The intersection
key". Each case builds its own throwaway git repo; the end-to-end cases
install the real gate as that repo's own .git/hooks/commit-msg."""
import subprocess
import sys
from pathlib import Path

from check_test_tampering import finding_key
from test_tampering_diff import build_diff, commit_all, merge_head_revs
from test_tampering_override import resolve_overrides
from test_tampering_probes_merge_fixtures import (
    ASSERT_FILES,
    BASE_TESTS,
    INFRA_FILES,
    TT13_FILES,
    checker_path,
    drop_func,
    git,
    make_merge,
    new_repo,
    run_checker,
    two_parent_repo,
)
from test_tampering_rules import Finding


# --- merge stand-down, item 3 ----------------------------------------


def _conflict_repo(tmp, name, trunk_extra=None):
    """_conflict_repo builds a repo whose branch and trunk edit one
    test file's body differently, then starts the merge."""
    repo = new_repo(tmp, name)
    (repo / "a_test.go").write_text(_body_test("base"))
    (repo / "b_test.go").write_text(BASE_TESTS["b_test.go"])
    commit_all(repo, "base")
    trunk = git(repo, ["rev-parse", "--abbrev-ref", "HEAD"]).strip()
    git(repo, ["checkout", "-q", "-b", "feature"])
    (repo / "a_test.go").write_text(_body_test("branch"))
    commit_all(repo, "branch edit")
    git(repo, ["checkout", "-q", trunk])
    (repo / "a_test.go").write_text(_body_test("trunk"))
    if trunk_extra is not None:
        trunk_extra(repo)
    commit_all(repo, "trunk edit")
    subprocess.run(["git", "merge", "--no-ff", "feature"], cwd=repo, capture_output=True, text=True)
    return repo


def _body_test(marker: str) -> str:
    return (
        'package a\n\nimport "testing"\n\n'
        "func TestKeepMe(t *testing.T) {\n"
        f'\tt.Fatal("{marker}")\n'
        "}\n"
    )


def _probe_conflicted_worktree_skips(tmp):
    """An unresolved conflict must stand the gate down. The conflicted
    file holds marker text, which is not a tree any commit will hold,
    and every file beside it can only be measured against the first
    parent. See _probe_conflicted_cherry_pick_skips for the case that
    fires without item 3."""
    repo = _conflict_repo(tmp, "merge_conflicted")
    result = run_checker(repo)
    if result.returncode != 0 or "merge in progress; skipping" not in result.stdout:
        return [f"conflicted worktree: expected the skip note, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_resolved_merge_worktree_skips(tmp):
    """A conflict resolved in favour of the first parent leaves the
    working tree equal to HEAD. The stand-down must still fire, so the
    gate never falls through to the pre-merge commit. The trunk commit
    carries its own finding, so a fall-through would report it."""
    repo = _conflict_repo(tmp, "merge_resolved", trunk_extra=lambda r: (r / "b_test.go").unlink())
    git(repo, ["checkout", "--ours", "--", "a_test.go"])
    git(repo, ["add", "a_test.go"])
    result = run_checker(repo)
    if result.returncode != 0 or "merge in progress; skipping" not in result.stdout:
        return [f"resolved merge: expected the skip note, got {result.returncode} {result.stdout!r}"]
    return []


# --- end-to-end commit-msg hook cases --------------------------------


def _install_commit_msg_hook(repo: Path) -> None:
    """_install_commit_msg_hook writes the real gate into the throwaway
    repository's own .git/hooks directory. The project's hook guard
    permits this; core.hooksPath is never set."""
    hooks = repo / ".git" / "hooks"
    hooks.mkdir(parents=True, exist_ok=True)
    hook = hooks / "commit-msg"
    hook.write_text(f'#!/bin/sh\nexec "{sys.executable}" "{checker_path()}" --message-file "$1"\n')
    hook.chmod(0o755)


def _hook_merge_repo(tmp, name):
    """_hook_merge_repo builds the benign-merge fixture: a branch that
    drops one test with valid trailers, and a trunk commit that carries
    no aggregate finding."""
    reason = "dropped a duplicated case the table now covers"
    branch_message = (
        "drop a duplicated test\n\n"
        f"Allow-Test-Change: TT01 {reason}\n"
        f"Allow-Test-Change: TT04 {reason}\n"
    )
    repo, _trunk = two_parent_repo(
        tmp,
        name,
        BASE_TESTS,
        {"a_test.go": drop_func(BASE_TESTS["a_test.go"], "TestDropMe")},
        branch_message,
        {"README.md": "trunk note\n"},
        "trunk note",
    )
    _install_commit_msg_hook(repo)
    return repo


def _probe_merge_commit_msg_hook_benign_merge_commits(tmp):
    """git merge runs the commit-msg hook. A merge that introduces
    nothing must commit. Before the merge-aware comparison this failed
    with `Not committing merge`."""
    repo = _hook_merge_repo(tmp, "merge_hook_benign")
    merged = subprocess.run(
        ["git", "merge", "--no-ff", "-m", "merge feature", "feature"],
        cwd=repo, capture_output=True, text=True,
    )
    parents = git(repo, ["rev-list", "--parents", "--max-count=1", "HEAD"]).split()
    if merged.returncode != 0 or len(parents) != 3:
        return [f"benign merge hook: expected a two-parent merge commit, got {merged.stdout!r} {merged.stderr!r}"]
    return []


def _probe_merge_commit_msg_hook_evil_merge_blocks(tmp):
    """A merge that drops a test both parents kept must still block at
    the commit-msg hook."""
    repo = _hook_merge_repo(tmp, "merge_hook_evil")
    subprocess.run(["git", "merge", "--no-ff", "--no-commit", "feature"], cwd=repo, capture_output=True, text=True)
    (repo / "a_test.go").write_text(drop_func((repo / "a_test.go").read_text(), "TestKeepMe"))
    git(repo, ["add", "-A"])
    committed = subprocess.run(
        ["git", "commit", "-m", "merge feature"], cwd=repo, capture_output=True, text=True
    )
    output = committed.stdout + committed.stderr
    if committed.returncode == 0 or "TT01" not in output:
        return [f"evil merge hook: expected the commit to fail naming TT01, got {committed.returncode} {output!r}"]
    return []


def _probe_squash_merge_is_judged_whole(tmp):
    """git merge --squash writes no MERGE_HEAD, so the squash is judged
    against the whole branch. The branch trailers survive only through
    the default SQUASH_MSG; a rewritten message drops them."""
    reason = "dropped a duplicated case the table now covers"
    branch_message = (
        "drop a duplicated test\n\n"
        f"Allow-Test-Change: TT01 {reason}\n"
        f"Allow-Test-Change: TT04 {reason}\n"
    )
    repo, _trunk = two_parent_repo(
        tmp, "merge_squash", BASE_TESTS,
        {"a_test.go": drop_func(BASE_TESTS["a_test.go"], "TestDropMe")}, branch_message,
        {"README.md": "trunk note\n"}, "trunk note",
    )
    git(repo, ["merge", "--squash", "feature"])
    if merge_head_revs(repo):
        return ["squash merge: expected no MERGE_HEAD for a squash"]
    from check_test_tampering import run_rules

    findings = run_rules(build_diff(repo, ["--cached"]))
    ids = sorted({f.id for f in findings})
    if "TT01" not in ids or "TT04" not in ids:
        return [f"squash merge: expected TT01 and TT04 against the squashed index, got {ids}"]
    squash_msg = (repo / ".git" / "SQUASH_MSG").read_text()
    unresolved, _overridden = resolve_overrides(findings, squash_msg)
    if unresolved:
        return [f"squash merge: SQUASH_MSG must carry the branch trailers, got unresolved={unresolved}"]
    unresolved, _overridden = resolve_overrides(findings, "squash feature\n")
    if len(unresolved) != len(findings):
        return [f"squash merge: a rewritten message must drop every trailer, got unresolved={unresolved}"]
    return []


# --- finished merge commits, item 6 ----------------------------------


def _probe_merge_introducing_nothing_is_silent(tmp):
    """A merge commit that introduces nothing must report nothing. It
    used to re-judge the whole branch against the first parent."""
    repo, _trunk = two_parent_repo(
        tmp, "merge_benign", BASE_TESTS,
        {"a_test.go": drop_func(BASE_TESTS["a_test.go"], "TestDropMe")}, "drop a test",
        {"README.md": "trunk note\n"}, "trunk note",
    )
    make_merge(repo, {"a_test.go": drop_func(BASE_TESTS["a_test.go"], "TestDropMe"),
                             "b_test.go": BASE_TESTS["b_test.go"], "README.md": "trunk note\n"}, "merge")
    result = run_checker(repo)
    if result.returncode != 0 or "TT" in result.stdout:
        return [f"benign merge commit: expected silence, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_evil_merge_still_fires(tmp):
    """A merge that drops a test both parents kept must fire, even when
    the branch shifted that test's line number. A key holding the line
    reports nothing here."""
    shifted = (
        'package a\n\nimport "testing"\n\n'
        'func TestNew(t *testing.T) {\n\tt.Fatal("new")\n}\n\n' + BASE_TESTS["a_test.go"].split("\n\n", 2)[2]
    )
    repo, _trunk = two_parent_repo(
        tmp, "merge_evil_shift", BASE_TESTS,
        {"a_test.go": shifted}, "add a test above",
        {"README.md": "trunk note\n"}, "trunk note",
    )
    merged = drop_func(shifted, "TestKeepMe")
    make_merge(repo, {"a_test.go": merged, "b_test.go": BASE_TESTS["b_test.go"],
                             "README.md": "trunk note\n"}, "merge")
    result = run_checker(repo)
    if "TT01" not in result.stdout or "TestKeepMe" not in result.stdout:
        return [f"evil merge shift: expected TT01 for TestKeepMe, got {result.stdout!r}"]
    return []


def _probe_evil_merge_beside_waived_branch_removal(tmp):
    """The merge-introduced removal must be reported and the branch's
    own, already-waived removal must not."""
    branch_file = drop_func(BASE_TESTS["a_test.go"], "TestDropMe")
    repo, _trunk = two_parent_repo(
        tmp, "merge_evil_beside", BASE_TESTS,
        {"a_test.go": branch_file}, "drop a test",
        {"README.md": "trunk note\n"}, "trunk note",
    )
    make_merge(repo, {"a_test.go": drop_func(branch_file, "TestKeepMe"),
                             "b_test.go": BASE_TESTS["b_test.go"], "README.md": "trunk note\n"}, "merge")
    result = run_checker(repo)
    if "TestKeepMe" not in result.stdout or "TestDropMe" in result.stdout:
        return [f"evil merge beside waived: expected only TestKeepMe reported, got {result.stdout!r}"]
    return []


def _probe_merge_aggregate_tt04_survives(tmp):
    """TT04 reports at the first test file in diff order, so its path
    changes with the parent. The ID-only key keeps the finding."""
    repo, _trunk = two_parent_repo(
        tmp, "merge_tt04", ASSERT_FILES,
        {"a_test.go": ASSERT_FILES["a_test.go"].replace('\tt.Fatal("a1")\n', "").replace('\tt.Fatal("a2")\n', "")},
        "trim two assertions",
        {"README.md": "trunk note\n"}, "trunk note",
    )
    merged_a = ASSERT_FILES["a_test.go"].replace('\tt.Fatal("a1")\n', "").replace('\tt.Fatal("a2")\n', "")
    merged_z = ASSERT_FILES["z_test.go"].replace('\tt.Fatal("z1")\n', "").replace('\tt.Fatal("z2")\n', "")
    make_merge(repo, {"a_test.go": merged_a, "z_test.go": merged_z, "README.md": "trunk note\n"}, "merge")
    result = run_checker(repo)
    if "TT04" not in result.stdout:
        return [f"merge TT04: expected the aggregate finding to survive, got {result.stdout!r}"]
    return []


def _probe_merge_aggregate_tt11_survives(tmp):
    """TT11 reports at the first gate-infra file in diff order. The two
    parents must report different infra paths, or the case proves
    nothing."""
    repo, _trunk = two_parent_repo(
        tmp, "merge_tt11", INFRA_FILES,
        {"scripts/a_check.py": "print('a2')\n", "foo/a.go": "package foo\n\nvar A = 2\n"},
        "touch a gate and its caller",
        {"README.md": "trunk note\n"}, "trunk note",
    )
    merged = dict(INFRA_FILES)
    merged.update({
        "scripts/a_check.py": "print('a2')\n", "foo/a.go": "package foo\n\nvar A = 2\n",
        "scripts/z_check.py": "print('z2')\n", "foo/z.go": "package foo\n\nvar Z = 2\n",
        "README.md": "trunk note\n",
    })
    make_merge(repo, merged, "merge")
    result = run_checker(repo)
    if "TT11" not in result.stdout:
        return [f"merge TT11: expected the aggregate finding to survive, got {result.stdout!r}"]
    return []


def _probe_merge_tt13_is_not_aggregate(tmp):
    """TT13 keeps the per-file key. A benign merge whose parents lowered
    two different floors must stay silent."""
    repo, _trunk = two_parent_repo(
        tmp, "merge_tt13", TT13_FILES,
        {"scripts/mutation_denylist/a.json": '{"floor": 40, "denylist": []}\n'}, "lower the mutation floor",
        {"Makefile": TT13_FILES["Makefile"].replace("COVERAGE_FLOOR := 85", "COVERAGE_FLOOR := 80")},
        "lower the coverage floor",
    )
    merged = dict(TT13_FILES)
    merged["scripts/mutation_denylist/a.json"] = '{"floor": 40, "denylist": []}\n'
    merged["Makefile"] = TT13_FILES["Makefile"].replace("COVERAGE_FLOOR := 85", "COVERAGE_FLOOR := 80")
    make_merge(repo, merged, "merge")
    result = run_checker(repo)
    if "TT13" in result.stdout:
        return [f"merge TT13: expected the per-file key to keep it silent, got {result.stdout!r}"]
    return []


def _probe_merge_independent_aggregate_blocks(tmp):
    """Known, accepted limit: two parents carrying the same aggregate ID
    independently make a benign merge block. TT01 separates correctly;
    TT04 does not."""
    branch_file = drop_func(BASE_TESTS["a_test.go"], "TestDropMe")
    trunk_file = drop_func(BASE_TESTS["a_test.go"], "TestKeepMe")
    repo, _trunk = two_parent_repo(
        tmp, "merge_limit", BASE_TESTS,
        {"a_test.go": branch_file}, "drop one test",
        {"a_test.go": trunk_file}, "drop another test",
    )
    both = drop_func(branch_file, "TestKeepMe")
    make_merge(repo, {"a_test.go": both, "b_test.go": BASE_TESTS["b_test.go"]}, "merge")
    result = run_checker(repo)
    if "TT01" in result.stdout or "TT04" not in result.stdout:
        return [f"merge limit: expected TT01 silent and TT04 reported, got {result.stdout!r}"]
    return []


def _probe_octopus_merge_introducing_nothing_is_silent(tmp):
    """An octopus merge that introduces nothing must report nothing.
    MERGE_HEAD holds one revision per remaining parent."""
    repo, _trunk, branches = _octopus_repo(tmp, "merge_octopus_benign",
                                          {"a_test.go": drop_func(BASE_TESTS["a_test.go"], "TestDropMe")},
                                          {"README2.md": "second branch\n"})
    merged = dict(BASE_TESTS)
    merged["a_test.go"] = drop_func(BASE_TESTS["a_test.go"], "TestDropMe")
    merged["README2.md"] = "second branch\n"
    make_merge(repo, merged, "octopus merge", extra_parents=branches)
    result = run_checker(repo)
    if result.returncode != 0 or "TT" in result.stdout:
        return [f"octopus benign: expected silence, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_octopus_merge_evil_still_fires(tmp):
    """An octopus merge that drops a test every parent kept must fire."""
    repo, _trunk, branches = _octopus_repo(tmp, "merge_octopus_evil",
                                          {"README1.md": "first branch\n"},
                                          {"README2.md": "second branch\n"})
    merged = dict(BASE_TESTS)
    merged["a_test.go"] = drop_func(BASE_TESTS["a_test.go"], "TestKeepMe")
    merged["README1.md"] = "first branch\n"
    merged["README2.md"] = "second branch\n"
    make_merge(repo, merged, "octopus merge", extra_parents=branches)
    result = run_checker(repo)
    if "TT01" not in result.stdout or "TestKeepMe" not in result.stdout:
        return [f"octopus evil: expected TT01 for TestKeepMe, got {result.stdout!r}"]
    return []


def _octopus_repo(tmp, name, first_files, second_files):
    """_octopus_repo builds a trunk plus two branches for a three-parent
    merge."""
    repo = new_repo(tmp, name)
    for path, text in BASE_TESTS.items():
        (repo / path).write_text(text)
    commit_all(repo, "base")
    trunk = git(repo, ["rev-parse", "--abbrev-ref", "HEAD"]).strip()
    branches = []
    for index, files in enumerate((first_files, second_files), start=1):
        git(repo, ["checkout", "-q", "-b", f"feature{index}", trunk])
        for path, text in files.items():
            (repo / path).write_text(text)
        commit_all(repo, f"branch {index}")
        branches.append(git(repo, ["rev-parse", "HEAD"]).strip())
        git(repo, ["checkout", "-q", trunk])
    return repo, trunk, branches


# --- the intersection key --------------------------------------------


def _probe_merge_key_normalizes_digits(tmp):
    """Two findings differing only in digits share one key: a count is
    relative to the parent being compared."""
    left = Finding("TT01", "a_test.go", 1, "test function removed at 12 sites")
    right = Finding("TT01", "a_test.go", 9, "test function removed at 345 sites")
    if finding_key(left) != finding_key(right):
        return [f"key digits: expected equal keys, got {finding_key(left)} {finding_key(right)}"]
    return []


def _probe_merge_key_aggregate_ids_use_id_alone(tmp):
    """TT04 and TT11 key on the ID alone; TT13 keeps the per-file key."""
    tt04 = [Finding("TT04", "a_test.go", 0, "assertion sites decreased net (4 removed, 0 added)"),
            Finding("TT04", "z_test.go", 0, "assertion sites decreased net (2 removed, 0 added)")]
    if finding_key(tt04[0]) != finding_key(tt04[1]):
        return [f"key aggregate TT04: expected equal keys, got {finding_key(tt04[0])} {finding_key(tt04[1])}"]
    tt11 = [Finding("TT11", "scripts/a_check.py", 1, "gate-infra change paired with a non-infra file change"),
            Finding("TT11", "scripts/z_check.py", 1, "gate-infra change paired with a non-infra file change")]
    if finding_key(tt11[0]) != finding_key(tt11[1]):
        return [f"key aggregate TT11: expected equal keys, got {finding_key(tt11[0])} {finding_key(tt11[1])}"]
    tt13 = [Finding("TT13", "Makefile", 1, "COVERAGE_FLOOR lowered (85 -> 80)"),
            Finding("TT13", "scripts/mutation_denylist/a.json", 1, "mutation floor lowered (50 -> 40)")]
    if finding_key(tt13[0]) == finding_key(tt13[1]):
        return [f"key TT13: expected distinct keys, got {finding_key(tt13[0])}"]
    return []


def _probe_merge_key_keeps_distinct_findings_distinct(tmp):
    """Two removals in one file stay two findings: the key holds the
    message, which names the function."""
    left = Finding("TT01", "a_test.go", 1, "test function TestOne removed with no matching move")
    right = Finding("TT01", "a_test.go", 1, "test function TestTwo removed with no matching move")
    if finding_key(left) == finding_key(right):
        return [f"key distinct: expected different keys, got {finding_key(left)}"]
    return []


_B_WITH_DROP = (
    'package a\n\nimport "testing"\n\n'
    "func TestOther(t *testing.T) {\n"
    '\tt.Fatal("other one")\n'
    "}\n\n"
    "func TestDropMe(t *testing.T) {\n"
    '\tt.Fatal("drop one")\n'
    '\tt.Fatal("drop two")\n'
    "}\n"
)


def _probe_conflicted_cherry_pick_skips(tmp):
    """A conflicted cherry-pick sets CHERRY_PICK_HEAD, never MERGE_HEAD,
    so only the unmerged-path half of the stand-down catches it. The
    cleanly applied second file carries the picked commit's own test
    deletion, which item 4 could not waive."""
    repo = new_repo(tmp, "cherry_pick_conflict")
    (repo / "a_test.go").write_text(_body_test("base"))
    (repo / "b_test.go").write_text(_B_WITH_DROP)
    commit_all(repo, "base")
    trunk = git(repo, ["rev-parse", "--abbrev-ref", "HEAD"]).strip()
    git(repo, ["checkout", "-q", "-b", "feature"])
    (repo / "a_test.go").write_text(_body_test("branch"))
    (repo / "b_test.go").write_text(drop_func(_B_WITH_DROP, "TestDropMe"))
    commit_all(repo, "branch edit")
    git(repo, ["checkout", "-q", trunk])
    (repo / "a_test.go").write_text(_body_test("trunk"))
    commit_all(repo, "trunk edit")
    subprocess.run(["git", "cherry-pick", "feature"], cwd=repo, capture_output=True, text=True)
    if merge_head_revs(repo):
        return ["cherry-pick conflict: expected no MERGE_HEAD for a cherry-pick"]
    result = run_checker(repo)
    if result.returncode != 0 or "merge in progress; skipping" not in result.stdout:
        return [f"cherry-pick conflict: expected the skip note, got {result.returncode} {result.stdout!r}"]
    return []


def run_merge_probes(tmp) -> list:
    """run_merge_probes runs every merge stand-down, merge-aware
    comparison, and intersection-key case against tmp."""
    problems = []
    for fn in (
        _probe_conflicted_worktree_skips,
        _probe_resolved_merge_worktree_skips,
        _probe_conflicted_cherry_pick_skips,
        _probe_merge_commit_msg_hook_benign_merge_commits,
        _probe_merge_commit_msg_hook_evil_merge_blocks,
        _probe_squash_merge_is_judged_whole,
        _probe_merge_introducing_nothing_is_silent,
        _probe_evil_merge_still_fires,
        _probe_evil_merge_beside_waived_branch_removal,
        _probe_merge_aggregate_tt04_survives,
        _probe_merge_aggregate_tt11_survives,
        _probe_merge_tt13_is_not_aggregate,
        _probe_merge_independent_aggregate_blocks,
        _probe_octopus_merge_introducing_nothing_is_silent,
        _probe_octopus_merge_evil_still_fires,
        _probe_merge_key_normalizes_digits,
        _probe_merge_key_aggregate_ids_use_id_alone,
        _probe_merge_key_keeps_distinct_findings_distinct,
    ):
        problems.extend(fn(tmp))
    return problems
