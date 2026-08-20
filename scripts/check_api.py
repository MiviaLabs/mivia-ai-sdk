#!/usr/bin/env python3
"""Gate: the exported API surface must match the locks in api/. A lock
path mirrors the package path, so a nested package locks at
api/<path>.txt. A deliberate surface change updates the lock with `make
api-update` and commits the diff. A missing lock, drift, or orphan lock
fails the gate."""
import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import go_packages  # noqa: E402

TOOL = "scripts/api_surface.go"


def run_surface(root: Path, env_extra: dict | None = None) -> subprocess.CompletedProcess:
    """run_surface runs the surface tool in root and returns the result."""
    env = dict(os.environ)
    env.update(env_extra or {})
    return subprocess.run(
        ["go", "run", TOOL], cwd=root, capture_output=True, text=True, env=env,
    )


def parse_surface(stdout: str) -> dict[str, str]:
    """parse_surface splits the tool output into one body per package
    path. The `package ` header line stays part of the body."""
    surface: dict[str, str] = {}
    name = None
    for line in stdout.splitlines():
        if line.startswith("package "):
            name = line.split()[1]
            surface[name] = line + "\n"
        elif name is not None:
            surface[name] += line + "\n"
    return surface


def current_surface(root: Path, env_extra: dict | None = None) -> dict[str, str]:
    """current_surface returns the exported surface of every package.
    A tool failure exits the gate; it is never an empty surface."""
    out = run_surface(root, env_extra)
    if out.returncode != 0:
        print(out.stderr)
        sys.exit(1)
    return parse_surface(out.stdout)


def lock_paths(root: Path) -> dict[str, Path]:
    """lock_paths maps each lock's package path to its file. The key is
    the path under api/ without the .txt suffix."""
    api = root / "api"
    if not api.is_dir():
        return {}
    locks = {}
    for path in api.rglob("*.txt"):
        rel = path.relative_to(api).as_posix()
        locks[rel[: -len(".txt")]] = path
    return locks


def check(root: Path, env_extra: dict | None = None) -> list[str]:
    """check runs the API gate against one repo root. Returns problem
    strings; empty means the gate passes."""
    surface = current_surface(root, env_extra)
    locks = lock_paths(root)
    problems = []
    for pkg, body in sorted(surface.items()):
        lock = locks.get(pkg)
        if lock is None:
            problems.append(f"{pkg}: no api lock; run `make api-update`")
        elif lock.read_text() != body:
            problems.append(
                f"{pkg}: API surface drifted; deliberate change? run `make api-update` and commit the diff"
            )
    for pkg in sorted(locks):
        if pkg not in surface:
            problems.append(f"{pkg}: lock exists but package is gone; remove the lock deliberately")
    return problems


# --- probes ---------------------------------------------------------


def _api_update_recipe(root: Path) -> str:
    """_api_update_recipe extracts the real api-update recipe from the
    repo Makefile, so a probe exercises the shipped recipe."""
    lines = (root / "Makefile").read_text().splitlines()
    out = []
    inside = False
    for line in lines:
        if line.startswith("api-update:"):
            inside = True
            out.append(line)
            continue
        if inside:
            if line.startswith("\t"):
                out.append(line)
                continue
            break
    return "\n".join(out) + "\n"


def _fixture(root: Path) -> None:
    """_fixture writes a module that carries the surface tool and the
    real api-update recipe."""
    repo = Path(__file__).resolve().parent.parent
    go_packages.write_file(root, "go.mod", f"module {go_packages.MODULE}\n\ngo 1.25.0\n")
    (root / "scripts").mkdir(parents=True, exist_ok=True)
    shutil.copy(repo / TOOL, root / TOOL)
    go_packages.write_file(root, "Makefile", _api_update_recipe(repo))


def _nested_and_flat(root: Path) -> None:
    go_packages.write_file(root, "flow/engine/engine.go", "package engine\n\nvar Run = 1\n")
    go_packages.write_file(root, "room/room.go", "package room\n\nvar Join = 1\n")


def _make_api_update(root: Path) -> subprocess.CompletedProcess:
    env = dict(os.environ)
    env.update(go_packages.probe_env())
    return subprocess.run(
        ["make", "api-update"], cwd=root, capture_output=True, text=True, env=env,
    )


