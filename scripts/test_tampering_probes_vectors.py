#!/usr/bin/env python3
"""--probe cases for TT09-TT10, the testdata/vectors/ rules."""
from test_tampering_diff import commit_all, diff_after_change, init_probe_repo
from test_tampering_rules import check_vector_deleted, check_vector_modified


def _new_repo(tmp, name):
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


def _has(findings, tid):
    return any(f.id == tid for f in findings)


def _probe_tt09_violation(tmp):
    repo = _new_repo(tmp, "tt09_violation")
    vec_dir = repo / "envelope" / "testdata" / "vectors"
    vec_dir.mkdir(parents=True)
    (vec_dir / "valid_basic.json").write_text('{"a": 1}\n')
    commit_all(repo, "base")
    diffs = diff_after_change(repo, lambda r: (r / "envelope" / "testdata" / "vectors" / "valid_basic.json").unlink())
    if not _has(check_vector_deleted(diffs), "TT09"):
        return ["tt09 violation: expected TT09 for a deleted conformance vector"]
    return []


def _probe_tt09_clean_add(tmp):
    repo = _new_repo(tmp, "tt09_clean")
    vec_dir = repo / "envelope" / "testdata" / "vectors"
    vec_dir.mkdir(parents=True)
    (vec_dir / "valid_basic.json").write_text('{"a": 1}\n')
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / "envelope" / "testdata" / "vectors" / "valid_extra.json").write_text('{"b": 2}\n')
    )
    findings = check_vector_deleted(diffs)
    if _has(findings, "TT09"):
        return [f"tt09 clean add: unexpected TT09: {findings}"]
    return []


def _probe_tt10_violation(tmp):
    repo = _new_repo(tmp, "tt10_violation")
    vec_dir = repo / "machine" / "testdata" / "vectors"
    vec_dir.mkdir(parents=True)
    (vec_dir / "valid_basic.json").write_text('{"a": 1}\n')
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / "machine" / "testdata" / "vectors" / "valid_basic.json").write_text('{"a": 2}\n')
    )
    if not _has(check_vector_modified(diffs), "TT10"):
        return ["tt10 violation: expected TT10 for a vector modified in place"]
    return []


def _probe_tt10_clean_new_file(tmp):
    repo = _new_repo(tmp, "tt10_clean")
    vec_dir = repo / "machine" / "testdata" / "vectors"
    vec_dir.mkdir(parents=True)
    (vec_dir / "valid_basic.json").write_text('{"a": 1}\n')
    commit_all(repo, "base")
    diffs = diff_after_change(
        repo, lambda r: (r / "machine" / "testdata" / "vectors" / "valid_new.json").write_text('{"b": 2}\n')
    )
    findings = check_vector_modified(diffs)
    if _has(findings, "TT10"):
        return [f"tt10 clean new file: unexpected TT10: {findings}"]
    return []


def run_vector_probes(tmp) -> list:
    """run_vector_probes runs every TT09-TT10 case against tmp."""
    problems = []
    for fn in (
        _probe_tt09_violation,
        _probe_tt09_clean_add,
        _probe_tt10_violation,
        _probe_tt10_clean_new_file,
    ):
        problems.extend(fn(tmp))
    return problems
