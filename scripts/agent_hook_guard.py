#!/usr/bin/env python3
"""PreToolUse guard for agent hooks. Reads the hook event JSON from
stdin and exits 2 (block) with a reason on stderr when the call is a
forbidden action:
- Bash: Git hook bypass flags or skip env vars.
- Write/Edit: manual edits to generated api/*.txt locks (use make api-update).
Any other call exits 0 (allow)."""
import json
import re
import sys

BYPASS = re.compile(
    r"--no-verify|\bgit\s+commit\s+(-[a-zA-Z]*n[a-zA-Z]*\s)|"
    r"\b(HUSKY\s*=\s*0|HUSKY_SKIP_HOOKS|SKIP_GIT_HOOKS|LEFTHOOK\s*=\s*0)\b"
)
API_LOCK = re.compile(r"(^|/)api/[^/]+\.txt$")


def main() -> int:
    try:
        event = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0  # unparseable input must not block the workflow
    tool = event.get("tool_name", "")
    data = event.get("tool_input", {})

    if tool == "Bash":
        cmd = data.get("command", "")
        if BYPASS.search(cmd):
            print("blocked: Git hook bypass is forbidden; fix the gate failure instead", file=sys.stderr)
            return 2
    if tool in ("Write", "Edit"):
        path = data.get("file_path", "")
        if API_LOCK.search(path):
            print("blocked: api/ locks are generated; run `make api-update` and commit the diff", file=sys.stderr)
            return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
