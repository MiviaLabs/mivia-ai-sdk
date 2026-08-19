#!/usr/bin/env python3
"""Gate: every top-level package needs a real internal caller, or a
declared exemption. An orphan is a package no other top-level
package's non-test .go file imports. Ground truth is grep over the
tree, not policy/layers.json, which states permission, not use. An
orphan missing from policy/pending_wiring.json fails; so does a
pending_wiring.json entry that is stale: its package no longer
exists, or it has gained a real caller since the entry was written."""
import argparse
import json
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from check_deps import IMPORT, package_dirs  # noqa: E402

REQUIRED_FIELDS = ("reason", "target", "permanent")


def compute_importers(root: Path, pkgs: list[str]) -> dict[str, set[str]]:
    """compute_importers maps each package name to the set of other
    top-level packages whose non-test .go files import it."""
    importers: dict[str, set[str]] = {p: set() for p in pkgs}
    for pkg in pkgs:
        for src in sorted((root / pkg).glob("*.go")):
            if src.name.endswith("_test.go"):
                continue
            for imp in IMPORT.findall(src.read_text()):
                if imp in importers and imp != pkg:
                    importers[imp].add(pkg)
    return importers


def find_orphans(root: Path, pkgs: list[str] | None = None) -> set[str]:
    """find_orphans returns the package names with zero non-test
    internal callers among the given (or discovered) top-level
    packages."""
    if pkgs is None:
        pkgs = package_dirs(root)
    importers = compute_importers(root, pkgs)
    return {p for p in pkgs if not importers[p]}


def load_pending_wiring(root: Path) -> dict:
    """load_pending_wiring reads policy/pending_wiring.json."""
    path = root / "policy" / "pending_wiring.json"
    return json.loads(path.read_text())


def validate_entry(name: str, entry: object) -> list[str]:
    """validate_entry checks one pending_wiring.json entry's required
    fields. Returns problem strings; empty means the entry is valid."""
    problems = []
    if not isinstance(entry, dict):
        return [f"{name}: pending_wiring.json entry is not an object"]
    for field in REQUIRED_FIELDS:
        if field not in entry:
            problems.append(f"{name}: missing required field '{field}'")
            continue
        value = entry[field]
        if field == "permanent":
            if not isinstance(value, bool):
                problems.append(f"{name}: field 'permanent' must be a boolean")
        else:
            if not isinstance(value, str) or not value.strip():
                problems.append(f"{name}: field '{field}' must be a non-empty string")
    return problems


def validate_target(name: str, target: str, pkgs: list[str]) -> list[str]:
    """validate_target checks a single-token target names a real
    package directory. A free-form (multi-word) target is prose about
    an external or test-only consumer, and is not checked."""
    token = target.strip()
    if " " in token:
        return []
    if token == "not yet planned":
        return []
    if token not in pkgs:
        return [f"{name}: target '{token}' names no top-level package directory"]
    return []


def check(root: Path) -> list[str]:
    """check runs the orphan gate against one repo root. Returns
    problem strings; empty means the gate passes."""
    problems: list[str] = []
    pkgs = package_dirs(root)
    orphans = find_orphans(root, pkgs)
    pending = load_pending_wiring(root)
    entries = pending.get("packages", {})

    for name, entry in entries.items():
        problems.extend(validate_entry(name, entry))
        if isinstance(entry, dict) and isinstance(entry.get("target"), str):
            problems.extend(validate_target(name, entry["target"], pkgs))
        if name not in pkgs:
            problems.append(
                f"{name}: stale pending_wiring.json entry, package directory no longer exists"
            )
        elif name not in orphans:
            problems.append(
                f"{name}: stale pending_wiring.json entry, package now has a real internal caller"
            )

    for orphan in sorted(orphans):
        if orphan not in entries:
            problems.append(
                f"{orphan}: orphan package with zero internal callers, "
                f"undeclared in policy/pending_wiring.json"
            )
    return problems


# --- probes ---------------------------------------------------------


def _write_pkg(root: Path, name: str, imports: list[str], test_only_import: str | None = None) -> None:
    """_write_pkg writes a minimal fixture package under root/name."""
    d = root / name
    d.mkdir(parents=True, exist_ok=True)
    body = "".join(f'\nimport "github.com/MiviaLabs/mivia-ai-sdk/{imp}"\n' for imp in imports)
    (d / f"{name}.go").write_text(f"package {name}\n{body}\nvar _ = 1\n")
    if test_only_import:
        (d / f"{name}_test.go").write_text(
            f'package {name}\n\nimport "github.com/MiviaLabs/mivia-ai-sdk/{test_only_import}"\n\nvar _ = 1\n'
        )


def _write_pending(root: Path, packages: dict) -> None:
    (root / "policy").mkdir(parents=True, exist_ok=True)
    (root / "policy" / "pending_wiring.json").write_text(
        json.dumps({"comment": "probe fixture", "packages": packages})
    )


