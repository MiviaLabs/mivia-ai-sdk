#!/usr/bin/env python3
"""Gate: the exported API surface must match the locks in api/.
A deliberate surface change updates the lock with `make api-update` and
commits the diff. A missing lock, drift, or orphan lock fails the gate."""
import subprocess
import sys
from pathlib import Path


def current_surface(root: Path) -> dict[str, str]:
    out = subprocess.run(
        ["go", "run", "scripts/api_surface.go"],
        cwd=root, capture_output=True, text=True,
    )
    if out.returncode != 0:
        print(out.stderr)
        sys.exit(1)
    surface: dict[str, str] = {}
    name = None
    for line in out.stdout.splitlines():
        if line.startswith("package "):
            name = line.split()[1]
            surface[name] = line + "\n"
        elif name is not None:
            surface[name] += line + "\n"
    return surface


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    surface = current_surface(root)
    locks = {p.stem: p for p in (root / "api").glob("*.txt")}
    problems = []
    for pkg, body in sorted(surface.items()):
        lock = root / "api" / f"{pkg}.txt"
        if not lock.exists():
            problems.append(f"{pkg}: no api lock; run `make api-update`")
        elif lock.read_text() != body:
            problems.append(
                f"{pkg}: API surface drifted; deliberate change? run `make api-update` and commit the diff"
            )
    for pkg in sorted(locks):
        if pkg not in surface:
            problems.append(f"{pkg}: lock exists but package is gone; remove the lock deliberately")
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
