#!/usr/bin/env python3
"""Gate: every exported symbol needs a real caller, or a declared
exemption. check_orphan_packages.py catches a whole unimported
package; this gate catches one exported func, type, or var inside a
package that itself has other, legitimate callers - the shape that
slipped past every other gate in 2026-09: a tested symbol with zero
production reference anywhere.

A symbol is read from api/<pkg>.txt (the locked surface, so this gate
never needs its own Go parser). Its name is searched, word-bounded,
across every non-test .go file in the module, and in an external
consumer checkout when one is named via --sibling or the
SDK_CONSUMER_PATH environment variable. This gate names no consumer
repo; a caller supplies the path or the check runs module-only. Two
or more occurrences of the name - its own declaration plus at least
one real reference - clears the symbol; fewer needs a
policy/pending_symbols.json entry, shaped like pending_wiring.json:
reason, target, permanent.

This is a name-text search, not a type-checked reference count, so it
undercounts problems on purpose: a common short name (New, Validate)
will read as used from unrelated matches elsewhere, and stays silent.
The gate is precision-first. It fails only on a name distinctive
enough that two or more occurrences genuinely means "nothing but the
declaration.\""""
import argparse
import json
import os
import re
import sys
import tempfile
from pathlib import Path

REQUIRED_FIELDS = ("reason", "target", "permanent")
_CONST_RE = re.compile(r"^  const (\w+)")
_VAR_RE = re.compile(r"^  var (\w+)")
_TYPE_RE = re.compile(r"^  type (\w+)")
_FUNC_RE = re.compile(r"^  func (\w+)\(")
_METHOD_RE = re.compile(r"^  func \([^)]+\) (\w+)\(")


def parse_symbols(text: str) -> list[str]:
    """parse_symbols extracts top-level exported names from one
    api/<pkg>.txt lock. Struct fields and interface methods sit inside
    a brace body opened by a top-level line ending in "{" and closed
    by a bare "}"; both are skipped, since a field or method is not
    independently callable - its enclosing type is the symbol that
    needs a caller."""
    names: list[str] = []
    depth = 0
    for line in text.splitlines():
        if depth > 0:
            if line == "}":
                depth = 0
            continue
        for pattern in (_CONST_RE, _VAR_RE, _TYPE_RE, _FUNC_RE, _METHOD_RE):
            m = pattern.match(line)
            if m:
                names.append(m.group(1))
                break
        if line.endswith("{"):
            depth = 1
    return names


def iter_lock_files(root: Path):
    """iter_lock_files yields every api/*.txt lock, package path
    first (mirroring the file path under api/), then the file."""
    api_dir = root / "api"
    if not api_dir.is_dir():
        return
    for path in sorted(api_dir.rglob("*.txt")):
        pkg = str(path.relative_to(api_dir))[: -len(".txt")]
        yield pkg, path


def load_pending_symbols(root: Path) -> dict:
    path = root / "policy" / "pending_symbols.json"
    if not path.is_file():
        return {"symbols": {}}
    return json.loads(path.read_text())


def validate_entry(key: str, entry: object) -> list[str]:
    problems = []
    if not isinstance(entry, dict):
        return [f"{key}: pending_symbols.json entry is not an object"]
    for field in REQUIRED_FIELDS:
        if field not in entry:
            problems.append(f"{key}: missing required field '{field}'")
            continue
        value = entry[field]
        if field == "permanent":
            if not isinstance(value, bool):
                problems.append(f"{key}: field 'permanent' must be a boolean")
        elif not isinstance(value, str) or not value.strip():
            problems.append(f"{key}: field '{field}' must be a non-empty string")
    return problems


def _iter_go_files(root: Path):
    for path in root.rglob("*.go"):
        rel = path.relative_to(root)
        parts = rel.parts
        if any(p.startswith(".") for p in parts):
            continue
        if parts and parts[0] == "docs" and "examples" not in parts:
            continue
        if path.name.endswith("_test.go"):
            continue
        yield path


_LINE_COMMENT = re.compile(r"//.*$")


def _strip_line_comments(text: str) -> str:
    """_strip_line_comments removes a trailing "//" comment from every
    line. A doc comment naming its own symbol ("NewScopeChecked
    builds a Scope like NewScope...") would otherwise inflate the
    occurrence count past the declaration itself, masking exactly the
    zero-caller case this gate exists to catch. Crude on a "//" inside
    a string literal or URL, which undercounts real usages in the
    safe direction for a gate that already favors precision over
    recall."""
    return "\n".join(_LINE_COMMENT.sub("", line) for line in text.splitlines())


