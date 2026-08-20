#!/usr/bin/env python3
"""--probe cases for diff-source resolution and override-trailer
resolution, including the TT11/TT14 hardness cases."""
import subprocess
import sys
from pathlib import Path

from check_test_tampering import resolve_diff_source
from test_tampering_diff import build_diff, commit_all, init_probe_repo, repo_root, stage_all
from test_tampering_override import resolve_overrides
from test_tampering_rules import Finding

_CHECKER = Path(__file__).resolve().parent / "check_test_tampering.py"


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


# --- diff-source resolution ---------------------------------------------


def _probe_staged_picked(tmp):
    repo = _new_repo(tmp, "diffsrc_staged")
    (repo / "a.txt").write_text("one\n")
    commit_all(repo, "base")
    (repo / "a.txt").write_text("two\n")
    stage_all(repo)
    diff_args, _message, skip = resolve_diff_source(None, None, repo)
    if skip or diff_args != ["--cached"]:
        return [f"diffsrc staged: expected ['--cached'], got {diff_args} skip={skip}"]
    return []


def _probe_fallback_to_parent(tmp):
    repo = _new_repo(tmp, "diffsrc_fallback")
    (repo / "a.txt").write_text("one\n")
    commit_all(repo, "base")
    (repo / "a.txt").write_text("two\n")
    commit_all(repo, "second")
    diff_args, message, skip = resolve_diff_source(None, None, repo)
    if skip or diff_args != ["HEAD~1", "HEAD"] or "second" not in message:
        return [f"diffsrc fallback: expected HEAD~1...HEAD with tip message, got {diff_args} {message!r} skip={skip}"]
    return []


def _probe_no_git_skips(tmp):
    plain = tmp / "plain_dir"
    plain.mkdir()
    if repo_root(plain) is not None:
        return ["diffsrc no-git: expected repo_root to return None outside any repository"]
    return []


def _probe_single_commit_skips(tmp):
    repo = _new_repo(tmp, "diffsrc_single")
    (repo / "a.txt").write_text("one\n")
    commit_all(repo, "only commit")
    diff_args, message, skip = resolve_diff_source(None, None, repo)
    if not skip or diff_args is not None or message is not None:
        return [f"diffsrc single commit: expected a skip signal, got {diff_args} {message!r} skip={skip}"]
    return []


def _probe_staged_reads_message_file(tmp):
    """A staged diff with a --message-file must read that file's text
    as the override message: the exact path the real commit-msg hook
    uses in production."""
    repo = _new_repo(tmp, "diffsrc_staged_message_file")
    (repo / "a.txt").write_text("one\n")
    commit_all(repo, "base")
    (repo / "a.txt").write_text("two\n")
    stage_all(repo)
    msg_file = tmp / "diffsrc_staged_message_file_msg.txt"
    msg_file.write_text("subject\n\nAllow-Test-Change: TT02 removed a flaky retry loop after root cause fix\n")
    diff_args, message, skip = resolve_diff_source(None, str(msg_file), repo)
    if skip or diff_args != ["--cached"] or message != msg_file.read_text():
        return [f"diffsrc staged message-file: expected the file's text, got {message!r} skip={skip}"]
    return []


def _probe_staged_no_message_file_is_none(tmp):
    """A staged diff with no --message-file must carry no message, so
    a bare `make verify-fast` run can never accidentally honor a
    trailer left over in the working tree."""
    repo = _new_repo(tmp, "diffsrc_staged_no_message_file")
    (repo / "a.txt").write_text("one\n")
    commit_all(repo, "base")
    (repo / "a.txt").write_text("two\n")
    stage_all(repo)
    diff_args, message, skip = resolve_diff_source(None, None, repo)
    if skip or diff_args != ["--cached"] or message is not None:
        return [f"diffsrc staged no message-file: expected message None, got {message!r} skip={skip}"]
    return []


def _probe_no_git_skips_end_to_end(tmp):
    """The CLI itself, not just repo_root, must skip cleanly with exit
    0 outside any git repository."""
    plain = tmp / "plain_dir_e2e"
    plain.mkdir()
    result = subprocess.run(
        [sys.executable, str(_CHECKER)], cwd=plain, capture_output=True, text=True
    )
    if result.returncode != 0 or "no .git found" not in result.stdout:
        return [f"no-git end-to-end: expected exit 0 with a skip note, got {result.returncode} {result.stdout!r}"]
    return []


