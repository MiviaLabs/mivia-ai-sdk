#!/usr/bin/env python3
"""Enforce timeout saturation: no unbounded int-to-Duration multiply.

Mechanism: time.Duration is int64. `time.Duration(n) * time.Second`
overflows to a negative Duration for n above ~9.2e9 (proportionally lower
for larger units), and a negative bound arms an already-expired deadline
(context.WithTimeout), an instantly-elapsed retention, or a never-firing
schedule. A conversion of a runtime integer must bound the value BEFORE
the multiply and carry a disposition entry in the policy naming that
guard.

Matched forms in non-test Go files, both operand orders plus the compound
assign that builds the conversion across statements:
  time.Duration(n) * time.<Unit>
  time.<Unit> * time.Duration(n)
  x *= time.<Unit>

Two failure modes, both exit 1: a match with no allow entry (new
unguarded conversion), and an allow entry with no match (stale
disposition), so the list cannot rot. Compile-time constant multiplies
without a time.Duration() conversion cannot overflow at runtime and are
out of scope.

Ported from mivia-agent's scripts/check_timeout_saturation.py (DC-7 in
its defect taxonomy). Policy: policy/timeout-saturation.json.

Modes:
  (default)   check the tree against the committed policy
  --probe     self-test the matchers on synthetic fixtures

Exit codes:
  0 = OK
  1 = violation (unlisted conversion, or stale allow entry)
  2 = usage / malformed policy error (fail closed, never a silent pass)
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
POLICY_REL = Path("policy") / "timeout-saturation.json"
SKIP_DIRS = {".git", ".claude", "semgrep", "scripts"}

CONVERSION_RE = re.compile(
    r"time\.Duration\((?P<operand>[^()]*(?:\([^()]*\)[^()]*)*)\)"
    r"\s*\*\s*time\.(?P<unit>Second|Minute|Hour|Millisecond)\b"
)
REVERSED_RE = re.compile(
    r"time\.(?P<unit>Second|Minute|Hour|Millisecond)\s*\*\s*"
    r"time\.Duration\((?P<operand>[^()]*(?:\([^()]*\)[^()]*)*)\)"
)
COMPOUND_RE = re.compile(
    r"(?P<operand>[\w.\[\]]+)\s*\*=\s*time\.(?P<unit>Second|Minute|Hour|Millisecond)\b"
)


def fail(msg: str, *, code: int = 1) -> None:
    print(f"check_timeout_saturation: {msg}", file=sys.stderr)
    raise SystemExit(code)


def normalize(operand: str) -> str:
    return re.sub(r"\s+", "", operand)


def load_policy(root: Path) -> list[dict]:
    path = root / POLICY_REL
    if not path.is_file():
        fail(f"missing policy {POLICY_REL}", code=2)
    try:
        policy = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as err:
        fail(f"malformed policy {POLICY_REL}: {err}", code=2)
    allow = policy.get("allow")
    if not isinstance(allow, list):
        fail(f"policy {POLICY_REL}: 'allow' must be a list", code=2)
    for i, entry in enumerate(allow):
        for key in ("file", "operand", "unit", "reason"):
            if not isinstance(entry.get(key), str) or not entry[key].strip():
                fail(f"policy {POLICY_REL}: allow[{i}] needs non-empty '{key}'", code=2)
    return allow


def go_files(root: Path):
    for path in sorted(root.rglob("*.go")):
        rel_parts = path.relative_to(root).parts
        if any(part in SKIP_DIRS or part.startswith(".") for part in rel_parts[:-1]):
            continue
        if path.name.endswith("_test.go"):
            continue
        yield path


def scan(root: Path) -> list[tuple[str, int, str, str, str]]:
    """Return (relpath, line, operand, unit, snippet) per conversion match."""
    hits: list[tuple[str, int, str, str, str]] = []
    for path in go_files(root):
        rel = path.relative_to(root).as_posix()
        for lineno, line in enumerate(
            path.read_text(encoding="utf-8").splitlines(), start=1
        ):
            # Comment text is not executable; a conversion named in a doc
            # comment is documentation, not a site.
            code = line.split("//", 1)[0]
            for pattern in (CONVERSION_RE, REVERSED_RE, COMPOUND_RE):
                for m in pattern.finditer(code):
                    hits.append(
                        (
                            rel,
                            lineno,
                            normalize(m.group("operand")),
                            m.group("unit"),
                            m.group(0).strip(),
                        )
                    )
    return hits


def check(root: Path) -> None:
    allow = load_policy(root)
    hits = scan(root)
    allowed = {(e["file"], normalize(e["operand"]), e["unit"]) for e in allow}
    used: set[tuple[str, str, str]] = set()
    violations: list[str] = []
    for rel, lineno, operand, unit, snippet in hits:
        key = (rel, operand, unit)
        if key in allowed:
            used.add(key)
            continue
        violations.append(
            f"{rel}:{lineno}: unbounded duration conversion: {snippet}\n"
            f"  bound the value before the multiply and add a dispositioned "
            f"entry to {POLICY_REL}"
        )
    for key in sorted(allowed - used):
        violations.append(
            f"stale policy entry (matches no site): file={key[0]} operand={key[1]} "
            f"unit={key[2]} - remove it from {POLICY_REL}"
        )
    if violations:
        for v in violations:
            print(f"check_timeout_saturation: {v}", file=sys.stderr)
        raise SystemExit(1)
    print(f"check_timeout_saturation: ok ({len(hits)} conversions, all dispositioned)")


def probe() -> None:
    if not CONVERSION_RE.search("\td := time.Duration(cfg.N) * time.Second\n"):
        fail("probe: matcher no longer flags a bare conversion", code=1)
    if CONVERSION_RE.search("\td := saturatingSeconds(cfg.N)\n"):
        fail("probe: matcher flags a non-conversion call", code=1)
    m = CONVERSION_RE.search("\td := time.Duration(f(a, b)) * time.Second\n")
    if not m or normalize(m.group("operand")) != "f(a,b)":
        fail("probe: matcher mishandles a nested-call operand", code=1)
    if not REVERSED_RE.search("\td := time.Second * time.Duration(cfg.N)\n"):
        fail("probe: matcher no longer flags the reversed operand order", code=1)
    if not COMPOUND_RE.search("\td *= time.Second\n"):
        fail("probe: matcher no longer flags a compound assign", code=1)
    print("check_timeout_saturation: probe ok")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=str(ROOT))
    parser.add_argument("--probe", action="store_true")
    args = parser.parse_args()
    if args.probe:
        probe()
        return
    check(Path(args.root).resolve())


if __name__ == "__main__":
    main()
