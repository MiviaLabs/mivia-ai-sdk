#!/usr/bin/env python3
"""Gate: diff-only test-tampering detector, collapsed single file.
Flags a change that makes the test suite ask less instead of making
the code pass more. Four rules, chosen by waiver evidence over the
commit history:

- TT01: a removed Test/Benchmark/Fuzz/Example function whose body
  reappears nowhere in the diff under a conforming name.
- TT02: a new t.Skip/t.SkipNow/t.Skipf/testing.Short call with no
  matching removal in the same hunk.
- TT04: a net decrease in assertion sites, suppressed by a new test
  function or a helper-extraction signal.
- TT09: a conformance vector under a scoped testdata/vectors/
  directory deleted. The TT10 in-place-modify rule collapsed into
  TT09 review practice; a modified vector is a normal review case.

The former gate-infra rules TT03, TT05-TT08, and TT11-TT14 are gone:
TT06, TT07, TT08, TT10 never fired outside their own probes, TT05 and
TT03 fired rarely, and TT11-TT14 duplicate the deps, api, and
thirdparty gates, which already fail on policy drift. See
docs/plans/test-tampering.md.

An `Allow-Test-Change: TTxx <reason>` commit-message trailer waives
one finding at a time; no CLI flag or env var ever does. The reason
has no word minimum, but only filler words counts as none.

--probe runs a built-in self-test: every rule must fire on a
violating diff and stay silent on its clean counterpart."""
import argparse
import difflib
import hashlib
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path

COLLECTION_REGEX = re.compile(r"^(Test|Benchmark|Fuzz|Example)([A-Z0-9_]|$)")
_FUNC_DECL = re.compile(r"^func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(")
_SKIP_PATTERNS = ("t.Skip(", "t.SkipNow(", "t.Skipf(", "testing.Short()")
_ASSERT_PATTERNS = ("t.Error(", "t.Errorf(", "t.Fatal(", "t.Fatalf(")
_HELPER_SUFFIXES = ("Cases(", "Suite(", "RunAll(")
_ASSERT_CALL = re.compile(r"(^|[^A-Za-z0-9_])assert[A-Za-z0-9_]*\(")
_VECTOR_SCOPES = ("envelope/", "machine/", "a2a/", "mcp/")
_TRAILER_RE = re.compile(r"^Allow-Test-Change:\s*(TT\d{2})[,:]?\s*(.*)$", re.MULTILINE)
_DIGIT_RUN = re.compile(r"\d+")
_AGGREGATE_IDS = frozenset({"TT04"})

_BOILERPLATE = {"fix", "cleanup", "refactor", "wip", "misc", "temp", "n/a", "na", "ok", "done"}


@dataclass
class Hunk:
    """Hunk is one -U0 diff hunk: removed and added lines, and the
    1-based start line of the added block."""

    removed: list
    added: list
    new_start: int


@dataclass
class Diff:
    """Diff is one changed file: its paths, status letter, full old and
    new texts, and its -U0 hunks."""

    old_path: str
    new_path: str
    status: str
    old_text: str
    new_text: str
    hunks: list = field(default_factory=list)

    @property
    def path(self) -> str:
        """path is the diff's target-side path: the new path, or the
        old path for a deletion."""
        return self.new_path or self.old_path


@dataclass
class Finding:
    """Finding is one rule hit: an ID, a path, a line, a message."""

    id: str
    path: str
    line: int
    message: str


def _git(root: Path, *args: str) -> bytes:
    proc = subprocess.run(
        ["git", "-C", str(root), *args], capture_output=True
    )
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.decode(errors="replace").strip())
    return proc.stdout


def repo_root(cwd: Path):
    """repo_root walks up to the checkout; None outside a repo."""
    for candidate in [cwd, *cwd.parents]:
        if (candidate / ".git").exists():
            return candidate
    return None


def _has_staged_changes(root: Path) -> bool:
    return subprocess.run(["git", "-C", str(root), "diff", "--cached", "--quiet"]).returncode != 0


def _has_worktree_changes(root: Path) -> bool:
    return subprocess.run(["git", "-C", str(root), "diff", "--quiet"]).returncode != 0


def _merge_head_revs(root: Path) -> list:
    try:
        out = _git(root, "rev-parse", "--no-quotes", "MERGE_HEAD").decode()
    except RuntimeError:
        return []
    return [rev for rev in out.split() if rev]