def _probe_single_commit_skips_end_to_end(tmp):
    """The CLI itself, not just resolve_diff_source, must skip cleanly
    with exit 0 on a repository with no parent commit and nothing
    staged."""
    repo = _new_repo(tmp, "diffsrc_single_e2e")
    (repo / "a.txt").write_text("one\n")
    commit_all(repo, "only commit")
    result = subprocess.run(
        [sys.executable, str(_CHECKER)], cwd=repo, capture_output=True, text=True
    )
    if result.returncode != 0 or "no parent commit" not in result.stdout:
        return [f"single-commit end-to-end: expected exit 0 with a skip note, got {result.returncode} {result.stdout!r}"]
    return []


# --- override resolution --------------------------------------------------


def _probe_override_waives_only_its_id(tmp):
    findings = [Finding("TT02", "a_test.go", 1, "x"), Finding("TT04", "a_test.go", 2, "y")]
    message = "subject\n\nAllow-Test-Change: TT02 removed a flaky retry loop after root cause fix\n"
    unresolved, overridden = resolve_overrides(findings, message)
    if [f.id for f in unresolved] != ["TT04"] or [f.id for f, _t in overridden] != ["TT02"]:
        return [f"override scoping: expected TT02 waived and TT04 unresolved, got {unresolved} {overridden}"]
    return []


def _probe_override_boilerplate_reason_unresolved(tmp):
    findings = [Finding("TT02", "a_test.go", 1, "x")]
    message = "subject\n\nAllow-Test-Change: TT02 fix cleanup wip misc temp\n"
    unresolved, _overridden = resolve_overrides(findings, message)
    if not unresolved:
        return ["override boilerplate: expected the finding to stay unresolved"]
    return []


def _probe_override_word_count_boundary(tmp):
    findings_pass = [Finding("TT02", "a_test.go", 1, "x")]
    six_words = "removed obsolete retry after fixing root cause"
    unresolved, overridden = resolve_overrides(
        findings_pass, f"subject\n\nAllow-Test-Change: TT02 {six_words}\n"
    )
    if unresolved or not overridden:
        return [f"override word count: a six-word reason must pass, got unresolved={unresolved}"]

    findings_fail = [Finding("TT02", "a_test.go", 1, "x")]
    five_words = "removed obsolete retry after fixing"
    unresolved, overridden = resolve_overrides(
        findings_fail, f"subject\n\nAllow-Test-Change: TT02 {five_words}\n"
    )
    if not unresolved or overridden:
        return [f"override word count: a five-word reason must fail, got overridden={overridden}"]
    return []


def _probe_gate_change_word_count_boundary(tmp):
    """The gate-change floor is fifteen words, not the test-change
    floor of six: a fourteen-word gate reason must still fail, even
    though it clears the easier bar by a wide margin."""
    findings_pass = [Finding("TT11", "Makefile", 1, "x")]
    fifteen_words = " ".join(["word"] * 15)
    unresolved, overridden = resolve_overrides(
        findings_pass, f"subject\n\nAllow-Gate-Change: TT11 {fifteen_words}\n"
    )
    if unresolved or not overridden:
        return [f"gate word count: a fifteen-word reason must pass, got unresolved={unresolved}"]

    findings_fail = [Finding("TT11", "Makefile", 1, "x")]
    fourteen_words = " ".join(["word"] * 14)
    unresolved, overridden = resolve_overrides(
        findings_fail, f"subject\n\nAllow-Gate-Change: TT11 {fourteen_words}\n"
    )
    if not unresolved or overridden:
        return [f"gate word count: a fourteen-word reason must fail, got overridden={overridden}"]
    return []


def _probe_first_trailer_per_id_wins_even_if_invalid(tmp):
    """resolve_overrides picks the first trailer for a given ID and
    never falls through to a later, valid one: an invalid trailer
    listed first must leave the finding unresolved, even when a
    perfectly valid trailer for the same ID follows it."""
    findings = [Finding("TT02", "a_test.go", 1, "x")]
    message = (
        "subject\n\n"
        "Allow-Test-Change: TT02 too short\n"
        "Allow-Test-Change: TT02 removed obsolete retry after fixing root cause thoroughly\n"
    )
    unresolved, overridden = resolve_overrides(findings, message)
    if not unresolved or overridden:
        return [
            "first trailer wins: expected the invalid first trailer to leave TT02 unresolved "
            f"despite a valid second trailer, got unresolved={unresolved} overridden={overridden}"
        ]
    return []


