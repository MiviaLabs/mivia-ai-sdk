#!/usr/bin/env python3
"""Gate: every exported top-level Go symbol needs a doc comment that
starts with the symbol name. Keeps the package surface self-describing
for AI and human consumers. Exits non-zero on violations."""
import re
import sys
from pathlib import Path

DECL = re.compile(r"^(?:func\s+(?:\([^)]*\)\s+)?|type\s+)([A-Z]\w*)")


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    violations = []
    for path in sorted(root.rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        lines = path.read_text().splitlines()
        for i, line in enumerate(lines):
            m = DECL.match(line)
            if not m:
                continue
            name = m.group(1)
            # Collect the contiguous comment block above the decl; the Go
            # convention requires its first line to start with the name.
            j = i - 1
            while j >= 0 and lines[j].strip().startswith("//"):
                j -= 1
            first = lines[j + 1].strip() if j + 1 < i else ""
            if not re.match(rf"^//\s*{re.escape(name)}\b", first):
                violations.append(f"{path.relative_to(root)}:{i + 1}: {name} lacks doc comment")
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
