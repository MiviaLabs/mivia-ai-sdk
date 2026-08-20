#!/usr/bin/env python3
"""Git plumbing for scripts/check_test_tampering.py.
Resolves the repository root, the staged/parent/range diff sources,
and builds the per-file diff model the fourteen TT01-TT14 rules read:
full old/new blob text plus per-hunk removed/added line lists. Also
holds the small synthetic-repo helpers the --probe suite (see
scripts/test_tampering_probes*.py) uses to build throwaway git repos,
since no fixture in this gate lives as a checked-in file."""
import re
import subprocess
from dataclasses import dataclass, field
from pathlib import Path


class DiffError(Exception):
    """DiffError reports a git plumbing failure; the caller exits 2."""


@dataclass
class Hunk:
    """Hunk is one unified-diff hunk: its header line numbers plus the
    removed and added line text, in order, with the +/- prefix
    stripped."""

    old_start: int
    new_start: int
    removed: list = field(default_factory=list)
    added: list = field(default_factory=list)


@dataclass
class FileDiff:
    """FileDiff is one file's change: its git status, full old and new
    blob text (None when the file did not exist on that side), and its
    hunks for same-hunk rules."""

    old_path: str
    new_path: str
    status: str
    old_text: str
    new_text: str
    hunks: list

    @property
    def path(self) -> str:
        """path is the reporting path: the new path, or the old path
        for a deleted file."""
        return self.new_path or self.old_path


def repo_root(start: Path) -> Path:
    """repo_root returns the working tree root above start, or None
    when no .git exists there (the pre-commit sandbox has none)."""
    result = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"], cwd=start, capture_output=True, text=True
    )
    if result.returncode != 0:
        return None
    return Path(result.stdout.strip())


def has_parent_commit(root: Path) -> bool:
    """has_parent_commit reports whether HEAD~1 resolves: false in the
    repository's first commit."""
    result = subprocess.run(
        ["git", "rev-parse", "--verify", "-q", "HEAD~1"], cwd=root, capture_output=True, text=True
    )
    return result.returncode == 0


def has_staged_changes(root: Path) -> bool:
    """has_staged_changes reports whether the index differs from HEAD."""
    result = subprocess.run(["git", "diff", "--cached", "--quiet"], cwd=root)
    return result.returncode != 0


def tip_commit(range_arg: str) -> str:
    """tip_commit extracts the right-hand revision of a `..`/`...`
    range; a range with no dots is itself the tip."""
    for sep in ("...", ".."):
        if sep in range_arg:
            right = range_arg.split(sep)[-1]
            return right or "HEAD"
    return range_arg


def commit_message(root: Path, rev: str) -> str:
    """commit_message reads one commit's full message body."""
    return _run_git(["log", "-1", "--format=%B", rev], root).stdout


def _run_git(args: list, cwd: Path, check: bool = True):
    """_run_git runs one git subcommand; a non-zero exit raises
    DiffError when check is set, matching the CLI's usage-error exit."""
    result = subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True)
    if check and result.returncode != 0:
        raise DiffError((result.stderr or f"git {' '.join(args)} failed").strip())
    return result


def _show_blob(root: Path, sha: str) -> str:
    """_show_blob reads one blob's text, or None for the all-zero sha
    git diff --raw uses when a file has no content on that side."""
    if not sha or set(sha) == {"0"}:
        return None
    result = subprocess.run(
        ["git", "show", sha], cwd=root, capture_output=True, text=True, errors="replace"
    )
    if result.returncode != 0:
        return None
    return result.stdout


_RAW_LINE = re.compile(r"^:(\d+) (\d+) (\w+) (\w+) (\w)\d*\t(.+)$")


def _parse_raw(output: str) -> list:
    """_parse_raw turns `git diff --raw --full-index` output into
    (old_sha, new_sha, status, path) tuples."""
    entries = []
    for line in output.splitlines():
        m = _RAW_LINE.match(line)
        if not m:
            continue
        _old_mode, _new_mode, old_sha, new_sha, status, path = m.groups()
        entries.append((old_sha, new_sha, "M" if status == "T" else status, path))
    return entries


