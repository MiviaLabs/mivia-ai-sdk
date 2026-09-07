#!/usr/bin/env python3
"""Gate: reject Go files and function names containing process-artifact
keywords. File basenames and function declarations must not contain:
phase, tdd, perf, wip, draft, scratch, tmp, old, backup, or a version
suffix like _v2, _v3 (versioning belongs in git, not in file names).
Also gates error-string prefixes: an errors.New or fmt.Errorf literal
that starts with "word: " must name the enclosing package, not a
package that folded away. Also gates the private consumer
repository's name: no tracked path and no line of a tracked text file
may match REPO_NAME's pattern. Exits non-zero on violations."""
import argparse
import re
import subprocess
import sys
import tempfile
from pathlib import Path

BAD_BASENAME = re.compile(
    r"(?i)(?:phase|tdd|perf|wip|draft|scratch|tmp|old|backup)"
    r"|_v\d+\."
)
BAD_WORDS = {"phase", "tdd", "perf", "wip", "draft", "scratch", "tmp", "old", "backup"}
VERSION_SUFFIX = re.compile(r"_v\d+", re.IGNORECASE)
FUNC_DECL = re.compile(r"\bfunc\s+(?:\([^)]*\)\s+)?(\w+)\s*\(")
CAMEL_WORD = re.compile(r"[A-Z]+(?![a-z])|[A-Z][a-z]*|[a-z]+|[0-9]+")
PACKAGE_DECL = re.compile(r"^package\s+(\w+)")
ERROR_LITERAL = re.compile(r'(?:errors\.New|fmt\.Errorf)\(\s*"([A-Za-z0-9_]+):\s')

# REPO_NAME rejects the private consumer repository's name. The org
# prefix is assembled from two halves so this gate file never holds
# the pattern's own trigger text and never reports itself. This
# module's own name is the one allowed use, hence the lookahead.
_ORG_PREFIX = "mi" + "via"
REPO_NAME = re.compile(_ORG_PREFIX + r"-(?!ai-sdk)", re.IGNORECASE)
SKIP_TOP_DIRS = {"x", ".git"}


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
    lines = path.read_text().splitlines()
    for n, line in enumerate(lines, 1):
        for m in FUNC_DECL.finditer(line):
            if _has_bad_word(m.group(1)):
                violations.append(
                    f"{rel}:{n}: function name contains a prohibited keyword"
                )
                break
    if not any(part.startswith("_") for part in rel.parts):
        violations.extend(check_error_prefixes(lines, rel))
    return violations


def check_error_prefixes(lines: list[str], rel: Path) -> list[str]:
    """check_error_prefixes rejects an errors.New/fmt.Errorf literal
    whose leading "word: " does not name the enclosing package. This
    catches a stale prefix left behind when a package folds into
    another one. Test files are exempt: they may construct arbitrary
    strings on purpose. A directory starting with "_" is exempt too:
    the go tool itself ignores it (docs/examples/_agentloop and
    similar), so it carries no production error taxonomy to protect."""
    if rel.name.endswith("_test.go"):
        return []
    package = None
    for line in lines:
        m = PACKAGE_DECL.match(line)
        if m:
            package = m.group(1)
            break
    if package is None:
        return []
    violations = []
    for n, line in enumerate(lines, 1):
        m = ERROR_LITERAL.search(line)
        if m and m.group(1) != package:
            violations.append(
                f"{rel}:{n}: error string prefix {m.group(1)!r} does not "
                f"match enclosing package {package!r}"
            )
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


def _tracked_files(root: Path) -> list[Path]:
    """Returns every tracked or addable file of this worktree, using
    the same `git ls-files` call and the same filesystem-walk fallback
    as _go_files."""
    try:
        out = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard"],
            cwd=root, capture_output=True, text=True, check=True,
        ).stdout
        return sorted(root / line for line in out.splitlines() if line)
    except (subprocess.CalledProcessError, FileNotFoundError):
        return sorted(p for p in root.rglob("*") if p.is_file())


def check_repo_name(root: Path) -> list[str]:
    """check_repo_name rejects the private consumer repository's name
    in any tracked path or in any line of a tracked text file. See
    REPO_NAME for the pattern. The x/ sub-module and .git/ are out of
    scope, and a binary file is read for its path only."""
    violations = []
    for path in _tracked_files(root):
        rel = path.relative_to(root)
        if set(rel.parts) & SKIP_TOP_DIRS:
            continue
        if REPO_NAME.search(rel.as_posix()):
            violations.append(
                f"{rel}: path matches the banned pattern {REPO_NAME.pattern}"
            )
        if not path.is_file():
            continue
        data = path.read_bytes()
        if b"\0" in data:
            continue
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            continue
        for n, line in enumerate(text.splitlines(), 1):
            if REPO_NAME.search(line):
                violations.append(
                    f"{rel}:{n}: line matches the banned pattern "
                    f"{REPO_NAME.pattern}"
                )
    return violations


