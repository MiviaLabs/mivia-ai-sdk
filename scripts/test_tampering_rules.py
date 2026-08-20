#!/usr/bin/env python3
"""TT01-TT10 detections for scripts/check_test_tampering.py: the
test-file rules (TT01-TT08, *_test.go only) and the conformance-vector
rules (TT09-TT10, testdata/vectors/ under envelope/, machine/, a2a/,
mcp/). See docs/plans/test-tampering.md, "Detections and finding IDs".
Gate-infra rules TT11-TT14 live in test_tampering_rules_infra.py."""
import hashlib
import re
from dataclasses import dataclass


@dataclass
class Finding:
    """Finding is one unresolved-or-not TT rule hit: an ID, a path, a
    line, and a human-readable message."""

    id: str
    path: str
    line: int
    message: str


COLLECTION_REGEX = re.compile(r"^(Test|Benchmark|Fuzz|Example)([A-Z0-9_]|$)")
_FUNC_DECL = re.compile(r"^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(")


def extract_functions(text: str) -> dict:
    """extract_functions maps a Go source's top-level function names to
    their (1-based start line, full body text) pairs, following
    check_structure.py's own func-to-closing-brace-at-column-0 rule. A
    Go raw string (backtick-delimited) can legitimately contain a
    line that is exactly `}`; a line inside one never counts as the
    real closing brace, or as a new function declaration."""
    if not text:
        return {}
    lines = text.splitlines()
    funcs: dict = {}
    start = None
    name = None
    in_raw_string = False
    for i, line in enumerate(lines):
        was_in_raw_string = in_raw_string
        if line.count("`") % 2 == 1:
            in_raw_string = not in_raw_string
        if was_in_raw_string:
            continue
        m = _FUNC_DECL.match(line)
        if m:
            start, name = i, m.group(1)
        elif line == "}" and start is not None:
            body = "\n".join(lines[start : i + 1])
            funcs.setdefault(name, []).append((start + 1, body))
            start = None
    return funcs


def _normalize_body(body: str) -> str:
    """_normalize_body collapses whitespace so a reformatted move still
    hashes the same as its original."""
    return re.sub(r"\s+", " ", body).strip()


def _body_hash(body: str) -> str:
    return hashlib.sha256(_normalize_body(body).encode()).hexdigest()


def _is_test_file(path: str) -> bool:
    return bool(path) and path.endswith("_test.go")


def check_moved_or_dropped(diffs: list) -> list:
    """TT01: a removed Test/Benchmark/Fuzz/Example function whose body
    hash reappears nowhere in the diff under a conforming name."""
    findings = []
    test_diffs = [d for d in diffs if _is_test_file(d.path)]

    added_index: dict = {}
    for d in test_diffs:
        for name, entries in extract_functions(d.new_text).items():
            for _line, body in entries:
                added_index.setdefault(_body_hash(body), []).append(name)

    for d in test_diffs:
        old_funcs = extract_functions(d.old_text)
        new_funcs = extract_functions(d.new_text)
        for name, entries in old_funcs.items():
            if not COLLECTION_REGEX.match(name) or name in new_funcs:
                continue
            for line, body in entries:
                candidates = added_index.get(_body_hash(body), [])
                if any(COLLECTION_REGEX.match(c) for c in candidates):
                    continue
                findings.append(
                    Finding("TT01", d.path, line, f"test function {name} removed with no matching move")
                )
    return findings


_SKIP_PATTERNS = ("t.Skip(", "t.SkipNow(", "t.Skipf(", "testing.Short()")


def check_skip_added(diffs: list) -> list:
    """TT02: a new skip call or short-mode guard, with no matching
    removal in the same hunk."""
    findings = []
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        for hunk in d.hunks:
            removed_join = "\n".join(hunk.removed)
            for line in hunk.added:
                for pat in _SKIP_PATTERNS:
                    if pat in line and pat not in removed_join:
                        findings.append(Finding("TT02", d.path, hunk.new_start, f"skip added: {pat}"))
                        break
    return findings


