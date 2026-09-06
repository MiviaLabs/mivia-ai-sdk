#!/usr/bin/env python3
"""Gate: diff-only test-tampering detector. Flags a change that makes
the test suite ask less instead of making the code pass more: a
skipped, dropped, or weakened test; a tampered conformance vector; or a
self-referential edit to the gates themselves. See
docs/plans/test-tampering.md. Fourteen stable finding IDs, TT01-TT14.
An Allow-Test-Change or Allow-Gate-Change commit-message trailer waives
one finding at a time; no CLI flag or env var ever does. Module layout
mirrors check_mutation.py plus mutation_tokenize.py: git plumbing in
test_tampering_diff.py, the rules in test_tampering_rules.py and
test_tampering_rules_infra.py, overrides in test_tampering_override.py,
--probe in test_tampering_probes.py."""
import argparse
import re
import sys
from pathlib import Path

from test_tampering_diff import DiffError, build_diff, commit_message, has_head_commit, has_staged_changes, \
    has_unmerged_paths, has_worktree_changes, head_parents, merge_head_revs, repo_root, tip_commit
from test_tampering_override import resolve_overrides
from test_tampering_rules import ALL_RULES as TEST_FILE_RULES
from test_tampering_rules_infra import ALL_RULES as INFRA_RULES

RULES = TEST_FILE_RULES + INFRA_RULES

AGGREGATE_IDS = frozenset({"TT04", "TT11"})
"""AGGREGATE_IDS names the rules that report once per diff, at a path
that depends on diff order. They key on the ID alone across parents.
TT13 is not a member; see docs/plans/test-tampering.md, "The
intersection key"."""

SKIP_NO_PARENT = "no parent commit; skipping"
SKIP_MERGE_IN_PROGRESS = "merge in progress; skipping"

_DIGIT_RUN = re.compile(r"\d+")


def resolve_diff_source(range_arg: str, message_file: str, root: Path):
    """resolve_diff_source picks the comparisons and the message source,
    following the plan's resolution order: --range, the staged tree, the
    merge stand-down, the working tree, the first staged commit, a merge
    commit, then the HEAD~1 HEAD fallback. Returns (comparisons,
    message, skip); comparisons holds one diff-arg list per parent for
    a merge and one otherwise, and skip holds the printed note when
    there is nothing to audit."""
    if range_arg:
        message = commit_message(root, tip_commit(range_arg))
        return [[range_arg]], message, None
    if message_file and has_staged_changes(root):
        comparisons = [["--cached"]] + [["--cached", rev] for rev in merge_head_revs(root)]
        return comparisons, Path(message_file).read_text(), None
    if merge_head_revs(root) or has_unmerged_paths(root):
        return None, None, SKIP_MERGE_IN_PROGRESS
    if has_head_commit(root) and has_worktree_changes(root):
        return [["HEAD"]], None, None
    if not has_head_commit(root):
        if has_staged_changes(root):
            return [["--cached"]], None, None
        return None, None, SKIP_NO_PARENT
    parents = head_parents(root)
    if not parents:
        return None, None, SKIP_NO_PARENT
    message = commit_message(root, "HEAD")
    if len(parents) > 1:
        return [[parent, "HEAD"] for parent in parents], message, None
    return [["HEAD~1", "HEAD"]], message, None


def run_rules(diffs: list) -> list:
    """run_rules runs every TT01-TT14 rule over one diff model."""
    findings = []
    for rule in RULES:
        findings.extend(rule(diffs))
    return findings


def finding_key(f):
    """finding_key returns the identity a finding keeps across parents.
    An aggregate ID keys on the ID alone. Every other ID keys on the ID,
    the path, and the message with every digit run replaced by a single
    `#`. The line is never part of the key."""
    if f.id in AGGREGATE_IDS:
        return (f.id,)
    return (f.id, f.path, _DIGIT_RUN.sub("#", f.message))


def introduced_findings(root: Path, comparisons: list) -> list:
    """introduced_findings runs the rules over each comparison. One
    comparison returns its findings unchanged. More return the first
    comparison's findings whose key appears in every other comparison's
    key set: the content the merge itself introduced."""
    first = run_rules(build_diff(root, comparisons[0]))
    if len(comparisons) == 1:
        return first
    shared = None
    for comparison in comparisons[1:]:
        keys = {finding_key(f) for f in run_rules(build_diff(root, comparison))}
        shared = keys if shared is None else shared & keys
    return [f for f in first if finding_key(f) in shared]


def format_finding(f) -> str:
    """format_finding matches the other check_*.py gates' output shape:
    <path>:<line>: <ID> <message>."""
    return f"{f.path}:{f.line}: {f.id} {f.message}"


def main() -> int:
    parser = argparse.ArgumentParser(description="diff-only test-tampering gate")
    parser.add_argument("--range", dest="range_arg", help="diff this range instead of the staged tree")
    parser.add_argument("--message-file", dest="message_file", help="read override trailers from this file")
    parser.add_argument("--probe", action="store_true", help="run the self-test suite and exit")
    args = parser.parse_args()

    if args.probe:
        from test_tampering_probes import run_probe

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
    except DiffError as exc:
        print(str(exc))
        return 2

    unresolved, overridden = resolve_overrides(findings, message)

    for f, t in overridden:
        print(f"{f.id} overridden by: {t.raw_line}")
    for f in unresolved:
        print(format_finding(f))

    return 1 if unresolved else 0


if __name__ == "__main__":
    sys.exit(main())
