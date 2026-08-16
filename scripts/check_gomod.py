#!/usr/bin/env python3
"""Gate: go.mod carries no require, replace, exclude, or retract
directives. The SDK is standard library only; a directive needs a
deliberate policy change before it lands."""
import re
import sys
from pathlib import Path

BLOCKED = ("require", "replace", "exclude", "retract")
DIRECTIVE = re.compile(r"^(require|replace|exclude|retract)\b")


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    gomod = root / "go.mod"
    if not gomod.exists():
        print("go.mod: missing")
        return 1
    problems = []
    for n, line in enumerate(gomod.read_text().splitlines(), 1):
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        m = DIRECTIVE.match(stripped)
        if m:
            problems.append(f"go.mod:{n}: directive {m.group(1)} is forbidden; standard library only")
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
