#!/usr/bin/env python3
"""--probe cases for TT11-TT14, the gate-infrastructure rules."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_rules_infra import (
    check_denylist_entry_added,
    check_self_preservation,
    check_self_reference_guard,
    check_weakened_floor,
)

_MAKEFILE_BASE = (
    "COVERAGE_FLOOR := 85\n\n"
    "verify-fast:\n"
    "\tgo vet ./...\n"
    "\tpython3 scripts/check_docs.py\n"
    "\tpython3 scripts/check_structure.py\n\n"
    "verify: verify-fast\n"
    "\tgo test -race ./...\n"
)

_HOOK_LIVE = '#!/bin/sh\nset -e\npython3 scripts/check_test_tampering.py --message-file "$1"\n'


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


def _has(findings, tid):
    return any(f.id == tid for f in findings)


# --- TT11: self-reference guard ----------------------------------------


def _probe_tt11_violation(tmp):
    repo = _new_repo(tmp, "tt11_violation")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "check_foo.py").write_text("x = 1\n")
    (repo / "other.go").write_text("package other\n")
    commit_all(repo, "base")

    def mutate(r):
        (r / "scripts" / "check_foo.py").write_text("x = 2\n")
        (r / "other.go").write_text("package other\n\nvar y = 1\n")

    diffs = diff_after_change(repo, mutate)
    if not _has(check_self_reference_guard(diffs), "TT11"):
        return ["tt11 violation: expected TT11 for a gate-infra change paired with another file"]
    return []


def _probe_tt11_clean_infra_only(tmp):
    repo = _new_repo(tmp, "tt11_clean")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "check_foo.py").write_text("x = 1\n")
    (repo / "other.go").write_text("package other\n")
    commit_all(repo, "base")
    diffs = diff_after_change(repo, lambda r: (r / "scripts" / "check_foo.py").write_text("x = 2\n"))
    findings = check_self_reference_guard(diffs)
    if _has(findings, "TT11"):
        return [f"tt11 clean infra-only: unexpected TT11: {findings}"]
    return []


# --- TT12: a new mutation denylist entry --------------------------------


def _probe_tt12_violation(tmp):
    repo = _new_repo(tmp, "tt12_violation")
    (repo / "scripts" / "mutation_denylist").mkdir(parents=True)
    (repo / "scripts" / "mutation_denylist" / "pkg.json").write_text(
        '{"denylist": [{"file": "x.go", "snippet": "a"}], "floor": 90}\n'
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "scripts" / "mutation_denylist" / "pkg.json").write_text(
            '{"denylist": [{"file": "x.go", "snippet": "a"}, {"file": "y.go", "snippet": "b"}], "floor": 90}\n'
        ),
    )
    if not _has(check_denylist_entry_added(diffs), "TT12"):
        return ["tt12 violation: expected TT12 for a new denylist entry"]
    return []


def _probe_tt12_clean_floor_raise(tmp):
    repo = _new_repo(tmp, "tt12_clean")
    (repo / "scripts" / "mutation_denylist").mkdir(parents=True)
    (repo / "scripts" / "mutation_denylist" / "pkg.json").write_text(
        '{"denylist": [{"file": "x.go", "snippet": "a"}], "floor": 90}\n'
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "scripts" / "mutation_denylist" / "pkg.json").write_text(
            '{"denylist": [{"file": "x.go", "snippet": "a"}], "floor": 95}\n'
        ),
    )
    findings = check_denylist_entry_added(diffs)
    if _has(findings, "TT12"):
        return [f"tt12 clean floor raise: unexpected TT12: {findings}"]
    return []


# --- TT13: a weakened floor ----------------------------------------------


def _probe_tt13_violation(tmp):
    repo = _new_repo(tmp, "tt13_violation")
    (repo / "Makefile").write_text(_MAKEFILE_BASE)
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "Makefile").write_text(
            _MAKEFILE_BASE.replace("\tpython3 scripts/check_docs.py\n", "")
        ),
    )
    if not _has(check_weakened_floor(diffs), "TT13"):
        return ["tt13 violation: expected TT13 for a removed gate invocation line"]
    return []


def _probe_tt13_clean_unrelated_edit(tmp):
    repo = _new_repo(tmp, "tt13_clean")
    (repo / "Makefile").write_text(_MAKEFILE_BASE)
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / "Makefile").write_text(_MAKEFILE_BASE.replace("go vet", "go vet -json"))
    )
    findings = check_weakened_floor(diffs)
    if _has(findings, "TT13"):
        return [f"tt13 clean unrelated edit: unexpected TT13: {findings}"]
    return []


def _probe_tt13_violation_coverage_floor(tmp):
    repo = _new_repo(tmp, "tt13_floor")
    (repo / "Makefile").write_text(_MAKEFILE_BASE)
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / "Makefile").write_text(_MAKEFILE_BASE.replace("COVERAGE_FLOOR := 85", "COVERAGE_FLOOR := 60"))
    )
    if not _has(check_weakened_floor(diffs), "TT13"):
        return ["tt13 coverage floor: expected TT13 for a lowered COVERAGE_FLOOR"]
    return []


# --- TT14: self-preservation of the hook and the checker ----------------


def _probe_tt14_violation_hook_deleted(tmp):
    repo = _new_repo(tmp, "tt14_hook_deleted")
    (repo / ".githooks").mkdir()
    (repo / ".githooks" / "commit-msg").write_text(_HOOK_LIVE)
    commit_all(repo, "base")
    diffs = diff_after_change(repo, lambda r: (r / ".githooks" / "commit-msg").unlink())
    if not _has(check_self_preservation(diffs), "TT14"):
        return ["tt14 violation: expected TT14 for a deleted commit-msg hook"]
    return []


def _probe_tt14_clean_hook_comment_added(tmp):
    repo = _new_repo(tmp, "tt14_hook_clean")
    (repo / ".githooks").mkdir()
    (repo / ".githooks" / "commit-msg").write_text(_HOOK_LIVE)
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / ".githooks" / "commit-msg").write_text("# note\n" + _HOOK_LIVE)
    )
    findings = check_self_preservation(diffs)
    if _has(findings, "TT14"):
        return [f"tt14 clean hook comment: unexpected TT14: {findings}"]
    return []


def _probe_tt14_hook_invocation_broken(tmp):
    repo = _new_repo(tmp, "tt14_hook_broken")
    (repo / ".githooks").mkdir()
    (repo / ".githooks" / "commit-msg").write_text(_HOOK_LIVE)
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / ".githooks" / "commit-msg").write_text(
            '#!/bin/sh\nset -e\n# python3 scripts/check_test_tampering.py --message-file "$1"\n'
        ),
    )
    if not _has(check_self_preservation(diffs), "TT14"):
        return ["tt14 hook broken: expected TT14 for a commented-out hook invocation"]
    return []


def _probe_tt14_checker_deleted(tmp):
    repo = _new_repo(tmp, "tt14_checker_deleted")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "test_tampering_rules.py").write_text("# TT01 lives here\n")
    commit_all(repo, "base")
    diffs = diff_after_change(repo, lambda r: (r / "scripts" / "test_tampering_rules.py").unlink())
    if not _has(check_self_preservation(diffs), "TT14"):
        return ["tt14 checker deleted: expected TT14 for a deleted checker source file"]
    return []


def _probe_tt14_id_vanished(tmp):
    repo = _new_repo(tmp, "tt14_id_vanished")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "check_test_tampering.py").write_text('ID = "TT01"\nOTHER = "TT02"\n')
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / "scripts" / "check_test_tampering.py").write_text('OTHER = "TT02"\n')
    )
    if not _has(check_self_preservation(diffs), "TT14"):
        return ["tt14 id vanished: expected TT14 for a vanished finding-ID literal"]
    return []


def _probe_tt14_unreachable_code_added(tmp):
    repo = _new_repo(tmp, "tt14_unreachable_added")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "test_tampering_rules_infra.py").write_text(
        "def check_something(diffs):\n    findings = []\n    return findings\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "scripts" / "test_tampering_rules_infra.py").write_text(
            "def check_something(diffs):\n"
            "    return []\n"
            "    for d in diffs:\n"
            "        findings.append(d)\n"
        ),
    )
    if not _has(check_self_preservation(diffs), "TT14"):
        return ["tt14 unreachable code: expected TT14 for dead code after an early return"]
    return []


def _probe_tt14_unreachable_code_removed_clean(tmp):
    repo = _new_repo(tmp, "tt14_unreachable_removed")
    (repo / "scripts").mkdir()
    (repo / "scripts" / "test_tampering_rules_infra.py").write_text(
        "def check_something(diffs):\n"
        "    return []\n"
        "    for d in diffs:\n"
        "        pass\n"
    )
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo,
        lambda r: (r / "scripts" / "test_tampering_rules_infra.py").write_text(
            "def check_something(diffs):\n    return []\n"
        ),
    )
    findings = check_self_preservation(diffs)
    if _has(findings, "TT14"):
        return [f"tt14 unreachable code removed: unexpected TT14 on a legitimate rewrite: {findings}"]
    return []


def run_infra_probes(tmp) -> list:
    """run_infra_probes runs every TT11-TT14 case against tmp."""
    problems = []
    for fn in (
        _probe_tt11_violation,
        _probe_tt11_clean_infra_only,
        _probe_tt12_violation,
        _probe_tt12_clean_floor_raise,
        _probe_tt13_violation,
        _probe_tt13_clean_unrelated_edit,
        _probe_tt13_violation_coverage_floor,
        _probe_tt14_violation_hook_deleted,
        _probe_tt14_clean_hook_comment_added,
        _probe_tt14_hook_invocation_broken,
        _probe_tt14_checker_deleted,
        _probe_tt14_id_vanished,
        _probe_tt14_unreachable_code_added,
        _probe_tt14_unreachable_code_removed_clean,
    ):
        problems.extend(fn(tmp))
    return problems
