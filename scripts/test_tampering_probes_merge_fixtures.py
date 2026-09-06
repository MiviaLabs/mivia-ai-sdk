#!/usr/bin/env python3
"""Throwaway-repo fixtures for test_tampering_probes_merge.py: the file
bodies every merge case starts from, plus the git helpers that build a
two-parent or three-parent history and a merge commit with an arbitrary
tree. No fixture lives as a checked-in file."""
import re
import subprocess
import sys
from pathlib import Path

from test_tampering_diff import commit_all, init_probe_repo

BASE_TESTS = {
    "a_test.go": (
        'package a\n\nimport "testing"\n\n'
        "func TestKeepMe(t *testing.T) {\n"
        '\tt.Fatal("keep one")\n'
        '\tt.Fatal("keep two")\n'
        "}\n\n"
        "func TestDropMe(t *testing.T) {\n"
        '\tt.Fatal("drop one")\n'
        '\tt.Fatal("drop two")\n'
        "}\n"
    ),
    "b_test.go": (
        'package a\n\nimport "testing"\n\n'
        "func TestOther(t *testing.T) {\n"
        '\tt.Fatal("other one")\n'
        "}\n"
    ),
}

ASSERT_FILES = {
    "a_test.go": (
        'package a\n\nimport "testing"\n\n'
        "func TestA(t *testing.T) {\n"
        '\tt.Fatal("a1")\n'
        '\tt.Fatal("a2")\n'
        '\tt.Fatal("a3")\n'
        "}\n"
    ),
    "z_test.go": (
        'package a\n\nimport "testing"\n\n'
        "func TestZ(t *testing.T) {\n"
        '\tt.Fatal("z1")\n'
        '\tt.Fatal("z2")\n'
        '\tt.Fatal("z3")\n'
        "}\n"
    ),
}

INFRA_FILES = {
    "scripts/a_check.py": "print('a1')\n",
    "scripts/z_check.py": "print('z1')\n",
    "foo/a.go": "package foo\n\nvar A = 1\n",
    "foo/z.go": "package foo\n\nvar Z = 1\n",
}

TT13_FILES = {
    "Makefile": "COVERAGE_FLOOR := 85\n\nall:\n\techo build\n",
    "scripts/mutation_denylist/a.json": '{"floor": 50, "denylist": []}\n',
}


def checker_path() -> Path:
    """checker_path returns the real gate script's absolute path."""
    return Path(__file__).resolve().parent / "check_test_tampering.py"


def git(repo: Path, args: list) -> str:
    """git runs one git subcommand in repo and returns its stdout."""
    result = subprocess.run(["git", *args], cwd=repo, capture_output=True, text=True, check=True)
    return result.stdout


def run_checker(repo: Path):
    """run_checker runs the gate as a subprocess inside repo."""
    return subprocess.run([sys.executable, str(checker_path())], cwd=repo, capture_output=True, text=True)


def new_repo(tmp: Path, name: str) -> Path:
    """new_repo makes a fresh throwaway repository under tmp."""
    repo = tmp / name
    repo.mkdir()
    init_probe_repo(repo)
    return repo


def drop_func(text: str, name: str) -> str:
    """drop_func removes one top-level Go function from a source text,
    together with the blank line that follows it."""
    pattern = re.compile(rf"func {re.escape(name)}\(.*?\n}}\n\n?", re.DOTALL)
    return pattern.sub("", text)


def write_files(repo: Path, files: dict) -> None:
    """write_files writes each name-to-text pair under repo."""
    for name, text in files.items():
        path = repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)


def set_tree(repo: Path, files: dict) -> None:
    """set_tree replaces the working tree with exactly these files."""
    for path in sorted(repo.rglob("*"), reverse=True):
        if ".git" in path.parts:
            continue
        if path.is_file():
            path.unlink()
    write_files(repo, files)


def two_parent_repo(tmp, name, base_files, branch_files, branch_message, trunk_files, trunk_message):
    """two_parent_repo builds a base commit, one `feature` commit, and
    one trunk commit. It returns the repo and the trunk branch name,
    with HEAD back on the trunk."""
    repo = new_repo(tmp, name)
    write_files(repo, base_files)
    commit_all(repo, "base")
    trunk = git(repo, ["rev-parse", "--abbrev-ref", "HEAD"]).strip()
    git(repo, ["checkout", "-q", "-b", "feature"])
    write_files(repo, branch_files)
    commit_all(repo, branch_message)
    git(repo, ["checkout", "-q", trunk])
    write_files(repo, trunk_files)
    commit_all(repo, trunk_message)
    return repo, trunk


def make_merge(repo: Path, files: dict, message: str, extra_parents: list = None) -> None:
    """make_merge commits an arbitrary tree as a merge of HEAD and the
    given parents, then moves the current branch onto it. git
    commit-tree needs no interactive merge resolution, so an octopus
    merge is as easy to build as a two-parent one."""
    parents = [git(repo, ["rev-parse", "HEAD"]).strip()]
    parents += extra_parents if extra_parents is not None else [git(repo, ["rev-parse", "feature"]).strip()]
    set_tree(repo, files)
    git(repo, ["add", "-A"])
    tree = git(repo, ["write-tree"]).strip()
    args = ["commit-tree", tree]
    for parent in parents:
        args += ["-p", parent]
    args += ["-m", message]
    sha = git(repo, args).strip()
    git(repo, ["reset", "--hard", "-q", sha])