def _has_unmerged_paths(root: Path) -> bool:
    return bool(_git(root, "ls-files", "-u"))


def _has_head_commit(root: Path) -> bool:
    try:
        _git(root, "rev-parse", "--verify", "-q", "HEAD")
        return True
    except RuntimeError:
        return False


def _head_parents(root: Path) -> list:
    return _git(root, "rev-parse", "HEAD", "--parents").decode().split()[1:]


def _tip_commit(root: Path, rev_range: str) -> str:
    return _git(root, "rev-list", "-1", rev_range).decode().strip()


def commit_message(root: Path, rev: str) -> str:
    try:
        return _git(root, "log", "-1", "--format=%B", rev).decode(errors="replace")
    except RuntimeError:
        return ""


def _normalize_range_args(root: Path, cmp_args: list) -> list:
    """_normalize_range_args resolves a single A...B or A..B range arg
    into the two-rev form [base, B]. An A...B three-dot range uses the
    merge base of A and B as the base rev, matching git diff's
    meaning. Resolving here keeps every later text read a plain
    rev:path git show, so an unresolvable rev fails loudly in name-
    status order rather than as silently empty file texts."""
    revs = [a for a in cmp_args if not a.startswith("-")]
    if len(revs) != 1 or ".." not in revs[0] or "--cached" in cmp_args:
        return cmp_args
    left, sep, right = revs[0].partition("...")
    if not sep:
        left, _, right = revs[0].partition("..")
    base = left or "HEAD"
    target = right or "HEAD"
    if sep:
        merge = _git(root, "merge-base", base, target).decode().strip()
        if merge:
            base = merge
    return [base, target]


def _diff_base_and_target(cmp_args: list) -> tuple:
    """_diff_base_and_target splits one git-diff arg list into (base,
    target) revs: target None means the worktree. `--cached` diffs the
    staged tree against HEAD or, with an extra rev, against that rev.
    Range args arrive already normalized to the two-rev form."""
    revs = [a for a in cmp_args if not a.startswith("-")]
    if "--cached" in cmp_args:
        base = revs[0] if revs else "HEAD"
        return base, None
    if len(revs) >= 2:
        return revs[0], revs[1]
    return revs[0] if revs else "HEAD", None


def _read_rev_text(root: Path, rev: str, path: str) -> str:
    try:
        return _git(root, "show", f"{rev}:{path}").decode(errors="replace")
    except RuntimeError:
        return ""


def _worktree_text(root: Path, path: str) -> str:
    p = root / path
    return p.read_text(errors="replace") if p.is_file() else ""


def build_diff(root: Path, cmp_args: list) -> list:
    """build_diff models one `git diff <cmp_args>` into Diff records:
    name-status for the file list and status letters, -U0 for hunks,
    `git show` for the base text, the worktree or rev for the target
    text."""
    out = _git(root, "diff", "--name-status", *cmp_args).decode()
    cmp_args = _normalize_range_args(root, cmp_args)
    base, target = _diff_base_and_target(cmp_args)
    diffs = []
    for line in out.splitlines():
        parts = line.split("\t")
        status = parts[0][0]
        old_path = parts[1]
        new_path = parts[2] if len(parts) > 2 else old_path
        old_text = _read_rev_text(root, base, old_path)
        if status == "D":
            new_text = ""
        elif target is None and "--cached" not in cmp_args:
            new_text = _worktree_text(root, new_path)
        elif target is None:
            new_text = _staged_text(root, new_path)
        else:
            new_text = _read_rev_text(root, target, new_path)
        hunks = _parse_hunks(_git(root, "diff", "-U0", *cmp_args, "--", new_path).decode())
        diffs.append(Diff(old_path, new_path, status, old_text, new_text, hunks))
    return diffs


def _staged_text(root: Path, path: str) -> str:
    try:
        return _git(root, "show", f":{path}").decode(errors="replace")
    except RuntimeError:
        return ""


