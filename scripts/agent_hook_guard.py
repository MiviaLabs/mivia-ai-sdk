#!/usr/bin/env python3
"""PreToolUse guard for agent hooks. Reads the hook event JSON from
stdin and blocks forbidden actions:
- Commands: Git hook bypass flags, skip env vars, core.hooksPath overrides,
  or direct writes to api/*.txt and .semgrepignore.
- File edits: manual edits to generated api/*.txt locks (use make api-update)
  or the pinned .semgrepignore.
Supports both Claude Code and Antigravity hook protocols.
The guard is best-effort against careless agents. It is not a security
boundary; a determined actor can bypass it. No CI exists in this repo,
so gates on the committed tree stay aspirational until CI exists."""
import json
import os
import re
import shlex
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Hook bypass and skip env vars. The commit -n flag matches at
# end-of-string; operands, option clusters, and global options may sit
# anywhere between git and the n-bearing flag. Message values are
# stripped before this pattern runs, so message text cannot match.
BYPASS = re.compile(
    r"--no-verify|"
    r"\bgit\s+(?:-\S+(?:\s+\S+)?\s+)*commit\s+"
    r"(?:\S+\s+)*(-[a-zA-Z]*n[a-zA-Z]*)(?:\s|$)|"
    r"\b(HUSKY\s*=\s*0|HUSKY_SKIP_HOOKS|SKIP_GIT_HOOKS|LEFTHOOK\s*=\s*0)\b"
)
# core.hooksPath overrides; the one sanctioned command is exempted first.
HOOKS_PATH = re.compile(
    r"\bgit\s+(?:-\S+(?:\s+\S+)?\s+)*"
    r"(?:-c\s+\S*core\.hooksPath|config\s+(?:-\S+(?:\s+\S+)?\s+)*core\.hooksPath)"
)
# go test -fuzz without -parallel spawns one worker per core, each with
# unbounded memory; it OOM-kills the desktop session that launched it.
# -fuzztime bounds duration, not workers, so it does not exempt a run.
FUZZ = re.compile(r"\bgo\s+test\b[^|;&]*\s-fuzz\b")
PARALLEL = re.compile(r"-parallel(?:\s+|=)\S+")
# A write-target token may carry a parenthesized span such as a command
# substitution; the span stays whole so spaces inside it are captured.
# The base class excludes parens so the span group must consume them.
TOKEN = r"[^\s\"';|&()]+(?:\([^)]*\)[^\s\"';|&()]*)*"
# Write operators and the path token each one targets. The token is
# resolved with the same path logic as the file-tool checks.
REDIRECT = re.compile(r">>?\s*[\"']?(" + TOKEN + ")")
TEE = re.compile(r"\btee\s+(?:-\S+\s+)*[\"']?(" + TOKEN + ")")
SED_TARGET = re.compile(
    r"\bsed\s+-i\S*[^\n|;&>]*?[\"']?(" + TOKEN.replace("|&()", "|&>()") + r")\s*(?:[|;&()>]|$)"
)
API_LOCK = re.compile(r"(^|/)api/[^/]+\.txt$")

FILE_TOOLS_CLAUDE = ("Write", "Edit", "MultiEdit", "NotebookEdit")
FILE_TOOLS_ANTIGRAVITY = ("write_to_file", "replace_file_content", "multi_replace_file_content")
SUBST = re.compile(r"\$\([^)]*\)")
BACKTICK = re.compile(r"`[^`]*`")


def normalize(cmd: str) -> str:
    """Strip shell quotes and backslashes; collapse whitespace."""
    cmd = cmd.replace("\\", "")
    cmd = re.sub(r"['\"`]", "", cmd)
    return re.sub(r"\s+", " ", cmd).strip()


def bypass_text(cmd: str) -> str:
    """Return the command with commit message values removed, so message
    text cannot match the bypass pattern. Falls back to the normalized
    command when shlex cannot tokenize the input."""
    try:
        toks = shlex.split(cmd)
    except ValueError:
        return normalize(cmd)
    kept = []
    i = 0
    while i < len(toks):
        t = toks[i]
        if t == "--":
            break  # operands after -- are paths, not flags
        if t in ("-m", "--message"):
            i += 2  # the value is the message; skip it
            continue
        if t.startswith("--message="):
            i += 1
            continue
        if (
            len(t) > 1
            and t.startswith("-")
            and not t.startswith("--")
            and t[1:].isalpha()
            and t.endswith("m")
            and "n" not in t
        ):
            i += 2  # a combined cluster like -am: the value follows
            continue
        kept.append(t)
        i += 1
    return normalize(" ".join(kept))


def clean_token(tok: str) -> str:
    """Drop command-substitution spans from a write target token."""
    tok = SUBST.sub("", tok)
    return BACKTICK.sub("", tok)


