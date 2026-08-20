#!/usr/bin/env python3
"""TT11-TT14 detections for scripts/check_test_tampering.py: the
gate-infrastructure rules, tree-wide. See
docs/plans/test-tampering.md, "Detections and finding IDs"."""
import json
import re

from test_tampering_rules import Finding

_GATE_INFRA_PREFIXES = ("scripts/", ".githooks/", "policy/", "semgrep/", ".github/workflows/")


def _is_gate_infra(path: str) -> bool:
    if path == "Makefile":
        return True
    return bool(path) and any(path.startswith(p) for p in _GATE_INFRA_PREFIXES)


def check_self_reference_guard(diffs: list) -> list:
    """TT11: any gate-infra change present in the same diff as a change
    to any other file at all. Fires even when nothing else does."""
    paths = [d.path for d in diffs]
    infra = [p for p in paths if _is_gate_infra(p)]
    other = [p for p in paths if not _is_gate_infra(p)]
    if infra and other:
        return [Finding("TT11", infra[0], 1, "gate-infra change paired with a non-infra file change")]
    return []


_DENYLIST_DIR_PREFIX = "scripts/mutation_denylist/"


def _denylist_entries(text: str) -> list:
    if not text:
        return []
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return []
    return data.get("denylist", [])


def check_denylist_entry_added(diffs: list) -> list:
    """TT12: a new denylist entry, or a new mutation_denylist file."""
    findings = []
    for d in diffs:
        path = d.path
        if not (path or "").startswith(_DENYLIST_DIR_PREFIX) or not path.endswith(".json"):
            continue
        if d.status == "A":
            findings.append(Finding("TT12", path, 1, "new mutation denylist file added"))
            continue
        if d.status != "M":
            continue
        old_entries = _denylist_entries(d.old_text)
        new_entries = _denylist_entries(d.new_text)
        if len(new_entries) > len(old_entries) or any(e not in old_entries for e in new_entries):
            findings.append(Finding("TT12", path, 1, "new denylist entry added"))
    return findings


_COVERAGE_FLOOR_RE = re.compile(r"^COVERAGE_FLOOR\s*:=\s*(\d+)")
_CHECK_INVOCATION_RE = re.compile(r"python3 scripts/check_\S+\.py(?: --probe)?")


def _makefile_floor(text: str):
    if not text:
        return None
    m = _COVERAGE_FLOOR_RE.search(text)
    return int(m.group(1)) if m else None


def _recipe_invocations(text: str, target: str) -> set:
    """_recipe_invocations returns the gate invocation lines found in
    one Makefile target's recipe body (its indented lines). A
    commented-out recipe line (`#` as its first non-blank character)
    never runs, so it does not count as a live invocation: otherwise
    prefixing the real line with `#` would disable the gate while
    leaving the invocation set unchanged."""
    if not text:
        return set()
    lines = text.splitlines()
    out = set()
    in_target = False
    for line in lines:
        if re.match(rf"^{re.escape(target)}:", line):
            in_target = True
            continue
        if in_target:
            if line and not line[0].isspace():
                break
            if line.strip().startswith("#"):
                continue
            m = _CHECK_INVOCATION_RE.search(line)
            if m:
                out.add(m.group(0))
    return out


def _json_floor(text: str):
    if not text:
        return None
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return None
    return data.get("floor")


def check_weakened_floor(diffs: list) -> list:
    """TT13: COVERAGE_FLOOR lowered, a mutation_denylist floor lowered,
    or a gate invocation line removed from verify-fast/verify. The
    removed-invocation case fires alone, no paired file needed."""
    findings = []
    for d in diffs:
        if d.path == "Makefile":
            old_floor = _makefile_floor(d.old_text)
            new_floor = _makefile_floor(d.new_text)
            if old_floor is not None and new_floor is not None and new_floor < old_floor:
                findings.append(Finding("TT13", d.path, 1, f"COVERAGE_FLOOR lowered ({old_floor} -> {new_floor})"))
            for target in ("verify-fast", "verify"):
                removed = _recipe_invocations(d.old_text, target) - _recipe_invocations(d.new_text, target)
                if removed:
                    findings.append(
                        Finding("TT13", d.path, 1, f"gate invocation removed from {target}: {sorted(removed)}")
                    )
        elif (d.path or "").startswith(_DENYLIST_DIR_PREFIX) and d.path.endswith(".json"):
            old_floor = _json_floor(d.old_text)
            new_floor = _json_floor(d.new_text)
            if old_floor is not None and new_floor is not None and new_floor < old_floor:
                findings.append(Finding("TT13", d.path, 1, f"mutation floor lowered ({old_floor} -> {new_floor})"))
    return findings


_COMMIT_MSG_HOOK_PATH = ".githooks/commit-msg"
_CHECKER_MAIN = "scripts/check_test_tampering.py"
_CHECKER_MODULE_PREFIX = "scripts/test_tampering_"
_LIVE_INVOCATION_RE = re.compile(r"check_test_tampering\.py\s+--message-file")
_ID_LITERAL = re.compile(r"\bTT(0[1-9]|1[0-4])\b")


def _is_checker_source(path: str) -> bool:
    return path == _CHECKER_MAIN or (bool(path) and path.startswith(_CHECKER_MODULE_PREFIX) and path.endswith(".py"))


def _has_live_invocation(text: str) -> bool:
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("#"):
            continue
        if _LIVE_INVOCATION_RE.search(line):
            return True
    return False


