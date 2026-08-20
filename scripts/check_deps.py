#!/usr/bin/env python3
"""Gate: internal imports must follow policy/layers.json. A package may
import only the internal packages the policy lists; a package missing
from the policy fails. Packages and policy rows are keyed by the path
relative to the module root, so a nested package is visible. Test files
are exempt (integration tests cross layers on purpose)."""
import argparse
import json
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import go_packages  # noqa: E402

MODULE = go_packages.MODULE


def load_policy(root: Path) -> dict:
    """load_policy reads the allowed_imports table of layers.json."""
    return json.loads((root / "policy" / "layers.json").read_text())["allowed_imports"]


def check(root: Path, env_extra: dict | None = None) -> list[str]:
    """check runs the deps gate against one repo root. Returns problem
    strings; empty means the gate passes."""
    policy = load_policy(root)
    problems = []
    for pkg, imports in sorted(go_packages.packages(root, env_extra).items()):
        if pkg not in policy:
            problems.append(f"{pkg}: missing from policy/layers.json; declare its allowed imports first")
            continue
        allowed = set(policy[pkg])
        for imp in sorted(imports):
            if imp not in allowed:
                problems.append(
                    f"{pkg}: imports {imp}, not allowed by policy/layers.json "
                    f"(allowed: {sorted(allowed) or 'none'})"
                )
    return problems


# --- probes ---------------------------------------------------------


def _write_fixture(root: Path, policy: dict) -> None:
    """_write_fixture writes a module with a flat package importing a
    nested package, plus the given policy table."""
    go_packages.write_file(root, "go.mod", f"module {MODULE}\n\ngo 1.25.0\n")
    go_packages.write_file(root, "flow/engine/engine.go", "package engine\n\nvar Run = 1\n")
    go_packages.write_file(root, "agent/agent.go", (
        "package agent\n\n"
        f'import "{MODULE}/flow/engine"\n\n'
        "var _ = engine.Run\n"
    ))
    go_packages.write_file(root, "policy/layers.json", json.dumps({"allowed_imports": policy}))


def _probe_nested_missing_from_policy_fails(root: Path) -> list[str]:
    _write_fixture(root, {"agent": ["flow/engine"]})
    problems = check(root, go_packages.probe_env())
    if not any(p.startswith("flow/engine: missing from policy") for p in problems):
        return [f"probe_nested_missing_from_policy_fails: expected a missing-policy problem, got {problems}"]
    return []


def _probe_nested_edge_allowed_passes(root: Path) -> list[str]:
    _write_fixture(root, {"agent": ["flow/engine"], "flow/engine": []})
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_nested_edge_allowed_passes: expected pass, got {problems}"]
    return []


def _probe_flat_importing_nested_fails(root: Path) -> list[str]:
    _write_fixture(root, {"agent": [], "flow/engine": []})
    problems = check(root, go_packages.probe_env())
    if not any("agent: imports flow/engine" in p for p in problems):
        return [f"probe_flat_importing_nested_fails: expected a forbidden-edge problem, got {problems}"]
    return []


def _probe_real_tree_passes() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    problems = check(root)
    if problems:
        return [f"probe_real_tree_passes: expected pass, got {problems}"]
    return []


def run_probe() -> bool:
    """run_probe exercises the enumeration helper and the deps gate
    against temp-module fixtures, following check_mutation.py's --probe
    convention."""
    problems = go_packages.run_probe()
    with tempfile.TemporaryDirectory(prefix="deps-probe-") as tmp:
        for fn in (
            _probe_nested_missing_from_policy_fails,
            _probe_nested_edge_allowed_passes,
            _probe_flat_importing_nested_fails,
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
    parser = argparse.ArgumentParser(description="internal-import policy gate")
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