def _probe_nested_lock_missing_then_present(root: Path) -> list[str]:
    _fixture(root)
    _nested_and_flat(root)
    problems = check(root, go_packages.probe_env())
    if not any("flow/engine: no api lock" in p for p in problems):
        return [f"probe_nested_lock_missing_then_present: expected a no-lock problem, got {problems}"]
    out = _make_api_update(root)
    if out.returncode != 0:
        return [f"probe_nested_lock_missing_then_present: make api-update failed:\n{out.stderr}"]
    if not (root / "api" / "flow" / "engine.txt").is_file():
        return ["probe_nested_lock_missing_then_present: api/flow/engine.txt was not written"]
    problems = check(root, go_packages.probe_env())
    if problems:
        return [f"probe_nested_lock_missing_then_present: expected pass, got {problems}"]
    return []


def _probe_orphan_lock_fails(root: Path) -> list[str]:
    _fixture(root)
    _nested_and_flat(root)
    if _make_api_update(root).returncode != 0:
        return ["probe_orphan_lock_fails: make api-update failed"]
    go_packages.write_file(root, "api/ghost.txt", "package ghost\n")
    problems = check(root, go_packages.probe_env())
    if not any("ghost: lock exists but package is gone" in p for p in problems):
        return [f"probe_orphan_lock_fails: expected an orphan-lock problem, got {problems}"]
    return []


def _probe_recipe_keeps_flat_locks(root: Path) -> list[str]:
    """_probe_recipe_keeps_flat_locks pins the fault the awk recipe had:
    the first nested package aborted the stream and left api/ empty."""
    _fixture(root)
    _nested_and_flat(root)
    go_packages.write_file(root, "api/stale.txt", "package stale\n")
    out = _make_api_update(root)
    if out.returncode != 0:
        return [f"probe_recipe_keeps_flat_locks: make api-update failed:\n{out.stderr}"]
    problems = []
    if not (root / "api" / "flow" / "engine.txt").is_file():
        problems.append("probe_recipe_keeps_flat_locks: the nested lock is missing")
    room = root / "api" / "room.txt"
    if not room.is_file() or "var Join" not in room.read_text():
        problems.append("probe_recipe_keeps_flat_locks: the flat lock after the nested one is missing")
    if not (root / "api" / "stale.txt").is_file():
        problems.append("probe_recipe_keeps_flat_locks: a stale lock must stay on disk")
    return problems


def _probe_underscore_dir_is_silent(root: Path) -> list[str]:
    _fixture(root)
    _nested_and_flat(root)
    go_packages.write_file(root, "docs/examples/_demo/main.go", "package main\n\nfunc main() {}\n")
    out = run_surface(root, go_packages.probe_env())
    if out.returncode != 0:
        return [f"probe_underscore_dir_is_silent: expected exit 0, got {out.returncode}:\n{out.stderr}"]
    if "_demo" in out.stdout:
        return ["probe_underscore_dir_is_silent: a `_`-prefixed directory must produce no header"]
    return []


def _probe_package_name_mismatch_is_named(root: Path) -> list[str]:
    _fixture(root)
    go_packages.write_file(root, "wrongname/other.go", "package other\n\nvar X = 1\n")
    out = run_surface(root, go_packages.probe_env())
    if out.returncode == 0:
        return ["probe_package_name_mismatch_is_named: expected a non-zero exit"]
    if "does not match directory name" not in out.stderr:
        return [f"probe_package_name_mismatch_is_named: expected a named error, got {out.stderr!r}"]
    return []


def _probe_tag_gated_package_has_header(root: Path) -> list[str]:
    _fixture(root)
    go_packages.write_file(root, "tagged/tagged.go", (
        "//go:build ledger_sqlite\n\npackage tagged\n\nvar Gated = 1\n"
    ))
    out = run_surface(root, go_packages.probe_env())
    if out.returncode != 0:
        return [f"probe_tag_gated_package_has_header: expected exit 0:\n{out.stderr}"]
    if "package tagged" not in out.stdout:
        return ["probe_tag_gated_package_has_header: the second tag run did not happen"]
    return []


def _probe_real_tree_passes() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    problems = check(root)
    if problems:
        return [f"probe_real_tree_passes: expected pass, got {problems}"]
    return []


def run_probe() -> bool:
    """run_probe exercises the API gate and the api-update recipe against
    temp-module fixtures, following check_mutation.py's --probe
    convention."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="api-probe-") as tmp:
        for fn in (
            _probe_nested_lock_missing_then_present,
            _probe_orphan_lock_fails,
            _probe_recipe_keeps_flat_locks,
            _probe_underscore_dir_is_silent,
            _probe_package_name_mismatch_is_named,
            _probe_tag_gated_package_has_header,
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
    parser = argparse.ArgumentParser(description="exported API lock gate")
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