def _parse_hunks(patch: str) -> list:
    """_parse_hunks turns a -U0 patch body into Hunk records."""
    hunks = []
    removed: list = []
    added: list = []
    new_start = 0
    for line in patch.splitlines():
        if line.startswith("@@"):
            if removed or added:
                hunks.append(Hunk(removed, added, new_start))
            m = re.search(r"\+(\d+)", line)
            new_start = int(m.group(1)) if m else 0
            removed, added = [], []
        elif line.startswith("-"):
            removed.append(line[1:])
        elif line.startswith("+"):
            added.append(line[1:])
    if removed or added:
        hunks.append(Hunk(removed, added, new_start))
    return hunks


def extract_functions(text: str) -> dict:
    """extract_functions maps top-level function names to their
    (1-based line, body) lists, closing at a bare `}` at column 0.
    A line inside a raw string never closes a function."""
    if not text:
        return {}
    lines = text.splitlines()
    funcs: dict = {}
    start = None
    name = None
    in_raw_string = False
    for i, line in enumerate(lines):
        was_in_raw_string = in_raw_string
        if line.count("`") % 2 == 1:
            in_raw_string = not in_raw_string
        if was_in_raw_string:
            continue
        m = _FUNC_DECL.match(line)
        if m:
            start, name = i, m.group(1)
        elif line == "}" and start is not None:
            funcs.setdefault(name, []).append((i + 1, "\n".join(lines[start : i + 1])))
            start = None
    return funcs


def _body_hash(body: str) -> str:
    return hashlib.sha256(re.sub(r"\s+", " ", body).strip().encode()).hexdigest()


def check_moved_or_dropped(diffs: list) -> list:
    """TT01: a removed conforming test function with no matching
    body elsewhere in the diff's added functions."""
    findings = []
    test_diffs = [d for d in diffs if d.path.endswith("_test.go")]
    added_index: dict = {}
    for d in test_diffs:
        for _name, entries in extract_functions(d.new_text).items():
            for _line, body in entries:
                added_index.setdefault(_body_hash(body), []).append(_name)
    for d in test_diffs:
        old_funcs = extract_functions(d.old_text)
        new_funcs = extract_functions(d.new_text)
        for name, entries in old_funcs.items():
            if not COLLECTION_REGEX.match(name) or name in new_funcs:
                continue
            for line, body in entries:
                if any(COLLECTION_REGEX.match(c) for c in added_index.get(_body_hash(body), [])):
                    continue
                findings.append(Finding("TT01", d.path, line, f"test function {name} removed with no matching move"))
    return findings


def check_skip_added(diffs: list) -> list:
    """TT02: a new skip call or short-mode guard with no matching
    removal in the same hunk."""
    findings = []
    for d in diffs:
        if not d.path.endswith("_test.go"):
            continue
        for hunk in d.hunks:
            removed_join = "\n".join(hunk.removed)
            for line in hunk.added:
                for pat in _SKIP_PATTERNS:
                    if pat in line and pat not in removed_join:
                        findings.append(Finding("TT02", d.path, hunk.new_start, f"skip added: {pat}"))
                        break
    return findings


def check_assertion_decrease(diffs: list) -> list:
    """TT04: net assertion-site decrease across the whole diff,
    suppressed by a new test function or a helper-extraction signal."""
    removed = added = 0
    new_test_func = False
    helper_signal = False
    first_path = None
    for d in diffs:
        if not d.path.endswith("_test.go"):
            continue
        first_path = first_path or d.path
        old_funcs = extract_functions(d.old_text)
        new_funcs = extract_functions(d.new_text)
        for name in new_funcs:
            if COLLECTION_REGEX.match(name) and name not in old_funcs:
                new_test_func = True
        for hunk in d.hunks:
            for line in hunk.removed:
                removed += sum(line.count(p) for p in _ASSERT_PATTERNS)
            for line in hunk.added:
                added += sum(line.count(p) for p in _ASSERT_PATTERNS)
                if any(suf in line for suf in _HELPER_SUFFIXES) or _ASSERT_CALL.search(line):
                    helper_signal = True
    if removed > added and not new_test_func and not helper_signal:
        return [Finding("TT04", first_path or "", 0, f"assertion sites decreased net ({removed} removed, {added} added)")]
    return []


def check_vector_deleted(diffs: list) -> list:
    """TT09: a conformance vector under a scoped testdata/vectors/
    directory deleted."""
    hits = []
    for d in diffs:
        path = d.old_path
        if d.status == "D" and "testdata/vectors/" in path and any(path.startswith(s) for s in _VECTOR_SCOPES):
            hits.append(Finding("TT09", path, 1, "conformance vector deleted"))
    return hits