def _find_build_tag(text: str) -> str:
    """_find_build_tag returns the first //go:build line before the
    package clause, or None."""
    if not text:
        return None
    for line in text.splitlines():
        if line.startswith("package "):
            break
        if line.strip().startswith("//go:build"):
            return line.strip()
    return None


def check_build_tag_changed(diffs: list) -> list:
    """TT03: a //go:build line added to a test file that had none, or
    an existing one's expression changed."""
    findings = []
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        old_tag = _find_build_tag(d.old_text)
        new_tag = _find_build_tag(d.new_text)
        if new_tag and new_tag != old_tag:
            findings.append(Finding("TT03", d.path, 1, "go:build constraint added or changed"))
    return findings


_ASSERT_PATTERNS = ("t.Error(", "t.Errorf(", "t.Fatal(", "t.Fatalf(")
_HELPER_SUFFIXES = ("Cases(", "Suite(", "RunAll(")
_ASSERT_CALL = re.compile(r"(^|[^A-Za-z0-9_])assert[A-Za-z0-9_]*\(")


def check_assertion_decrease(diffs: list) -> list:
    """TT04: net decrease in assertion sites across the whole diff,
    suppressed by a new test function or a helper-extraction signal.
    Tuned to miss rather than false-positive."""
    removed = added = 0
    new_test_func = False
    helper_signal = False
    first_path = None
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        first_path = first_path or d.path
        for hunk in d.hunks:
            for line in hunk.removed:
                removed += sum(line.count(p) for p in _ASSERT_PATTERNS)
            for line in hunk.added:
                added += sum(line.count(p) for p in _ASSERT_PATTERNS)
                if any(suf in line for suf in _HELPER_SUFFIXES) or _ASSERT_CALL.search(line):
                    helper_signal = True
        old_funcs = extract_functions(d.old_text)
        new_funcs = extract_functions(d.new_text)
        for name in new_funcs:
            if COLLECTION_REGEX.match(name) and name not in old_funcs:
                new_test_func = True
    if removed > added and not new_test_func and not helper_signal:
        return [
            Finding(
                "TT04", first_path or "", 0, f"assertion sites decreased net ({removed} removed, {added} added)"
            )
        ]
    return []


_COMPARISON_LINE = re.compile(r"if\s+([^\s()]+)\s*(==|!=)\s*([^\s()]+)")
_BARE_ERR_LINE = re.compile(r"^\s*if\s+err\s*(==|!=)\s*nil\s*\{?\s*$")


def _is_nonerror_comparison(line: str) -> bool:
    m = _COMPARISON_LINE.search(line)
    if not m:
        return False
    lhs, _op, rhs = m.groups()
    return lhs != "err" and rhs != "err"


def check_comparison_dropped_for_bare_err(diffs: list) -> list:
    """TT05: a removed non-error comparison replaced in the same hunk
    by nothing but a bare `if err != nil` / `if err == nil`."""
    findings = []
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        for hunk in d.hunks:
            removed_hit = any(_is_nonerror_comparison(l) for l in hunk.removed)
            added_hit = any(_BARE_ERR_LINE.match(l) for l in hunk.added)
            if removed_hit and added_hit:
                findings.append(Finding("TT05", d.path, hunk.new_start, "comparison replaced by a bare err check"))
    return findings


_CAPTURE_LINE = re.compile(r"^\s*(\w+)\s*,\s*err\s*:?=\s*(\w[\w.]*)\(")
_DISCARD_LINE = re.compile(r"^\s*_\s*,?\s*_?\s*=\s*(\w[\w.]*)\(")
_ERR_CHECK_LINE = re.compile(r"\berr\b.*(!=|==)\s*nil")


def _call_names(call: str) -> set:
    """_call_names returns a call's raw text and its unqualified
    suffix (after the last `.`), so `pkg.Foo(` captured and `Foo(`
    discarded (or vice versa) still match as the same call."""
    return {call, call.rsplit(".", 1)[-1]}


