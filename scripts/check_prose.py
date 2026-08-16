#!/usr/bin/env python3
"""Gate: plan prose follows the writing standard (AGENTS.md). Checks
sentence length in docs/plans/*.md: one idea per sentence, at most 25
words. Code fences, headings, and list lines are exempt."""
import re
import sys
from pathlib import Path

MAX_WORDS = 25
SENTENCE = re.compile(r"[^.!?]+[.!?]")


def check_file(path: Path, rel: Path) -> list[str]:
    violations = []
    in_fence = False
    for n, line in enumerate(path.read_text().splitlines(), 1):
        stripped = line.strip()
        if stripped.startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence or stripped.startswith(("#", "-", "|")):
            continue
        for m in SENTENCE.finditer(stripped):
            words = len(m.group().split())
            if words > MAX_WORDS:
                violations.append(f"{rel}:{n}: sentence has {words} words > {MAX_WORDS}")
    return violations


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    violations = []
    for path in sorted((root / "docs" / "plans").glob("*.md")):
        violations.extend(check_file(path, path.relative_to(root)))
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