def _probe_true_orphan(root: Path) -> list[str]:
    _write_pkg(root, "lonely", [])
    _write_pending(root, {})
    orphans = find_orphans(root, package_dirs(root))
    if "lonely" not in orphans:
        return ["probe_true_orphan: expected 'lonely' to be reported orphan"]
    return []


def _probe_caller_found(root: Path) -> list[str]:
    _write_pkg(root, "leaf", [])
    _write_pkg(root, "caller", ["leaf"])
    orphans = find_orphans(root, package_dirs(root))
    if "leaf" in orphans:
        return ["probe_caller_found: expected 'leaf' to not be reported orphan"]
    return []


def _probe_test_only_caller_ignored(root: Path) -> list[str]:
    _write_pkg(root, "leaf2", [])
    _write_pkg(root, "tester", [], test_only_import="leaf2")
    orphans = find_orphans(root, package_dirs(root))
    if "leaf2" not in orphans:
        return ["probe_test_only_caller_ignored: a test-only import must not count as a real caller"]
    return []


def _probe_pending_exempts(root: Path) -> list[str]:
    _write_pkg(root, "pendingpkg", [])
    _write_pending(root, {
        "pendingpkg": {"reason": "r", "target": "not yet planned", "permanent": False},
    })
    problems = check(root)
    if problems:
        return [f"probe_pending_exempts: expected pass, got {problems}"]
    return []


def _probe_permanent_exempts(root: Path) -> list[str]:
    _write_pkg(root, "permpkg", [])
    _write_pending(root, {
        "permpkg": {"reason": "r", "target": "external application code (outside this module)", "permanent": True},
    })
    problems = check(root)
    if problems:
        return [f"probe_permanent_exempts: expected pass, got {problems}"]
    return []


def _probe_missing_field_fails(root: Path) -> list[str]:
    _write_pkg(root, "brokenpkg", [])
    _write_pending(root, {
        "brokenpkg": {"reason": "r", "target": ""},
    })
    problems = check(root)
    if not any("brokenpkg" in p and "permanent" in p for p in problems):
        return [f"probe_missing_field_fails: expected a missing-'permanent' problem, got {problems}"]
    if not any("brokenpkg" in p and "target" in p for p in problems):
        return [f"probe_missing_field_fails: expected an empty-'target' problem, got {problems}"]
    return []


def _probe_stale_entry_fails(root: Path) -> list[str]:
    problems_all: list[str] = []

    # sub-case: declared package no longer exists as a directory.
    root_a = root / "a"
    root_a.mkdir()
    _write_pending(root_a, {
        "ghost": {"reason": "r", "target": "not yet planned", "permanent": False},
    })
    problems = check(root_a)
    if not any("ghost" in p and "no longer exists" in p for p in problems):
        problems_all.append(f"probe_stale_entry_fails (gone): expected a no-longer-exists problem, got {problems}")

    # sub-case: declared package has gained a real caller.
    root_b = root / "b"
    root_b.mkdir()
    _write_pkg(root_b, "nowcalled", [])
    _write_pkg(root_b, "caller2", ["nowcalled"])
    _write_pending(root_b, {
        "nowcalled": {"reason": "r", "target": "not yet planned", "permanent": False},
    })
    problems = check(root_b)
    if not any("nowcalled" in p and "real internal caller" in p for p in problems):
        problems_all.append(f"probe_stale_entry_fails (caller): expected a real-caller problem, got {problems}")

    return problems_all


def _probe_real_tree_matches_declared_set() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    pkgs = package_dirs(root)
    orphans = find_orphans(root, pkgs)
    pending = load_pending_wiring(root)
    declared = set(pending.get("packages", {}).keys())
    problems = []
    undeclared = orphans - declared
    if undeclared:
        problems.append(f"probe_real_tree_matches_declared_set: undeclared orphans {sorted(undeclared)}")
    stale = declared - orphans
    if stale:
        problems.append(f"probe_real_tree_matches_declared_set: stale declared entries {sorted(stale)}")
    return problems


def run_probe() -> bool:
    """run_probe exercises the gate's own logic against small,
    isolated temp-directory fixtures, following
    check_mutation.py's --probe convention."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="orphan-probe-") as tmp:
        for fn in (
            _probe_true_orphan,
            _probe_caller_found,
            _probe_test_only_caller_ignored,
            _probe_pending_exempts,
            _probe_permanent_exempts,
            _probe_missing_field_fails,
        ):
            sub = Path(tmp) / fn.__name__
            sub.mkdir()
            (sub / "policy").mkdir()
            (sub / "policy" / "pending_wiring.json").write_text(json.dumps({"packages": {}}))
            problems.extend(fn(sub))

        sub = Path(tmp) / "_probe_stale_entry_fails"
        sub.mkdir()
        problems.extend(_probe_stale_entry_fails(sub))

    problems.extend(_probe_real_tree_matches_declared_set())

    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="orphan-package gate")
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
