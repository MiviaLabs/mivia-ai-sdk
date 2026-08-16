#!/usr/bin/env python3
"""Gate: mechanical structure limits for Go code.
Rules: file <= 500 lines, function <= 80 lines. Function length is
measured from a top-level "func" line to its closing brace at column 0,
which gofmt guarantees. Exits non-zero on violations."""
import re
import sys
from pathlib import Path

MAX_FILE_LINES = 500
MAX_FUNC_LINES = 80
FUNC_START = re.compile(r"^func\s")


def check_file(path: Path, rel: Path) -> list[str]:
    lines = path.read_text().splitlines()
    violations = []
    if len(lines) > MAX_FILE_LINES:
        violations.append(f"{rel}: {len(lines)} lines > {MAX_FILE_LINES}")
    start = None
    for i, line in enumerate(lines):
        if FUNC_START.match(line):
            start = i
        elif line == "}" and start is not None:
            length = i - start + 1
            if length > MAX_FUNC_LINES:
                violations.append(
                    f"{rel}:{start + 1}: function is {length} lines > {MAX_FUNC_LINES}"
                )
            start = None
    return violations


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    violations = []
    for path in sorted(root.rglob("*.go")):
        if ".git" in path.parts:
            continue
        violations.extend(check_file(path, path.relative_to(root)))
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
