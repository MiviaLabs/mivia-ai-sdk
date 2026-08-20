#!/usr/bin/env python3
"""--probe cases for TT04-TT05, the assertion/comparison *_test.go
rules. Split from test_tampering_probes_testfile.py (which keeps
TT01-TT03) to stay within this repo's file-size convention. TT06-TT08
live in test_tampering_probes_testfile_discard.py for the same reason;
see docs/plans/test-tampering.md."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_probes_testfile_discard import run_testfile_discard_probes
from test_tampering_rules import check_assertion_decrease, check_comparison_dropped_for_bare_err


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


def _has(findings, tid):
    return any(f.id == tid for f in findings)


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


def _probe_tt04_clean_new_test_func_only(tmp):
    """new_test_func alone, with no helper-suffix or assert-call
    signal in the diff, must independently suppress TT04."""
    repo = _new_repo(tmp, "tt04_clean_new_func_only")
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
            "}\n\n"
            "func TestBar(t *testing.T) {\n\tdoWork()\n}\n"
        ),
    )
    findings = check_assertion_decrease(diffs)
    if _has(findings, "TT04"):
        return [f"tt04 clean new test func only: unexpected TT04: {findings}"]
    return []


def _probe_tt04_clean_helper_suffix_only(tmp):
    """A _HELPER_SUFFIXES match (Cases(/Suite(/RunAll() alone, with no
    new test function and no assert-prefixed call, must independently
    suppress TT04."""
    repo = _new_repo(tmp, "tt04_clean_suffix_only")
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
            "\trunValidateCases(t)\n"
            "}\n"
        ),
    )
    findings = check_assertion_decrease(diffs)
    if _has(findings, "TT04"):
        return [f"tt04 clean helper suffix only: unexpected TT04: {findings}"]
    return []


def _probe_tt04_clean_assert_call_only(tmp):
    """An assert*(-prefixed call alone, with no new test function and
    no _HELPER_SUFFIXES match, must independently suppress TT04: this
    is the third, previously unprobed suppression signal."""
    repo = _new_repo(tmp, "tt04_clean_assert_only")
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
            "\tassertNoLeak(t, obj)\n"
            "}\n"
        ),
    )
    findings = check_assertion_decrease(diffs)
    if _has(findings, "TT04"):
        return [f"tt04 clean assert call only: unexpected TT04: {findings}"]
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


def _probe_tt05_violation_equals_variant(tmp):
    """The `==` comparison must fire TT05 too, not just `!=`: the
    regex's `(==|!=)` alternation has a second, unprobed side."""
    repo = _new_repo(tmp, "tt05_violation_equals")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif got == want {\n\t\tt.Fatal("unexpected match")\n\t}\n'
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\tif err == nil {\n\t\tt.Fatal(err)\n\t}\n"
            "}\n"
        ),
    )
    if not _has(check_comparison_dropped_for_bare_err(diffs), "TT05"):
        return ["tt05 violation equals: expected TT05 for an == comparison replaced by a bare err check"]
    return []



def run_testfile_assert_probes(tmp) -> list:
    """run_testfile_assert_probes runs every TT04-TT08 case against
    tmp: the TT04-TT05 cases here, plus TT06-TT08 from
    test_tampering_probes_testfile_discard."""
    problems = []
    for fn in (
        _probe_tt04_violation,
        _probe_tt04_clean_helper_extraction,
        _probe_tt04_clean_new_test_func_only,
        _probe_tt04_clean_helper_suffix_only,
        _probe_tt04_clean_assert_call_only,
        _probe_tt05_violation,
        _probe_tt05_violation_equals_variant,
        _probe_tt05_clean_new_check,
    ):
        problems.extend(fn(tmp))
    problems.extend(run_testfile_discard_probes(tmp))
    return problems
