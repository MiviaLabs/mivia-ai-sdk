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
import sys
from pathlib import Path

from test_tampering_diff import DiffError, build_diff, commit_message, has_parent_commit, has_staged_changes, \
    repo_root, tip_commit
from test_tampering_override import resolve_overrides
from test_tampering_rules import ALL_RULES as TEST_FILE_RULES
from test_tampering_rules_infra import ALL_RULES as INFRA_RULES

RULES = TEST_FILE_RULES + INFRA_RULES


def resolve_diff_source(range_arg: str, message_file: str, root: Path):
    """resolve_diff_source picks the diff args and the message source,
    following the plan's resolution order: --range, the staged tree,
    then the HEAD~1...HEAD fallback. Returns (diff_args, message,
    skip); skip is set when there is no parent commit to fall back to."""
    if range_arg:
        diff_args = [range_arg]
        message = commit_message(root, tip_commit(range_arg))
        return diff_args, message, False
    if has_staged_changes(root):
        message = Path(message_file).read_text() if message_file else None
        return ["--cached"], message, False
    if not has_parent_commit(root):
        return None, None, True
    return ["HEAD~1", "HEAD"], commit_message(root, "HEAD"), False


def run_rules(diffs: list) -> list:
    """run_rules runs every TT01-TT14 rule over one diff model."""
    findings = []
    for rule in RULES:
        findings.extend(rule(diffs))
    return findings


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
        diff_args, message, skip = resolve_diff_source(args.range_arg, args.message_file, root)
        if skip:
            print("check_test_tampering: no parent commit; skipping")
            return 0
        diffs = build_diff(root, diff_args)
    except DiffError as exc:
        print(str(exc))
        return 2

    findings = run_rules(diffs)
    unresolved, overridden = resolve_overrides(findings, message)

    for f, t in overridden:
        print(f"{f.id} overridden by: {t.raw_line}")
    for f in unresolved:
        print(format_finding(f))

    return 1 if unresolved else 0


if __name__ == "__main__":
    sys.exit(main())