def check_result_discarded(diffs: list) -> list:
    """TT06: a call whose result a removed line captured and checked
    now discarded with `_ = f()` / `_, _ = f()` in the same hunk."""
    findings = []
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        for hunk in d.hunks:
            removed_calls = {m.group(2) for l in hunk.removed if (m := _CAPTURE_LINE.match(l))}
            removed_names = set()
            for call in removed_calls:
                removed_names |= _call_names(call)
            err_checked = any(_ERR_CHECK_LINE.search(l) for l in hunk.removed)
            if not (removed_calls and err_checked):
                continue
            for line in hunk.added:
                m = _DISCARD_LINE.match(line)
                if m and _call_names(m.group(1)) & removed_names:
                    findings.append(
                        Finding("TT06", d.path, hunk.new_start, f"result of {m.group(1)}(...) now discarded")
                    )
    return findings


_TOLERANCE_KEYWORDS = ("time.Sleep(", "time.After(", "Timeout", "Retries", "Tolerance", "Delta")
_NUMBER = re.compile(r"(\d+(?:\.\d+)?)")


def _first_number(line: str):
    m = _NUMBER.search(line)
    return float(m.group(1)) if m else None


def check_numeric_literal_increase(diffs: list) -> list:
    """TT07: a numeric literal near a sleep/timeout/tolerance keyword
    that grew strictly larger. Pairs a removed and an added line by
    matching keyword occurrence order within the hunk, not by raw
    line index: an unrelated insertion or deletion earlier in the
    same hunk shifts index-based pairing and can hide a real
    increase, or compare two unrelated lines."""
    findings = []
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        for hunk in d.hunks:
            for k in _TOLERANCE_KEYWORDS:
                r_lines = [l for l in hunk.removed if k in l]
                a_lines = [l for l in hunk.added if k in l]
                for rline, aline in zip(r_lines, a_lines):
                    rn, an = _first_number(rline), _first_number(aline)
                    if rn is not None and an is not None and an > rn:
                        findings.append(
                            Finding("TT07", d.path, hunk.new_start, f"numeric literal increased ({rn} -> {an})")
                        )
    return findings


def check_parallel_removed(diffs: list) -> list:
    """TT08: a removed t.Parallel() call with no matching addition in
    the same function."""
    findings = []
    for d in diffs:
        if not _is_test_file(d.path):
            continue
        old_funcs = extract_functions(d.old_text)
        new_funcs = extract_functions(d.new_text)
        for name in old_funcs:
            if name not in new_funcs:
                continue
            _old_line, old_body = old_funcs[name][0]
            new_line, new_body = new_funcs[name][0]
            if "t.Parallel()" in old_body and "t.Parallel()" not in new_body:
                findings.append(Finding("TT08", d.path, new_line, f"t.Parallel() removed from {name}"))
    return findings


_VECTOR_SCOPES = ("envelope/", "machine/", "a2a/", "mcp/")


def _is_vector_path(path: str) -> bool:
    if not path or "testdata/vectors/" not in path:
        return False
    return any(path.startswith(scope) for scope in _VECTOR_SCOPES)


def check_vector_deleted(diffs: list) -> list:
    """TT09: a file under a scoped testdata/vectors/ directory deleted."""
    return [
        Finding("TT09", d.old_path, 1, "conformance vector deleted")
        for d in diffs
        if d.status == "D" and _is_vector_path(d.old_path)
    ]


def check_vector_modified(diffs: list) -> list:
    """TT10: a file under a scoped testdata/vectors/ directory modified
    in place. A new file added alongside an unmodified old one does not
    fire, since its status is A, not M."""
    return [
        Finding("TT10", d.new_path, 1, "conformance vector modified in place")
        for d in diffs
        if d.status == "M" and _is_vector_path(d.new_path)
    ]


ALL_RULES = (
    check_moved_or_dropped,
    check_skip_added,
    check_build_tag_changed,
    check_assertion_decrease,
    check_comparison_dropped_for_bare_err,
    check_result_discarded,
    check_numeric_literal_increase,
    check_parallel_removed,
    check_vector_deleted,
    check_vector_modified,
)
