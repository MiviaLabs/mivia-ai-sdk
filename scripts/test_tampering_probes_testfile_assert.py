#!/usr/bin/env python3
"""--probe cases for TT04-TT08, the assertion/comparison/discard/
tolerance/parallel *_test.go rules. Split from
test_tampering_probes_testfile.py (which keeps TT01-TT03) to stay
within this repo's file-size convention; see docs/plans/test-tampering.md."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_rules import (
    check_assertion_decrease,
    check_comparison_dropped_for_bare_err,
    check_numeric_literal_increase,
    check_parallel_removed,
    check_result_discarded,
)


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


def _probe_tt06_violation_single_value_discard(tmp):
    """The single-value discard form `_ = f()`, not just `_, _ = f()`,
    must also fire TT06."""
    repo = _new_repo(tmp, "tt06_violation_single")
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
            "\t_ = f()\n"
            "}\n"
        ),
    )
    if not _has(check_result_discarded(diffs), "TT06"):
        return ["tt06 violation single discard: expected TT06 for a single-value `_ = f()` discard"]
    return []


def _probe_tt06_clean_different_function_discarded(tmp):
    """The strong near-miss: a DIFFERENT, unrelated call is discarded
    in the same hunk, while the checked call f() is left untouched.
    The function-name-matching guard (m.group(1) in removed_calls)
    must not fire on this, unlike the weaker unrelated-rename probe
    above which never puts any discard pattern in the diff at all."""
    repo = _new_repo(tmp, "tt06_clean_different_func")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        "\tv, err := f()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = v\n"
        "\tg()\n"
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\tv, err := f()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = v\n"
            "\t_ = g()\n"
            "}\n"
        ),
    )
    findings = check_result_discarded(diffs)
    if _has(findings, "TT06"):
        return [f"tt06 clean different function discarded: unexpected TT06: {findings}"]
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


def _probe_tt07_violation_other_keywords(tmp):
    """Five of the six _TOLERANCE_KEYWORDS entries beyond time.Sleep(
    must each independently fire TT07."""
    problems = []
    cases = (
        ("time_after", "time.After(10 * time.Millisecond)", "time.After(100 * time.Millisecond)"),
        ("timeout", "requestTimeout := 5", "requestTimeout := 50"),
        ("retries", "maxRetries := 3", "maxRetries := 30"),
        ("tolerance", "epsilonTolerance := 1", "epsilonTolerance := 10"),
        ("delta", "allowedDelta := 2", "allowedDelta := 20"),
    )
    for suffix, before, after in cases:
        repo = _new_repo(tmp, f"tt07_violation_{suffix}")
        (repo / "a_test.go").write_text(
            'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
            f"func TestFoo(t *testing.T) {{\n\t{before}\n}}\n"
        )
        commit_all(repo, "base")
        diffs = diff_after_change(
            repo,
            lambda r, after=after: (r / "a_test.go").write_text(
                'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
                f"func TestFoo(t *testing.T) {{\n\t{after}\n}}\n"
            ),
        )
        if not _has(check_numeric_literal_increase(diffs), "TT07"):
            problems.append(f"tt07 violation {suffix}: expected TT07 for {before!r} -> {after!r}")
    return problems


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


def run_testfile_assert_probes(tmp) -> list:
    """run_testfile_assert_probes runs every TT04-TT08 case against tmp."""
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
        _probe_tt06_violation,
        _probe_tt06_violation_single_value_discard,
        _probe_tt06_clean_rename,
        _probe_tt06_clean_different_function_discarded,
        _probe_tt07_violation,
        _probe_tt07_violation_other_keywords,
        _probe_tt07_clean_decrease,
        _probe_tt08_violation,
        _probe_tt08_clean_unrelated_edit,
    ):
        problems.extend(fn(tmp))
    return problems