def _probe_gate_change_alone_leaves_sibling_unresolved(tmp):
    findings = [Finding("TT11", "Makefile", 1, "x"), Finding("TT04", "a_test.go", 2, "y")]
    gate_reason = " ".join(["word"] * 15)
    message = f"subject\n\nAllow-Gate-Change: TT11 {gate_reason}\n"
    unresolved, overridden = resolve_overrides(findings, message)
    if len(unresolved) != 2 or overridden:
        return [f"gate-change alone: expected both findings unresolved, got {unresolved} {overridden}"]

    message_with_sibling = (
        f"subject\n\nAllow-Gate-Change: TT11 {gate_reason}\n"
        "Allow-Test-Change: TT04 removed a helper assertion no longer needed by the caller\n"
    )
    unresolved, overridden = resolve_overrides(findings, message_with_sibling)
    if unresolved or len(overridden) != 2:
        return [f"gate-change with sibling: expected both cleared, got {unresolved} {overridden}"]
    return []


def _probe_test_change_never_waives_gate_ids(tmp):
    findings = [Finding("TT14", "scripts/check_test_tampering.py", 1, "x")]
    six_words = "removed obsolete retry after fixing root"
    message = f"subject\n\nAllow-Test-Change: TT14 {six_words}\n"
    unresolved, overridden = resolve_overrides(findings, message)
    if not unresolved or overridden:
        return [f"test-change on TT14: expected it to stay unresolved, got {unresolved} {overridden}"]

    gate_reason = " ".join(["word"] * 15)
    message_gate = f"subject\n\nAllow-Gate-Change: TT14 {gate_reason}\n"
    unresolved, overridden = resolve_overrides(findings, message_gate)
    if unresolved or not overridden:
        return [f"gate-change on TT14: expected it to clear, got {unresolved} {overridden}"]
    return []


def _probe_range_reads_tip_message(tmp):
    repo = _new_repo(tmp, "range_tip_message")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "sample_test.go").write_text(
        'package sample\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif !cond1 {\n\t\tt.Error("one")\n\t}\n'
        '\tif !cond2 {\n\t\tt.Error("two")\n\t}\n'
        "}\n"
    )
    commit_all(repo, "base")
    (repo / "scripts" / "sample_test.go").write_text(
        'package sample\n\nimport "testing"\n\n'
        "func TestFoo(t *testing.T) {\n"
        '\tif !cond1 {\n\t\tt.Error("one")\n\t}\n'
        "}\n"
    )
    reason = "removed a duplicated assertion the refactor made obsolete"
    commit_all(repo, f"trim duplicate assertion\n\nAllow-Test-Change: TT04 {reason}\n")

    diff_args, message, skip = resolve_diff_source("HEAD~1...HEAD", None, repo)
    if skip or diff_args != ["HEAD~1...HEAD"]:
        return [f"range tip message: unexpected diff args {diff_args} skip={skip}"]
    diffs = build_diff(repo, diff_args)
    from test_tampering_rules import check_assertion_decrease

    findings = check_assertion_decrease(diffs)
    unresolved, overridden = resolve_overrides(findings, message)
    if unresolved or not overridden:
        return [f"range tip message: expected the tip commit's own trailer to clear TT04, got {unresolved}"]
    return []


def _probe_range_and_message_file_exit_2(tmp):
    result = subprocess.run(
        [sys.executable, str(_CHECKER), "--range", "HEAD~1..HEAD", "--message-file", "somefile"],
        cwd=tmp, capture_output=True, text=True,
    )
    if result.returncode != 2:
        return [f"range+message-file: expected exit 2, got {result.returncode}"]
    return []


def run_diffoverride_probes(tmp) -> list:
    """run_diffoverride_probes runs every diff-resolution and
    override-resolution case against tmp."""
    problems = []
    for fn in (
        _probe_staged_picked,
        _probe_fallback_to_parent,
        _probe_no_git_skips,
        _probe_single_commit_skips,
        _probe_staged_reads_message_file,
        _probe_staged_no_message_file_is_none,
        _probe_no_git_skips_end_to_end,
        _probe_single_commit_skips_end_to_end,
        _probe_override_waives_only_its_id,
        _probe_override_boilerplate_reason_unresolved,
        _probe_override_word_count_boundary,
        _probe_gate_change_word_count_boundary,
        _probe_first_trailer_per_id_wins_even_if_invalid,
        _probe_gate_change_alone_leaves_sibling_unresolved,
        _probe_test_change_never_waives_gate_ids,
        _probe_range_reads_tip_message,
        _probe_range_and_message_file_exit_2,
    ):
        problems.extend(fn(tmp))
    return problems
