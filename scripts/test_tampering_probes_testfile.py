#!/usr/bin/env python3
"""--probe cases for TT01-TT08, the *_test.go rules. Each case builds
its own throwaway git repo (see test_tampering_diff.py's probe-repo
helpers): one violation commit that must fire the ID, one clean
commit that must stay silent."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_rules import (
    check_assertion_decrease,
    check_build_tag_changed,
    check_comparison_dropped_for_bare_err,
    check_moved_or_dropped,
    check_numeric_literal_increase,
    check_parallel_removed,
    check_result_discarded,
    check_skip_added,
)


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


# --- TT04: net decrease in assertion sites -----------------------------


def _probe_tt04_violation(tmp):
    repo = _new_repo(tmp, "tt04_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif !cond1 {\n\t\tt.Error("one")\n\t}\n'
        '\tif !cond2 {\n\t\tt.Error("two")\n\t}\n'
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            '\tif !cond1 {\n\t\tt.Error("one")\n\t}\n'
            "}\n"
        ),
    )
    if not _has(check_assertion_decrease(diffs), "TT04"):
        return ["tt04 violation: expected TT04 for a net assertion decrease"]
    return []


def _probe_tt04_clean_helper_extraction(tmp):
    repo = _new_repo(tmp, "tt04_clean")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif !cond1 {\n\t\tt.Error("one")\n\t}\n'
        '\tif !cond2 {\n\t\tt.Error("two")\n\t}\n'
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            '\tif !cond1 {\n\t\tt.Error("one")\n\t}\n'
            "\trunCases(t)\n"
            "}\n\n"
            "func TestBar(t *testing.T) {\n"
            "\trunCases(t)\n"
            "}\n"
        ),
    )
    findings = check_assertion_decrease(diffs)
    if _has(findings, "TT04"):
        return [f"tt04 clean helper extraction: unexpected TT04: {findings}"]
    return []


# --- TT05: a non-error comparison replaced by a bare err check --------


def _probe_tt05_violation(tmp):
    repo = _new_repo(tmp, "tt05_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif got != want {\n\t\tt.Fatal("mismatch")\n\t}\n'
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n"
            "}\n"
        ),
    )
    if not _has(check_comparison_dropped_for_bare_err(diffs), "TT05"):
        return ["tt05 violation: expected TT05 for a comparison replaced by a bare err check"]
    return []


def _probe_tt05_clean_new_check(tmp):
    repo = _new_repo(tmp, "tt05_clean")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif got != want {\n\t\tt.Fatal("mismatch")\n\t}\n'
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            '\tif got != want {\n\t\tt.Fatal("mismatch")\n\t}\n'
            "\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n"
            "}\n"
        ),
    )
    findings = check_comparison_dropped_for_bare_err(diffs)
    if _has(findings, "TT05"):
        return [f"tt05 clean new check: unexpected TT05: {findings}"]
    return []


# --- TT06: a checked result now discarded ------------------------------


def _probe_tt06_violation(tmp):
    repo = _new_repo(tmp, "tt06_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        "\tv, err := f()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = v\n"
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\t_, _ = f()\n"
            "}\n"
        ),
    )
    if not _has(check_result_discarded(diffs), "TT06"):
        return ["tt06 violation: expected TT06 for a discarded, previously checked result"]
    return []


def _probe_tt06_clean_rename(tmp):
    repo = _new_repo(tmp, "tt06_clean")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        "\tv, err := f()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = v\n"
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\tresult, err := f()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = result\n"
            "}\n"
        ),
    )
    findings = check_result_discarded(diffs)
    if _has(findings, "TT06"):
        return [f"tt06 clean rename: unexpected TT06: {findings}"]
    return []


# --- TT07: a tolerance/timeout literal increased -----------------------


def _probe_tt07_violation(tmp):
    repo = _new_repo(tmp, "tt07_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
        "func TestFoo(t *testing.T) {\n\ttime.Sleep(10 * time.Millisecond)\n}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
            "func TestFoo(t *testing.T) {\n\ttime.Sleep(100 * time.Millisecond)\n}\n"
        ),
    )
    if not _has(check_numeric_literal_increase(diffs), "TT07"):
        return ["tt07 violation: expected TT07 for an increased sleep literal"]
    return []


def _probe_tt07_clean_decrease(tmp):
    repo = _new_repo(tmp, "tt07_clean")
    (repo / "a_test.go").write_text(
        'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
        "func TestFoo(t *testing.T) {\n\ttime.Sleep(100 * time.Millisecond)\n}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
            "func TestFoo(t *testing.T) {\n\ttime.Sleep(10 * time.Millisecond)\n}\n"
        ),
    )
    findings = check_numeric_literal_increase(diffs)
    if _has(findings, "TT07"):
        return [f"tt07 clean decrease: unexpected TT07: {findings}"]
    return []


# --- TT08: a removed t.Parallel() with no matching addition -----------


def _probe_tt08_violation(tmp):
    repo = _new_repo(tmp, "tt08_violation")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n\tt.Parallel()\n\tdoWork()\n}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\nfunc TestFoo(t *testing.T) {\n\tdoWork()\n}\n'
        ),
    )
    if not _has(check_parallel_removed(diffs), "TT08"):
        return ["tt08 violation: expected TT08 for a removed t.Parallel()"]
    return []


def _probe_tt08_clean_unrelated_edit(tmp):
    repo = _new_repo(tmp, "tt08_clean")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n\tt.Parallel()\n\tdoWork()\n}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n\tt.Parallel()\n\tdoOtherWork()\n}\n"
        ),
    )
    findings = check_parallel_removed(diffs)
    if _has(findings, "TT08"):
        return [f"tt08 clean unrelated edit: unexpected TT08: {findings}"]
    return []


def run_testfile_probes(tmp) -> list:
    """run_testfile_probes runs every TT01-TT08 case against tmp."""
    problems = []
    for fn in (
        _probe_tt01_violation,
        _probe_tt01_clean_move,
        _probe_tt02_violation,
        _probe_tt02_clean_message_change,
        _probe_tt03_violation,
        _probe_tt03_clean_unchanged_tag,
        _probe_tt04_violation,
        _probe_tt04_clean_helper_extraction,
        _probe_tt05_violation,
        _probe_tt05_clean_new_check,
        _probe_tt06_violation,
        _probe_tt06_clean_rename,
        _probe_tt07_violation,
        _probe_tt07_clean_decrease,
        _probe_tt08_violation,
        _probe_tt08_clean_unrelated_edit,
    ):
        problems.extend(fn(tmp))
    return problems