def run(root: Path) -> list[str]:
    violations = []
    for path in _go_files(root):
        if not path.is_file():
            continue
        violations.extend(check_file(path, path.relative_to(root)))
    violations.extend(check_repo_name(root))
    return violations


# --- probes ---------------------------------------------------------


def _probe_wrong_prefix_fails(tmp: Path) -> list[str]:
    pkg = tmp / "spool"
    pkg.mkdir()
    (pkg / "spool.go").write_text(
        'package spool\n\nimport "errors"\n\n'
        'var ErrNoPrincipal = errors.New("memory: no principal in context")\n'
    )
    problems = run(tmp)
    if not any("does not match enclosing package" in p for p in problems):
        return [f"probe_wrong_prefix_fails: expected a prefix mismatch, got {problems}"]
    return []


def _probe_right_prefix_passes(tmp: Path) -> list[str]:
    pkg = tmp / "spool"
    pkg.mkdir()
    (pkg / "spool.go").write_text(
        'package spool\n\nimport "errors"\n\n'
        'var ErrNoPrincipal = errors.New("spool: no principal in context")\n'
    )
    problems = run(tmp)
    if problems:
        return [f"probe_right_prefix_passes: expected pass, got {problems}"]
    return []


def _probe_test_file_exempt(tmp: Path) -> list[str]:
    pkg = tmp / "spool"
    pkg.mkdir()
    (pkg / "spool_test.go").write_text(
        'package spool\n\nimport "errors"\n\n'
        'var errBoom = errors.New("memory: boom")\n'
    )
    problems = run(tmp)
    if problems:
        return [f"probe_test_file_exempt: expected pass, got {problems}"]
    return []


def _probe_underscore_dir_exempt(tmp: Path) -> list[str]:
    pkg = tmp / "_agentloop"
    pkg.mkdir()
    (pkg / "main.go").write_text(
        'package main\n\nimport "errors"\n\n'
        'var errBoom = errors.New("cannedCompleter: boom")\n'
    )
    problems = run(tmp)
    if problems:
        return [f"probe_underscore_dir_exempt: expected pass, got {problems}"]
    return []


def _probe_repo_name_fails(tmp: Path) -> list[str]:
    pkg = tmp / "agentloop"
    pkg.mkdir()
    cited = _ORG_PREFIX + "-" + "agent/internal/provider/api_message.go"
    (pkg / "shape.go").write_text(
        "package agentloop\n\n// isEmptyAssistantTurn mirrors " + cited + ".\n"
    )
    problems = run(tmp)
    if not any("matches the banned pattern" in p for p in problems):
        return [f"probe_repo_name_fails: expected a banned-name hit, got {problems}"]
    return []


def _probe_repo_name_allows_this_module(tmp: Path) -> list[str]:
    pkg = tmp / "agentloop"
    pkg.mkdir()
    cited = _ORG_PREFIX + "-" + "ai-sdk/agentloop"
    (pkg / "shape.go").write_text("package agentloop\n\n// See " + cited + ".\n")
    problems = run(tmp)
    if problems:
        return [f"probe_repo_name_allows_this_module: expected pass, got {problems}"]
    return []


def _probe_real_tree_passes() -> list[str]:
    root = Path(__file__).resolve().parent.parent
    problems = run(root)
    if problems:
        return [f"probe_real_tree_passes: expected pass, got {problems}"]
    return []


def run_probe() -> bool:
    """run_probe exercises check_error_prefixes and check_repo_name
    against fixture files,
    following check_deps.py's --probe convention. Fixtures are plain
    directories (no go.mod, no git repo) since _go_files falls back to
    a filesystem walk when git ls-files finds no repository there."""
    problems = []
    for fn in (
        _probe_wrong_prefix_fails,
        _probe_right_prefix_passes,
        _probe_test_file_exempt,
        _probe_underscore_dir_exempt,
        _probe_repo_name_fails,
        _probe_repo_name_allows_this_module,
    ):
        with tempfile.TemporaryDirectory(prefix="names-probe-") as tmp:
            problems.extend(fn(Path(tmp)))
    problems.extend(_probe_real_tree_passes())
    if problems:
        print("\n".join(problems))
        return False
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="file/function/error-prefix naming gate")
    parser.add_argument("--probe", action="store_true", help="run the gate's own probe suite")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1

    root = Path(__file__).resolve().parent.parent
    violations = run(root)
    if violations:
        print("\n".join(violations))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
