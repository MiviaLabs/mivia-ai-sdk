#!/usr/bin/env python3
"""Gate: audit-finding labels never appear in comments, docs, or plans.
A label is a whole word: a letter A through G followed by a digit. The
scan reads file bytes and never decodes text, so binary files cannot
crash it. It walks the physical tree and needs no Git repository. The
scan skips .git/ and semgrep/, mirroring the Makefile marker-scan
exclusions. A hit reports the file path and line; the script exits
one. A clean tree exits zero."""
import re
import sys
from pathlib import Path

LABEL = re.compile(rb"\b[A-G][0-9]\b")


def walk(root: Path):
    """Yield every file below root, sorted, skipping .git and semgrep."""
    for child in sorted(root.iterdir()):
        if child.is_dir():
            if child.name in (".git", "semgrep"):
                continue
            yield from walk(child)
        else:
            yield child


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    problems = []
    for path in walk(root):
        try:
            data = path.read_bytes()
        except OSError as exc:
            problems.append(f"{path}: unreadable: {exc}")
            continue
        for n, line in enumerate(data.splitlines(), 1):
            if LABEL.search(line):
                problems.append(f"{path}:{n}: audit-finding label")
    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