RULES = (check_moved_or_dropped, check_skip_added, check_assertion_decrease, check_vector_deleted)


def finding_key(f: Finding):
    """finding_key is a finding's identity across parents: the ID
    alone for aggregate rules, else ID, path, and message with digit
    runs collapsed."""
    if f.id in _AGGREGATE_IDS:
        return (f.id,)
    return (f.id, f.path, _DIGIT_RUN.sub("#", f.message))


def run_rules(diffs: list) -> list:
    findings = []
    for rule in RULES:
        findings.extend(rule(diffs))
    return findings


def resolve_overrides(findings: list, message: str) -> tuple:
    """resolve_overrides splits findings into (unresolved, overridden).
    One Allow-Test-Change trailer waives exactly one finding of its
    ID: the second finding of the same ID needs a second trailer. The
    trailer's reason has no word minimum; a reason of only boilerplate
    filler words waives nothing."""
    waivers: dict = {}
    for kind, m in _TRAILER_RE.findall(message or ""):
        reason_words = [w for w in re.split(r"\s+", m.strip()) if w]
        significant = [w for w in reason_words if w.strip(".,;:!?").lower() not in _BOILERPLATE]
        if any(ch.isalpha() for ch in m) and significant:
            waivers.setdefault(kind, []).append(m.strip())
    unresolved, overridden = [], []
    for f in findings:
        if waivers.get(f.id):
            waivers[f.id].pop()
            overridden.append(f)
        else:
            unresolved.append(f)
    return unresolved, overridden


def resolve_diff_source(range_arg: str, message_file: str, root: Path):
    """resolve_diff_source picks the comparisons and message source:
    --range, the staged tree (with merge parents), the merge
    stand-down, the working tree, the first staged commit, a merge
    commit, then the HEAD~1 HEAD fallback."""
    if range_arg:
        return [[range_arg]], commit_message(root, _tip_commit(root, range_arg)), None
    if message_file and _has_staged_changes(root):
        comparisons = [["--cached"]] + [["--cached", rev] for rev in _merge_head_revs(root)]
        return comparisons, Path(message_file).read_text(), None
    if _merge_head_revs(root) or _has_unmerged_paths(root):
        return None, None, "merge in progress; skipping"
    if _has_head_commit(root) and _has_worktree_changes(root):
        return [["HEAD"]], None, None
    if not _has_head_commit(root):
        if _has_staged_changes(root):
            return [["--cached"]], None, None
        return None, None, "no parent commit; skipping"
    parents = _head_parents(root)
    if not parents:
        return None, None, "no parent commit; skipping"
    message = commit_message(root, "HEAD")
    if len(parents) > 1:
        return [[parent, "HEAD"] for parent in parents], message, None
    return [["HEAD~1", "HEAD"]], message, None


def introduced_findings(root: Path, comparisons: list) -> list:
    """introduced_findings intersects rule hits across merge parents:
    only content every parent diff reports counts as introduced."""
    first = run_rules(build_diff(root, comparisons[0]))
    if len(comparisons) == 1:
        return first
    shared = None
    for comparison in comparisons[1:]:
        keys = {finding_key(f) for f in run_rules(build_diff(root, comparison))}
        shared = keys if shared is None else shared & keys
    return [f for f in first if finding_key(f) in shared]


# --- probe -------------------------------------------------------------


def _mkdiff(path: str, old: str, new: str, status: str = "M") -> Diff:
    return Diff(path, path, status, old, new, _parse_hunks(_unified0(path, old, new)))


def _unified0(path: str, old: str, new: str) -> str:
    return "\n".join(
        difflib.unified_diff(old.splitlines(), new.splitlines(), lineterm="", n=0)
    )