def _check_hook_deleted_or_broken(diffs: list) -> list:
    for d in diffs:
        if d.old_path == _COMMIT_MSG_HOOK_PATH and d.status == "D":
            return [Finding("TT14", d.old_path, 1, "commit-msg hook deleted")]
        if d.path == _COMMIT_MSG_HOOK_PATH and d.status == "M":
            old_ok = bool(d.old_text and _has_live_invocation(d.old_text))
            new_ok = bool(d.new_text and _has_live_invocation(d.new_text))
            if old_ok and not new_ok:
                return [Finding("TT14", d.path, 1, "commit-msg hook invocation removed or broken")]
    return []


def _check_checker_deleted(diffs: list) -> list:
    return [
        Finding("TT14", d.old_path, 1, "test-tampering checker source deleted")
        for d in diffs
        if d.status == "D" and _is_checker_source(d.old_path)
    ]


def _check_id_vanished(diffs: list) -> list:
    findings = []
    for d in diffs:
        if not _is_checker_source(d.path) or d.status != "M":
            continue
        old_ids = set(_ID_LITERAL.findall(d.old_text or ""))
        new_ids = set(_ID_LITERAL.findall(d.new_text or ""))
        vanished = old_ids - new_ids
        if vanished:
            findings.append(Finding("TT14", d.path, 1, f"finding ID literal(s) vanished from source: {sorted(vanished)}"))
    return findings


def _bracket_delta(line: str) -> int:
    """_bracket_delta counts net open brackets on a line, ignoring
    string and comment contents well enough for this checker's own
    simple source: good enough to find where a `return` statement's
    own continuation ends, not a general Python parser."""
    in_string = None
    delta = 0
    i = 0
    while i < len(line):
        ch = line[i]
        if in_string:
            if ch == "\\":
                i += 2
                continue
            if ch == in_string:
                in_string = None
        elif ch in ("'", '"'):
            in_string = ch
        elif ch == "#":
            break
        elif ch in "([{":
            delta += 1
        elif ch in ")]}":
            delta -= 1
        i += 1
    return delta


_TRIPLE_QUOTE = re.compile(r'"""|\'\'\'')


def _in_open_triple_quote(lines: list, upto: int) -> bool:
    """_in_open_triple_quote reports whether line `upto` ends inside an
    unclosed triple-quoted string, counting `\"\"\"`/`'''` markers from
    the start of `lines` through `upto`. `_bracket_delta`'s in-string
    tracking resets every line and never sees a triple-quote body as
    a continuation of the statement that opened it; without this, a
    `return \"\"\"` followed by a multi-line string's own content lines
    reads as dead code after the return, not as the return's value."""
    count = sum(len(_TRIPLE_QUOTE.findall(lines[i])) for i in range(upto + 1))
    return count % 2 == 1


def _statement_end(lines: list, start: int) -> int:
    """_statement_end returns the index of the last line belonging to
    the statement beginning at `start`, following open brackets, a
    trailing backslash continuation, and an open triple-quoted string
    across lines."""
    depth = _bracket_delta(lines[start])
    end = start
    while (
        depth > 0 or lines[end].rstrip().endswith("\\") or _in_open_triple_quote(lines, end)
    ) and end + 1 < len(lines):
        end += 1
        depth += _bracket_delta(lines[end])
    return end


def _unreachable_lines(new_text: str) -> list:
    """_unreachable_lines flags dead code after a `return`, reading the
    new blob alone: no notion of old vs. changed. A `return` statement
    followed by non-blank, non-comment lines at the same or a deeper
    indent, with no immediately-following if/elif/else branch opener,
    marks everything until the next dedent as unreachable. A `return`
    that spans multiple lines (an open bracket or a trailing
    backslash) is skipped past its own continuation first, so the
    statement's own wrapped lines are never mistaken for the code that
    follows it."""
    lines = new_text.splitlines()
    results = []
    for i, raw_line in enumerate(lines):
        stripped = raw_line.strip()
        if not (stripped == "return" or stripped.startswith(("return ", "return("))):
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        stmt_end = _statement_end(lines, i)
        first_after = None
        j = stmt_end + 1
        while j < len(lines):
            candidate = lines[j].strip()
            if candidate == "" or candidate.startswith("#"):
                j += 1
                continue
            cur_indent = len(lines[j]) - len(lines[j].lstrip(" "))
            if cur_indent < indent:
                break
            if first_after is None:
                first_after = candidate
                if first_after.startswith(("if ", "if(", "elif ", "else")):
                    break
            results.append(j + 1)
            j += 1
    return results


def _check_unreachable_code(diffs: list) -> list:
    findings = []
    for d in diffs:
        if not _is_checker_source(d.path) or not d.new_text:
            continue
        lines = _unreachable_lines(d.new_text)
        if lines:
            findings.append(Finding("TT14", d.path, lines[0], "unreachable code after return in checker source"))
    return findings


def check_self_preservation(diffs: list) -> list:
    """TT14: the hook or the checker's own source disabled, standalone,
    no paired file required. See docs/plans/test-tampering.md for the
    four sub-cases this dispatches to."""
    findings = []
    findings += _check_hook_deleted_or_broken(diffs)
    findings += _check_checker_deleted(diffs)
    findings += _check_id_vanished(diffs)
    findings += _check_unreachable_code(diffs)
    return findings


ALL_RULES = (
    check_self_reference_guard,
    check_denylist_entry_added,
    check_weakened_floor,
    check_self_preservation,
)
