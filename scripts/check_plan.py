#!/usr/bin/env python3
"""Gate: every top-level Go package needs a plan at docs/plans/<pkg>.md
with the sections from docs/plans/TEMPLATE.md. The plan is where an
agent declares the package's goal, scope, API, tests, and verification
BEFORE or WITH the code; the gate makes the structure non-optional."""
import re
import sys
from pathlib import Path

REQUIRED = ["## Goal", "## Scope", "## API", "## Tests", "## Verification"]


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    problems = []
    for d in sorted(root.iterdir()):
        if not d.is_dir() or d.name.startswith(".") or d.name == "scripts":
            continue
        if not any(d.glob("*.go")):
            continue
        plan = root / "docs" / "plans" / f"{d.name}.md"
        if not plan.exists():
            problems.append(f"{d.name}: no plan; create docs/plans/{d.name}.md from TEMPLATE.md")
            continue
        text = plan.read_text()
        for section in REQUIRED:
            if not re.search(rf"^{re.escape(section)}\s*$", text, re.M):
                problems.append(f"{d.name}: plan lacks section {section!r}")
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