_HUNK_HEADER = re.compile(r"^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@")


def _parse_hunks(patch_text: str) -> list:
    """_parse_hunks splits one file's unified-diff body into Hunks."""
    hunks = []
    current = None
    for line in patch_text.splitlines():
        m = _HUNK_HEADER.match(line)
        if m:
            if current is not None:
                hunks.append(current)
            current = Hunk(old_start=int(m.group(1)), new_start=int(m.group(2)))
            continue
        if current is None:
            continue
        if line.startswith("-") and not line.startswith("---"):
            current.removed.append(line[1:])
        elif line.startswith("+") and not line.startswith("+++"):
            current.added.append(line[1:])
    if current is not None:
        hunks.append(current)
    return hunks


_DIFF_GIT_HEADER = re.compile(r"^diff --git a/(.+) b/(.+)$")


def _split_patch_by_file(patch_text: str) -> dict:
    """_split_patch_by_file groups a multi-file unified diff's body
    lines under each file's new (or old, for a delete) path."""
    per_file: dict = {}
    path = None
    buf: list = []
    for line in patch_text.splitlines(keepends=True):
        if line.startswith("diff --git "):
            if path is not None:
                per_file[path] = "".join(buf)
            m = _DIFF_GIT_HEADER.match(line.strip())
            path = m.group(2) if m else None
            buf = []
        else:
            buf.append(line)
    if path is not None:
        per_file[path] = "".join(buf)
    return per_file


def build_diff(root: Path, diff_args: list) -> list:
    """build_diff resolves one git diff comparison (a range, `--cached`,
    or two revisions) into the FileDiff model every TT rule reads.
    `--no-renames` forces every rename into a plain delete-plus-add
    pair: a rule that gates on `status == "D"` must see a deletion,
    not a single `R100` raw line naming two paths on one line."""
    raw = _run_git(["diff", "--no-renames", "--raw", "--full-index", *diff_args, "--"], root).stdout
    entries = _parse_raw(raw)
    patch = _run_git(["diff", "--no-renames", "--unified=3", *diff_args, "--"], root).stdout
    per_file_patch = _split_patch_by_file(patch)

    diffs = []
    for old_sha, new_sha, status, raw_path in entries:
        new_path = raw_path if status != "D" else None
        old_path = raw_path if status != "A" else None
        old_text = _show_blob(root, old_sha)
        new_text = _show_blob(root, new_sha)
        hunks = _parse_hunks(per_file_patch.get(raw_path, ""))
        diffs.append(FileDiff(old_path, new_path, status, old_text, new_text, hunks))
    return diffs


# --- probe-repo helpers ----------------------------------------------
# Shared by scripts/test_tampering_probes*.py: build a throwaway git
# repository per probe case, never a checked-in fixture.


def init_probe_repo(repo: Path) -> None:
    """init_probe_repo makes repo a fresh, locally-configured git repo.
    The identity and signing config are local to this temp repo only;
    they never touch the real project's git config."""
    subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
    subprocess.run(["git", "config", "user.email", "probe@example.com"], cwd=repo, check=True)
    subprocess.run(["git", "config", "user.name", "probe"], cwd=repo, check=True)
    subprocess.run(["git", "config", "commit.gpgsign", "false"], cwd=repo, check=True)


def commit_all(repo: Path, message: str = "commit") -> None:
    """commit_all stages the whole tree and commits it."""
    subprocess.run(["git", "add", "-A"], cwd=repo, check=True)
    subprocess.run(["git", "commit", "-q", "-m", message], cwd=repo, check=True)


def stage_all(repo: Path) -> None:
    """stage_all stages the whole tree without committing."""
    subprocess.run(["git", "add", "-A"], cwd=repo, check=True)


def diff_after_change(repo: Path, mutate_fn, message: str = "change") -> list:
    """diff_after_change runs mutate_fn on repo's tree, commits the
    result, and returns the FileDiff list against the parent commit.
    The common shape for a --probe violation-then-clean pair."""
    mutate_fn(repo)
    commit_all(repo, message)
    return build_diff(repo, ["HEAD~1", "HEAD"])