def write_targets(cmd: str) -> list[str]:
    """Return the path tokens that redirects, tee, and sed -i target."""
    out = list(REDIRECT.findall(cmd))
    out.extend(TEE.findall(cmd))
    out.extend(SED_TARGET.findall(cmd))
    return out


def target_paths(path: str) -> list[str]:
    """Return the normpath, repo-relative, and realpath forms of path."""
    if not path:
        return []
    out = [os.path.normpath(path)]
    if path.startswith("/"):
        out.append(path.lstrip("/"))
    try:
        rel = os.path.normpath(os.path.relpath(path, ROOT))
        out.append(rel)
    except (OSError, ValueError):
        pass
    if os.path.exists(path):
        rp = os.path.realpath(path)
        out.append(rp)
        try:
            out.append(os.path.normpath(os.path.relpath(rp, ROOT)))
        except (OSError, ValueError):
            pass
    return out


def locked_target(path: str) -> str:
    """Return "api", "semgrepignore", or "" for a target path."""
    for cand in target_paths(path):
        if API_LOCK.search(cand):
            return "api"
        if cand == ".semgrepignore" or cand == str(ROOT / ".semgrepignore"):
            return "semgrepignore"
    return ""


def check_command(cmd: str) -> str:
    """Check command for forbidden bypass or writes. Return reason if blocked."""
    norm = normalize(cmd)
    if HOOKS_PATH.search(norm) and norm != "git config core.hooksPath .githooks":
        return "blocked: core.hooksPath overrides are forbidden; use `make install-hooks`"
    stripped = bypass_text(cmd)
    if FUZZ.search(stripped) and not PARALLEL.search(stripped):
        return (
            "blocked: `go test -fuzz` with default parallelism spawns one worker per core"
            " with unbounded memory and OOM-kills the session that launched it;"
            " rerun capped: `go test -fuzz <Target> -parallel 2 -fuzztime 60s`"
        )
    if BYPASS.search(bypass_text(cmd)):
        return "blocked: Git hook bypass is forbidden; fix the gate failure instead"
    for target in write_targets(norm):
        if locked_target(clean_token(target)):
            return "blocked: api/ locks and .semgrepignore are generated; run `make api-update` and commit the diff"
    return ""


def check_file_target(path: str) -> str:
    """Check file path for locked targets. Return reason if blocked."""
    locked = locked_target(path)
    if locked == "api":
        return "blocked: api/ locks are generated; run `make api-update` and commit the diff"
    if locked == "semgrepignore":
        return "blocked: .semgrepignore is pinned by scripts/check_semgrepignore.py; change the gate deliberately"
    return ""


def probe() -> int:
    """Assert the fuzz guard fires on violations and stays silent on
    clean commands. Wired into make verify-fast, following the --probe
    convention of the other gates."""
    blocked = [
        "go test ./agentloop/ -run XXXX -fuzz FuzzCanonicalizeArgs -fuzztime 90s",
        "go test -fuzz=FuzzTruncateContent ./agentloop/",
        "go test ./... -fuzz FuzzDecode",
    ]
    allowed = [
        "go test ./agentloop/ -run XXXX -fuzz FuzzCanonicalizeArgs -fuzztime 90s -parallel 2",
        "go test -fuzz FuzzDecode -parallel=4 ./mcp/",
        "go test ./agentloop/... ",
        "go test ./agentloop/ -fuzztime 90s",
        "git commit -m 'run go test -fuzz next'",
    ]
    for cmd in blocked:
        if "fuzz" not in check_command(cmd):
            print(f"probe failed to block: {cmd}", file=sys.stderr)
            return 1
    for cmd in allowed:
        if reason := check_command(cmd):
            print(f"probe blocked a clean command ({reason}): {cmd}", file=sys.stderr)
            return 1
    return 0


def main() -> int:
    if "--probe" in sys.argv[1:]:
        return probe()
    try:
        event = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0  # unparseable input must not block the workflow

    if "toolCall" in event:
        call = event.get("toolCall", {})
        tool = call.get("name", "")
        args = call.get("args", {})
        reason = ""
        if tool == "run_command":
            reason = check_command(args.get("CommandLine", ""))
        elif tool in FILE_TOOLS_ANTIGRAVITY:
            reason = check_file_target(args.get("TargetFile", ""))

        if reason:
            print(json.dumps({"decision": "deny", "reason": reason}))
        else:
            print(json.dumps({"decision": "allow"}))
        return 0

    tool = event.get("tool_name", "")
    data = event.get("tool_input", {})
    reason = ""
    if tool == "Bash":
        reason = check_command(data.get("command", ""))
    elif tool in FILE_TOOLS_CLAUDE:
        path = data.get("file_path") or data.get("path") or data.get("notebook_path") or ""
        reason = check_file_target(path)

    if reason:
        print(reason, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
