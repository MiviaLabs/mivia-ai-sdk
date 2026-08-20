#!/usr/bin/env python3
"""--probe cases for TT06-TT08, the discard/tolerance/parallel
*_test.go rules. Split from test_tampering_probes_testfile_assert.py
(which keeps TT04-TT05) to stay within this repo's file-size
convention; see docs/plans/test-tampering.md."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_rules import check_numeric_literal_increase, check_parallel_removed, check_result_discarded


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


def _has(findings, tid):
    return any(f.id == tid for f in findings)


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


def _probe_tt06_violation_qualified_name_mismatch(tmp):
    """A call captured with a package-qualified name (pkg.Foo() ) and
    discarded with the bare, unqualified name (Foo()), or vice versa,
    must still match as the same call."""
    repo = _new_repo(tmp, "tt06_violation_qualified")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        "\tv, err := pkg.Foo()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = v\n"
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\t_, _ = Foo()\n"
            "}\n"
        ),
    )
    if not _has(check_result_discarded(diffs), "TT06"):
        return ["tt06 violation qualified name: expected TT06 across a qualified/unqualified name mismatch"]
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


def _probe_tt07_violation_shifted_by_unrelated_insertion(tmp):
    """An unrelated line inserted earlier in the same hunk must not
    hide a real tolerance increase: keyword lines pair by matching
    occurrence order, not by raw removed[i]/added[i] index."""
    repo = _new_repo(tmp, "tt07_violation_shifted")
    (repo / "a_test.go").write_text(
        'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
        "func TestFoo(t *testing.T) {\n\ttime.Sleep(10 * time.Millisecond)\n}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport (\n\t"testing"\n\t"time"\n)\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\tx := 1\n"
            "\t_ = x\n"
            "\ttime.Sleep(100 * time.Millisecond)\n"
            "}\n"
        ),
    )
    if not _has(check_numeric_literal_increase(diffs), "TT07"):
        return ["tt07 violation shifted: expected TT07 despite an unrelated insertion earlier in the hunk"]
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


def _probe_tt08_violation_raw_string_closing_brace(tmp):
    """A Go raw string containing a line that is exactly `}` must not
    truncate extract_functions's view of the function body: a real
    t.Parallel() removal after such a line must still fire TT08."""
    repo = _new_repo(tmp, "tt08_violation_raw_string")
    (repo / "a_test.go").write_text(
        'package a\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        "\ts := `\n}\n`\n"
        "\tt.Parallel()\n"
        "\t_ = s\n"
        "}\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "a_test.go").write_text(
            'package a\n\nimport "testing"\n\n'
            "func TestFoo(t *testing.T) {\n"
            "\ts := `\n}\n`\n"
            "\t_ = s\n"
            "}\n"
        ),
    )
    if not _has(check_parallel_removed(diffs), "TT08"):
        return ["tt08 violation raw string: expected TT08 despite a `}` line inside a raw string"]
    return []


def run_testfile_discard_probes(tmp) -> list:
    """run_testfile_discard_probes runs every TT06-TT08 case against tmp."""
    problems = []
    for fn in (
        _probe_tt06_violation,
        _probe_tt06_violation_single_value_discard,
        _probe_tt06_violation_qualified_name_mismatch,
        _probe_tt06_clean_rename,
        _probe_tt06_clean_different_function_discarded,
        _probe_tt07_violation,
        _probe_tt07_violation_other_keywords,
        _probe_tt07_violation_shifted_by_unrelated_insertion,
        _probe_tt07_clean_decrease,
        _probe_tt08_violation,
        _probe_tt08_violation_raw_string_closing_brace,
        _probe_tt08_clean_unrelated_edit,
    ):
        problems.extend(fn(tmp))
    return problems
