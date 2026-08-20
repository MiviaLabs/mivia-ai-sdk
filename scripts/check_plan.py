#!/usr/bin/env python3
"""Gate: every Go package needs a plan at docs/plans/<path>.md with the
sections from docs/plans/TEMPLATE.md. The path is the package path
relative to the module root, so a nested package needs a nested plan
file. The plan is where an agent declares the package's goal, scope,
API, tests, and verification BEFORE or WITH the code; the gate makes the
structure non-optional."""
import argparse
import re
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import go_packages  # noqa: E402

REQUIRED = ["## Goal", "## Scope", "## API", "## Tests", "## Verification"]


def check(root: Path, env_extra: dict | None = None) -> list[str]:
    """check runs the plan gate against one repo root. Returns problem
    strings; empty means the gate passes."""
    problems = []
    for pkg in go_packages.package_paths(root, env_extra):
        plan = root / "docs" / "plans" / f"{pkg}.md"
        if not plan.exists():
            problems.append(f"{pkg}: no plan; create docs/plans/{pkg}.md from TEMPLATE.md")
            continue
        text = plan.read_text()
        for section in REQUIRED:
            if not re.search(rf"^{re.escape(section)}\s*$", text, re.M):
                problems.append(f"{pkg}: plan lacks section {section!r}")
    return problems


# --- probes ---------------------------------------------------------


def _write_fixture(root: Path) -> None:
    """_write_fixture writes a module holding one nested package."""
    go_packages.write_file(root, "go.mod", f"module {go_packages.MODULE}\n\ngo 1.25.0\n")
    go_packages.write_file(root, "flow/engine/engine.go", "package engine\n\nvar Run = 1\n")


def _plan_text(sections: list[str]) -> str:
    return "# Plan\n\n" + "\n\n".join(f"{s}\n\nText.\n" for s in sections)


def _probe_nested_without_plan_fails(root: Path) -> list[str]:
    _write_fixture(root)
    problems = check(root, go_packages.probe_env())
    if not any("flow/engine: no plan" in p for p in problems):
        return [f"probe_nested_without_plan_fails: expected a no-plan problem, got {problems}"]
    return []


def _probe_nested_with_plan_passes(root: Path) -> list[str]:
    _write_fixture(root)
    go_packages.write_file(root, "docs/plans/flow/engine.md", _plan_text(REQUIRED))
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_nested_with_plan_passes: expected pass, got {problems}"]
    return []


def _probe_missing_section_fails(root: Path) -> list[str]:
    _write_fixture(root)
    go_packages.write_file(root, "docs/plans/flow/engine.md", _plan_text(REQUIRED[:-1]))
    problems = check(root, go_packages.probe_env())
    if not any("plan lacks section '## Verification'" in p for p in problems):
        return [f"probe_missing_section_fails: expected a missing-section problem, got {problems}"]
    return []


def _probe_real_tree_passes() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    problems = check(root)
    if problems:
        return [f"probe_real_tree_passes: expected pass, got {problems}"]
    return []


def run_probe() -> bool:
    """run_probe exercises the plan gate against temp-module fixtures,
    following check_mutation.py's --probe convention."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="plan-probe-") as tmp:
        for fn in (
            _probe_nested_without_plan_fails,
            _probe_nested_with_plan_passes,
            _probe_missing_section_fails,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            problems.extend(fn(sub))
    problems.extend(_probe_real_tree_passes())
    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="package plan gate")
    parser.add_argument("--probe", action="store_true", help="run the gate's own probe suite")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    root = Path(__file__).resolve().parent.parent
    problems = check(root)
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
