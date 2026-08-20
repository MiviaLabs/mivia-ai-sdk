#!/usr/bin/env python3
"""--probe cases for TT01-TT03, the function-collection and go:build
*_test.go rules. TT04-TT08 live in
test_tampering_probes_testfile_assert.py; see that file's header for
why this is split. Each case builds its own throwaway git repo (see
test_tampering_diff.py's probe-repo helpers): one violation commit
that must fire the ID, one clean commit that must stay silent."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_probes_testfile_assert import run_testfile_assert_probes
from test_tampering_rules import check_build_tag_changed, check_moved_or_dropped, check_skip_added


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


def _has(findings, tid):
    return any(f.id == tid for f in findings)


# --- TT01: function moved or dropped ----------------------------------


def _probe_tt01_violation(tmp):
    repo = _new_repo(tmp, "tt01_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n\tif 1 != 2 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(repo, lambda r: (r / "a_test.go").write_text('package a\n\nimport "testing"\n'))
    if not _has(check_moved_or_dropped(diffs), "TT01"):
        return ["tt01 violation: expected TT01 for a dropped test function"]
    return []


def _probe_tt01_clean_move(tmp):
    repo = _new_repo(tmp, "tt01_move")
    body = 'func TestFoo(t *testing.T) {\n\tif 1 != 2 {\n\t\tt.Fatal("bad")\n\t}\n}\n'
    (repo / "a_test.go").write_text(f'package a\n\nimport "testing"\n\n{body}')
    commit_all(repo, "base")

    def mutate(r):
        (r / "a_test.go").write_text('package a\n\nimport "testing"\n')
        (r / "b_test.go").write_text(f'package a\n\nimport "testing"\n\n{body}')

    diffs = diff_after_change(repo, mutate)
    findings = check_moved_or_dropped(diffs)
    if _has(findings, "TT01"):
        return [f"tt01 clean move: unexpected TT01: {findings}"]
    return []


def _probe_tt01_violation_move_and_rename(tmp):
    """A cross-file move that ALSO renames the function still fires
    TT01, by design: the hash covers the func-declaration line (name
    included), so a rename never "reappears" under the plan's own
    hash-equality definition of a move, even to another conforming
    name. This locks in that a rename-during-move needs an
    Allow-Test-Change trailer, distinct from the untouched-name move
    in _probe_tt01_clean_move above."""
    repo = _new_repo(tmp, "tt01_move_rename")
    body_stmts = '\tif 1 != 2 {\n\t\tt.Fatal("bad")\n\t}\n'
    (repo / "a_test.go").write_text(
        f'package a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {{\n{body_stmts}}}\n'
    )
    commit_all(repo, "base")

    def mutate(r):
        (r / "a_test.go").write_text('package a\n\nimport "testing"\n')
        (r / "b_test.go").write_text(
            f'package a\n\nimport "testing"\n\nfunc TestFooV2(t *testing.T) {{\n{body_stmts}}}\n'
        )

    diffs = diff_after_change(repo, mutate)
    if not _has(check_moved_or_dropped(diffs), "TT01"):
        return ["tt01 violation move and rename: expected TT01, since a rename changes the hashed body"]
    return []


# --- TT02: a new skip, unmatched by a removal in the same hunk --------


def _probe_tt02_violation(tmp):
    repo = _new_repo(tmp, "tt02_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {\n\tdoWork()\n}\n'
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            'func TestFoo(t *testing.T) {\n\tt.Skip("slow")\n\tdoWork()\n}\n'
        ),
    )
    if not _has(check_skip_added(diffs), "TT02"):
        return ["tt02 violation: expected TT02 for an unmatched new skip"]
    return []


def _probe_tt02_violation_other_patterns(tmp):
    """Three of the four _SKIP_PATTERNS entries beyond t.Skip( must
    each independently fire TT02: t.SkipNow(, t.Skipf(, and the
    testing.Short() short-mode guard."""
    problems = []
    for suffix, added_line in (
        ("skipnow", "\tt.SkipNow()\n"),
        ("skipf", '\tt.Skipf("slow: %s", reason)\n'),
        ("short", "\tif testing.Short() {\n\t\tt.Skip(\"slow\")\n\t}\n"),
    ):
        repo = _new_repo(tmp, f"tt02_violation_{suffix}")
        (repo / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {\n\tdoWork()\n}\n'
        )
        commit_all(repo, "base")
        diffs = diff_after_change(
            repo,
            lambda r, added_line=added_line: (r / "a_test.go").write_text(
                'package a\n\nimport "testing"\n\n'
                f"func TestFoo(t *testing.T) {{\n{added_line}\tdoWork()\n}}\n"
            ),
        )
        if not _has(check_skip_added(diffs), "TT02"):
            problems.append(f"tt02 violation {suffix}: expected TT02 for {added_line.strip()!r}")
    return problems


def _probe_tt02_clean_message_change(tmp):
    repo = _new_repo(tmp, "tt02_clean")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        'func TestFoo(t *testing.T) {\n\tt.Skip("old reason")\n\tdoWork()\n}\n'
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            'func TestFoo(t *testing.T) {\n\tt.Skip("new reason")\n\tdoWork()\n}\n'
        ),
    )
    findings = check_skip_added(diffs)
    if _has(findings, "TT02"):
        return [f"tt02 clean message change: unexpected TT02: {findings}"]
    return []


# --- TT03: a go:build line added or changed ----------------------------


def _probe_tt03_violation(tmp):
    repo = _new_repo(tmp, "tt03_violation")
    (repo / "a_test.go").write_text('package a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {}\n')
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            '//go:build sometag\n\npackage a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {}\n'
        ),
    )
    if not _has(check_build_tag_changed(diffs), "TT03"):
        return ["tt03 violation: expected TT03 for a newly added go:build line"]
    return []


def _probe_tt03_violation_changed_tag(tmp):
    """An existing go:build expression changed, not merely added where
    none existed, is the other documented TT03 sub-case."""
    repo = _new_repo(tmp, "tt03_violation_changed")
    (repo / "a_test.go").write_text(
        '//go:build oldtag\n\npackage a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {}\n'
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            '//go:build newtag\n\npackage a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {}\n'
        ),
    )
    if not _has(check_build_tag_changed(diffs), "TT03"):
        return ["tt03 violation changed tag: expected TT03 for a changed go:build expression"]
    return []


def _probe_tt03_clean_unchanged_tag(tmp):
    repo = _new_repo(tmp, "tt03_clean")
    (repo / "a_test.go").write_text(
        '//go:build sometag\n\npackage a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {\n\tx := 1\n\t_ = x\n}\n'
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            '//go:build sometag\n\npackage a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {\n\tx := 2\n\t_ = x\n}\n'
        ),
    )
    findings = check_build_tag_changed(diffs)
    if _has(findings, "TT03"):
        return [f"tt03 clean unchanged tag: unexpected TT03: {findings}"]
    return []


def run_testfile_probes(tmp) -> list:
    """run_testfile_probes runs every TT01-TT08 case against tmp: the
    TT01-TT03 cases here, plus TT04-TT08 from
    test_tampering_probes_testfile_assert."""
    problems = []
    for fn in (
        _probe_tt01_violation,
        _probe_tt01_clean_move,
        _probe_tt01_violation_move_and_rename,
        _probe_tt02_violation,
        _probe_tt02_violation_other_patterns,
        _probe_tt02_clean_message_change,
        _probe_tt03_violation,
        _probe_tt03_violation_changed_tag,
        _probe_tt03_clean_unchanged_tag,
    ):
        problems.extend(fn(tmp))
    problems.extend(run_testfile_assert_probes(tmp))
    return problems