def run_probe() -> bool:
    """run_probe fires every rule on a violation and requires silence
    on the clean counterpart."""
    problems = []

    moved_old = "package p\n\nfunc TestA(t *testing.T) {\n\t_ = 1\n}\n"
    moved_dropped = "package p\n\nfunc TestB(t *testing.T) {\n\t_ = 2\n}\n"
    if not check_moved_or_dropped([_mkdiff("a_test.go", moved_old, moved_dropped)]):
        problems.append("TT01 did not fire on a dropped test")
    moved_to_b = Diff("a_test.go", "a_test.go", "D", moved_old, moved_old.replace("package p\n", ""), [])
    moved_from_b = Diff("b_test.go", "b_test.go", "A", "", moved_old, [])
    moved_to_b.status, moved_from_b.status = "D", "A"
    if check_moved_or_dropped([moved_to_b, moved_from_b]):
        problems.append("TT01 fired on a true move")

    skip_old = "package p\n\nfunc TestA(t *testing.T) {\n\t_ = 1\n}\n"
    skip_new = "package p\n\nfunc TestA(t *testing.T) {\n\tt.Skip(\"later\")\n\t_ = 1\n}\n"
    if not check_skip_added([_mkdiff("a_test.go", skip_old, skip_new)]):
        problems.append("TT02 did not fire on a new skip")
    if check_skip_added([_mkdiff("a_test.go", skip_old, skip_old)]):
        problems.append("TT02 fired on an unchanged file")

    weak_old = "package p\n\nfunc TestA(t *testing.T) {\n\tt.Fatal(\"x\")\n\tt.Fatal(\"y\")\n}\n"
    weak_new = "package p\n\nfunc TestA(t *testing.T) {\n\t_ = 1\n}\n"
    if not check_assertion_decrease([_mkdiff("a_test.go", weak_old, weak_new)]):
        problems.append("TT04 did not fire on an assertion decrease")
    strong_new = "package p\n\nfunc TestA(t *testing.T) {\n\tt.Fatal(\"x\")\n\tt.Fatal(\"y\")\n\tt.Fatal(\"z\")\n}\n"
    if check_assertion_decrease([_mkdiff("a_test.go", weak_old, strong_new)]):
        problems.append("TT04 fired on an assertion increase")

    vec = Diff("envelope/testdata/vectors/valid_x.json", "envelope/testdata/vectors/valid_x.json", "D", "{}", "", [])
    if not check_vector_deleted([vec]):
        problems.append("TT09 did not fire on a deleted vector")
    kept = Diff("envelope/testdata/vectors/valid_x.json", "envelope/testdata/vectors/valid_x.json", "M", "{}", "{}", [])
    if check_vector_deleted([kept]):
        problems.append("TT09 fired on a kept vector")

    findings = [Finding("TT01", "a_test.go", 1, "m1"), Finding("TT01", "a_test.go", 2, "m2"), Finding("TT04", "a_test.go", 0, "m")]
    msg = "subject\n\nAllow-Test-Change: TT01 feature removed with its tests.\n"
    unresolved, overridden = resolve_overrides(findings, msg)
    if len(overridden) != 1 or [f.id for f in unresolved] != ["TT01", "TT04"]:
        problems.append("one trailer must waive exactly one finding")
    _junk, overridden2 = resolve_overrides(findings, "Allow-Test-Change: TT01 cleanup\n")
    if overridden2:
        problems.append("boilerplate-only reason waived a finding")

    if problems:
        print("\n".join(problems))
        return False
    print("check_test_tampering: probe ok")
    return True


def main() -> int:
    parser = argparse.ArgumentParser(description="diff-only test-tampering gate")
    parser.add_argument("--range", dest="range_arg", help="diff this range instead of the staged tree")
    parser.add_argument("--message-file", dest="message_file", help="read override trailers from this file")
    parser.add_argument("--probe", action="store_true", help="run the built-in self-test and exit")
    args = parser.parse_args()

    if args.probe:
        return 0 if run_probe() else 1
    if args.range_arg and args.message_file:
        print("--range and --message-file cannot be used together")
        return 2

    root = repo_root(Path.cwd())
    if root is None:
        print("check_test_tampering: no .git found here; skipping")
        return 0

    try:
        comparisons, message, skip = resolve_diff_source(args.range_arg, args.message_file, root)
        if skip:
            print(f"check_test_tampering: {skip}")
            return 0
        findings = introduced_findings(root, comparisons)
    except RuntimeError as exc:
        print(str(exc))
        return 2

    unresolved, overridden = resolve_overrides(findings, message)
    for f in overridden:
        print(f"{f.id} overridden by commit trailer")
    for f in unresolved:
        print(f"{f.path}:{f.line}: {f.id} {f.message}")
    return 1 if unresolved else 0


if __name__ == "__main__":
    sys.exit(main())
