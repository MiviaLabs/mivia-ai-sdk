#!/usr/bin/env python3
"""Gate: reject Go files and function names containing process-artifact
keywords. File basenames and function declarations must not contain:
phase, tdd, perf, wip, draft, scratch, tmp, old, backup, or a version
suffix like _v2, _v3 (versioning belongs in git, not in file names).
Exits non-zero on violations."""
import re
import subprocess
import sys
from pathlib import Path

BAD_BASENAME = re.compile(
    r"(?i)(?:phase|tdd|perf|wip|draft|scratch|tmp|old|backup)"
    r"|_v\d+\."
)
BAD_WORDS = {"phase", "tdd", "perf", "wip", "draft", "scratch", "tmp", "old", "backup"}
VERSION_SUFFIX = re.compile(r"_v\d+", re.IGNORECASE)
FUNC_DECL = re.compile(r"\bfunc\s+(?:\([^)]*\)\s+)?(\w+)\s*\(")
CAMEL_WORD = re.compile(r"[A-Z]+(?![a-z])|[A-Z][a-z]*|[a-z]+|[0-9]+")


def _has_bad_word(name: str) -> bool:
    """True if name contains a banned keyword as its own camelCase word,
    not merely as a substring (so Hold does not trip on old)."""
    if VERSION_SUFFIX.search(name):
        return True
    words = (w.lower() for w in CAMEL_WORD.findall(name))
    return any(w in BAD_WORDS for w in words)


def check_file(path: Path, rel: Path) -> list[str]:
    violations = []
    if BAD_BASENAME.search(path.name):
        violations.append(f"{rel}: filename contains a prohibited keyword")
    for n, line in enumerate(path.read_text().splitlines(), 1):
        for m in FUNC_DECL.finditer(line):
            if _has_bad_word(m.group(1)):
                violations.append(
                    f"{rel}:{n}: function name contains a prohibited keyword"
                )
                break
    return violations


def _go_files(root: Path) -> list[Path]:
    """Returns this worktree's own .go files: tracked plus untracked
    files git would add, via `git ls-files`, so a nested worktree (a
    sibling git checkout that happens to live under this tree, e.g.
    .claude/worktrees/) never leaks into the scan. `git ls-files`
    already resolves relative to the current worktree's own root, and
    --exclude-standard applies .gitignore and .git/info/exclude, which
    is where a nested worktree's directory is excluded. Falls back to
    a plain filesystem walk when git is unavailable."""
    try:
        out = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go"],
            cwd=root, capture_output=True, text=True, check=True,
        ).stdout
        return sorted(root / line for line in out.splitlines() if line)
    except (subprocess.CalledProcessError, FileNotFoundError):
        return sorted(
            p for p in root.rglob("*.go")
            if ".git" not in p.parts and "semgrep" not in p.parts
        )


def main() -> int:
    root = Path(__file__).resolve().parent.parent
    violations = []
    for path in _go_files(root):
        if not path.is_file():
            continue
        violations.extend(check_file(path, path.relative_to(root)))
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
