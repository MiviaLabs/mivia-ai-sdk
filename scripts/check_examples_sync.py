#!/usr/bin/env python3
"""Gate: a Markdown example's fenced Go program stays byte-identical
to its committed, runnable counterpart under docs/examples/. Each
tuple below names the Markdown file, the header the fence follows, and
the committed .go file the fence must match exactly. Exits non-zero on
a missing header, a missing file, or a byte-level mismatch."""
import difflib
import sys
from pathlib import Path

PAIRS = [
    (
        "docs/examples/agent-composition.md",
        "## The program",
        "docs/examples/_agentcomposition/main.go",
    ),
    (
        "docs/examples/agent-composition.md",
        "## SQLiteStore variant",
        "docs/examples/_agentcompositionsqlite/main.go",
    ),
    (
        "docs/examples/agentrun.md",
        "## The program",
        "docs/examples/_agentrun/main.go",
    ),
]

FENCE_OPEN = "```go"
FENCE_CLOSE = "```"


def extract_fence(md_path: Path, header: str) -> str | None:
    """Returns the fenced Go block following header, or None when the
    header, the opening fence, or the closing fence is missing."""
    lines = md_path.read_text().splitlines()

    header_idx = None
    for i, line in enumerate(lines):
        if line.strip() == header:
            header_idx = i
            break
    if header_idx is None:
        return None

    open_idx = None
    for i in range(header_idx, len(lines)):
        if lines[i] == FENCE_OPEN:
            open_idx = i
            break
    if open_idx is None:
        return None

    close_idx = None
    for i in range(open_idx + 1, len(lines)):
        if lines[i] == FENCE_CLOSE:
            close_idx = i
            break
    if close_idx is None:
        return None

    return "\n".join(lines[open_idx + 1:close_idx])


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    problems = []

    for md_rel, header, go_rel in PAIRS:
        md_path = root / md_rel
        go_path = root / go_rel

        fence = extract_fence(md_path, header)
        if fence is None:
            problems.append(
                f"{md_rel}: no fenced go block found after header {header!r}"
            )
            continue

        if not go_path.exists():
            problems.append(f"{go_rel}: file does not exist")
            continue

        target = go_path.read_text()
        if fence + "\n" != target:
            diff = "\n".join(
                difflib.unified_diff(
                    target.splitlines(),
                    fence.splitlines(),
                    fromfile=go_rel,
                    tofile=f"{md_rel} ({header})",
                    lineterm="",
                )
            )
            problems.append(
                f"{md_rel} ({header}) and {go_rel} differ:\n{diff}"
            )

    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