def count_occurrences(name: str, files) -> int:
    pattern = re.compile(r"\b" + re.escape(name) + r"\b")
    total = 0
    for path in files:
        try:
            text = path.read_text(errors="ignore")
        except OSError:
            continue
        total += len(pattern.findall(_strip_line_comments(text)))
        if total >= 2:
            return total
    return total


def sibling_repo(root: Path, override: str | None) -> Path | None:
    """sibling_repo resolves an external consumer checkout from
    --sibling, falling back to the SDK_CONSUMER_PATH environment
    variable. Neither names a specific repo; both are the caller's
    choice. No path, or a path that does not exist, means the check
    runs module-only."""
    value = override or os.environ.get("SDK_CONSUMER_PATH")
    if not value:
        return None
    candidate = Path(value)
    return candidate if candidate.is_dir() else None


def check(root: Path, sibling: Path | None = None) -> list[str]:
    """check runs the symbol-wiring gate against one repo root.
    Returns problem strings; empty means the gate passes."""
    problems: list[str] = []
    pending = load_pending_symbols(root)
    entries = pending.get("symbols", {})
    for key, entry in entries.items():
        problems.extend(validate_entry(key, entry))

    module_files = list(_iter_go_files(root))
    sibling_files = list(_iter_go_files(sibling)) if sibling else []

    seen_keys: set[str] = set()
    for pkg, lock_path in iter_lock_files(root):
        for name in parse_symbols(lock_path.read_text()):
            key = f"{pkg}.{name}"
            seen_keys.add(key)
            if key in entries:
                continue
            hits = count_occurrences(name, module_files)
            if hits >= 2:
                continue
            if sibling_files and count_occurrences(name, sibling_files) >= 1:
                continue
            problems.append(
                f"{key}: exported symbol with no non-test reference in the "
                f"module or the sibling consumer, undeclared in "
                f"policy/pending_symbols.json"
            )

    for key in entries:
        if key not in seen_keys:
            problems.append(
                f"{key}: stale pending_symbols.json entry, symbol no longer "
                f"appears in any api/*.txt lock"
            )
    return problems


# --- probes ---------------------------------------------------------


def _write(root: Path, rel: str, text: str) -> None:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)


def _probe_undeclared_dead_symbol_fails(root: Path) -> list[str]:
    _write(root, "api/leaf.txt", "package leaf\n  func LonelyHelper() (error)\n")
    _write(root, "leaf/leaf.go", "package leaf\n\nfunc LonelyHelper() error { return nil }\n")
    _write(root, "policy/pending_symbols.json", json.dumps({"symbols": {}}))
    problems = check(root)
    if not any("leaf.LonelyHelper" in p for p in problems):
        return [f"probe_undeclared_dead_symbol_fails: expected a finding, got {problems}"]
    return []


def _probe_real_caller_passes(root: Path) -> list[str]:
    _write(root, "api/leaf.txt", "package leaf\n  func UsedHelper() (error)\n")
    _write(root, "leaf/leaf.go", "package leaf\n\nfunc UsedHelper() error { return nil }\n")
    _write(root, "caller/caller.go", "package caller\n\nimport \"m/leaf\"\n\nvar _ = leaf.UsedHelper\n")
    _write(root, "policy/pending_symbols.json", json.dumps({"symbols": {}}))
    problems = check(root)
    if any("leaf.UsedHelper" in p for p in problems):
        return [f"probe_real_caller_passes: expected no finding, got {problems}"]
    return []


def _probe_pending_entry_exempts(root: Path) -> list[str]:
    _write(root, "api/leaf.txt", "package leaf\n  func StagedHelper() (error)\n")
    _write(root, "leaf/leaf.go", "package leaf\n\nfunc StagedHelper() error { return nil }\n")
    _write(root, "policy/pending_symbols.json", json.dumps({
        "symbols": {
            "leaf.StagedHelper": {"reason": "r", "target": "not yet planned", "permanent": False},
        },
    }))
    problems = check(root)
    if any("leaf.StagedHelper" in p for p in problems):
        return [f"probe_pending_entry_exempts: expected no finding, got {problems}"]
    return []


