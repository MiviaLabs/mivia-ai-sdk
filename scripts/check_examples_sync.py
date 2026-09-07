#!/usr/bin/env python3
"""Gate: a Markdown example's fenced Go program stays byte-identical
to its committed, runnable counterpart under docs/examples/. Each
tuple below names the Markdown file, the header the fence follows, and
the committed .go file the fence must match exactly. Exits non-zero on
a missing header, a missing file, or a byte-level mismatch."""
import argparse
import difflib
import sys
import tempfile
from pathlib import Path

PAIRS = [
    (
        "docs/examples/workflow-composition.md",
        "## The program",
        "docs/examples/_workflowcomposition/main.go",
    ),
    (
        "docs/examples/workflow-composition.md",
        "## SQLiteStore variant",
        "docs/examples/_workflowcompositionsqlite/main.go",
    ),
    (
        "docs/examples/workflow-run.md",
        "## The program",
        "docs/examples/_workflowrun/main.go",
    ),
    (
        "docs/examples/agentloop.md",
        "## The program",
        "docs/examples/_agentloop/main.go",
    ),
    (
        "README.md",
        "## Quick Start",
        "docs/examples/_quickstart/main.go",
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


def check(root: Path, pairs: list[tuple[str, str, str]]) -> list[str]:
    """check runs the fence-vs-file comparison for every pair rooted
    at root. Returns a problem string per header, missing file, or
    mismatch found."""
    problems = []

    for md_rel, header, go_rel in pairs:
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

    return problems


def run_probe() -> bool:
    """run_probe plants a Markdown fence that drifts from its paired
    .go file in a temp fixture, and asserts check reports it."""
    problems: list[str] = []
    with tempfile.TemporaryDirectory(prefix="examples-sync-probe-") as tmp:
        root = Path(tmp)
        md_path = root / "probe.md"
        go_dir = root / "docs/examples/_probe"
        go_dir.mkdir(parents=True)
        go_path = go_dir / "main.go"

        go_path.write_text("package main\n\nfunc main() {}\n")
        md_path.write_text(
            "## The program\n\n```go\npackage main\n\nfunc main() { drift() }\n```\n"
        )

        pair = [("probe.md", "## The program", "docs/examples/_probe/main.go")]
        found = check(root, pair)
        if not found:
            problems.append("probe_drift_detected: expected a mismatch, got none")

        go_path.write_text("package main\n\nfunc main() { drift() }\n")
        found = check(root, pair)
        if found:
            problems.append(f"probe_match_passes: expected no mismatch, got {found}")

    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="examples-sync gate")
    parser.add_argument("--probe", action="store_true", help="run the gate's own probe suite")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    root = Path(__file__).resolve().parent.parent
    problems = check(root, PAIRS)

    if problems:
        print("\n".join(problems))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
