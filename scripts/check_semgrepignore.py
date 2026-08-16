#!/usr/bin/env python3
"""Gate: .semgrepignore content is pinned to exactly one line. The file
lists what the Semgrep scan must exclude. The repo excludes only .git/
so test files are scanned again. Edit the gate deliberately; never the
file on a whim."""
import sys
from pathlib import Path

EXPECTED = ".git/\n"


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    path = root / ".semgrepignore"
    if not path.exists():
        print(f"{path.relative_to(root)}: missing; expected exactly {EXPECTED!r}")
        return 1
    actual = path.read_text()
    if actual != EXPECTED:
        print(f"{path.relative_to(root)}: content differs from the pinned form:")
        print("expected: " + repr(EXPECTED))
        print("actual:   " + repr(actual))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