def _probe_stale_pending_entry_fails(root: Path) -> list[str]:
    _write(root, "api/leaf.txt", "package leaf\n  func StillHere() (error)\n")
    _write(root, "leaf/leaf.go", "package leaf\n\nfunc StillHere() error { return nil }\n")
    _write(root, "caller/caller.go", "package caller\n\nimport \"m/leaf\"\n\nvar _ = leaf.StillHere\n")
    _write(root, "policy/pending_symbols.json", json.dumps({
        "symbols": {
            "leaf.GoneNow": {"reason": "r", "target": "not yet planned", "permanent": False},
        },
    }))
    problems = check(root)
    if not any("leaf.GoneNow" in p and "stale" in p for p in problems):
        return [f"probe_stale_pending_entry_fails: expected a stale-entry finding, got {problems}"]
    return []


def _probe_struct_fields_and_interface_methods_skipped(root: Path) -> list[str]:
    _write(root, "api/leaf.txt", (
        "package leaf\n"
        "  type Config struct {\n"
        "  Timeout int\n"
        "}\n"
        "  type Runner interface {\n"
        "\tRun() error\n"
        "}\n"
    ))
    _write(root, "leaf/leaf.go", "package leaf\n\ntype Config struct{ Timeout int }\ntype Runner interface{ Run() error }\n")
    _write(root, "caller/caller.go", "package caller\n\nimport \"m/leaf\"\n\nvar _ = leaf.Config{}\nvar _ leaf.Runner\n")
    _write(root, "policy/pending_symbols.json", json.dumps({"symbols": {}}))
    problems = check(root)
    if any("Timeout" in p or "Runner.Run" in p for p in problems):
        return [f"probe_struct_fields_and_interface_methods_skipped: field/method wrongly treated as a symbol, got {problems}"]
    return []


def _probe_self_referencing_doc_comment_still_fails(root: Path) -> list[str]:
    """A doc comment that names its own symbol, with no real caller
    anywhere, must still fail: this is the exact NewScopeChecked case
    that motivated this gate."""
    _write(root, "api/leaf.txt", "package leaf\n  func Documented() (error)\n")
    _write(root, "leaf/leaf.go", (
        "package leaf\n\n"
        "// Documented does the thing. See Documented for details.\n"
        "func Documented() error { return nil }\n"
    ))
    _write(root, "policy/pending_symbols.json", json.dumps({"symbols": {}}))
    problems = check(root)
    if not any("leaf.Documented" in p for p in problems):
        return [f"probe_self_referencing_doc_comment_still_fails: expected a finding, got {problems}"]
    return []


def _probe_sibling_caller_passes(root: Path) -> list[str]:
    sdk = root / "sdk"
    sibling = root / "sibling"
    _write(sdk, "api/leaf.txt", "package leaf\n  func BridgedHelper() (error)\n")
    _write(sdk, "leaf/leaf.go", "package leaf\n\nfunc BridgedHelper() error { return nil }\n")
    _write(sdk, "policy/pending_symbols.json", json.dumps({"symbols": {}}))
    _write(sibling, "internal/app.go", "package app\n\nfunc use() { leaf.BridgedHelper() }\n")
    problems = check(sdk, sibling)
    if any("leaf.BridgedHelper" in p for p in problems):
        return [f"probe_sibling_caller_passes: expected no finding, got {problems}"]
    return []


def run_probe() -> bool:
    """run_probe exercises the gate's own logic against small,
    isolated temp-directory fixtures, following
    check_orphan_packages.py's --probe convention."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="symbol-wiring-probe-") as tmp:
        for fn in (
            _probe_undeclared_dead_symbol_fails,
            _probe_real_caller_passes,
            _probe_pending_entry_exempts,
            _probe_stale_pending_entry_fails,
            _probe_struct_fields_and_interface_methods_skipped,
            _probe_self_referencing_doc_comment_still_fails,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            problems.extend(fn(sub))
        problems.extend(_probe_sibling_caller_passes(Path(tmp) / "_probe_sibling_caller_passes"))

    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="symbol-wiring gate")
    parser.add_argument("--probe", action="store_true", help="run the gate's own probe suite")
    parser.add_argument("--sibling", help="path to an external consumer repo checkout")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    root = Path(__file__).resolve().parent.parent
    sibling = sibling_repo(root, args.sibling)
    problems = check(root, sibling)
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
