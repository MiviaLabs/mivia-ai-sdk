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
    r"\bgit\s+(?:-\S+(?:\s+\S+)?\s+)*commit\s+"
    r"(?:\S+\s+)*(-[a-zA-Z]*n[a-zA-Z]*)(?:\s|$)|"
    r"\b(HUSKY\s*=\s*0|HUSKY_SKIP_HOOKS|SKIP_GIT_HOOKS|LEFTHOOK\s*=\s*0)\b"
)
# The bare bypass flag counts only in a segment that also names git.
# The flag alone matches prose, a path, and a search pattern.
NO_VERIFY = re.compile(r"--no-verify")
GIT_TOKEN = re.compile(r"\bgit\b")
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


def join_continuations(cmd: str) -> str:
    """Join every backslash line continuation into one line."""
    return re.sub(r"\\\r?\n", " ", cmd)


def _quoted_span(text: str, i: int) -> int:
    """Return the index after the quoted span starting at text[i], or -1
    when the quote never closes. A backslash escapes inside "" only."""
    quote = text[i]
    i += 1
    while i < len(text):
        ch = text[i]
        if quote == '"' and ch == "\\":
            i += 2
            continue
        if ch == quote:
            return i + 1
        i += 1
    return -1


def _separates(text: str, i: int) -> bool:
    """Report whether text[i] ends a command segment. An & that belongs
    to 2>&1, >&2, or &>log is part of a redirect, not a separator."""
    ch = text[i]
    if ch in ";|\n\r":
        return True
    if ch != "&":
        return False
    if text[i + 1:i + 2] == ">":
        return False
    j = i - 1
    while j >= 0 and text[j] in " \t":
        j -= 1
    return j < 0 or text[j] != ">"


def command_segments(cmd: str) -> list[str]:
    """Return the command's separate shell commands, in order. Quotes,
    $( ) spans, <( ) spans, >( ) spans, and backtick spans hold their
    text in one segment. A bare ( is not tracked, so a subshell group
    still splits. An unterminated quote returns the whole text."""
    text = join_continuations(cmd)
    segments = []
    start = 0
    depth = 0
    tick = False
    i = 0
    while i < len(text):
        ch = text[i]
        if ch == "\\":
            i += 2
            continue
        if ch in "'\"":
            j = _quoted_span(text, i)
            if j < 0:
                return [text]
            i = j
            continue
        if ch == "`":
            tick = not tick
            i += 1
            continue
        if text[i:i + 2] in ("$(", "<(", ">("):
            depth += 1
            i += 2
            continue
        if ch == ")" and depth > 0:
            depth -= 1
            i += 1
            continue
        if depth == 0 and not tick and _separates(text, i):
            segments.append(text[start:i])
            start = i + 1
        i += 1
    segments.append(text[start:])
    kept = [s for s in segments if s.strip()]
    return kept or [text]


def strip_comment(seg: str) -> str:
    """Return the segment without its shell comments. A # inside a quote
    is literal. A # with a non-blank character before it, such as
    foo#bar, is literal. A comment ends at its own newline, so the strip
    keeps every later line. A segment holds more than one line when the
    unterminated-quote fallback returns the whole script. Run this
    before normalize, which deletes the quotes that decide whether a #
    opens a comment."""
    out = []
    i = 0
    while i < len(seg):
        ch = seg[i]
        if ch == "\\":
            out.append(seg[i:i + 2])
            i += 2
            continue
        if ch in "'\"":
            j = _quoted_span(seg, i)
            if j < 0:
                out.append(seg[i:])
                break
            out.append(seg[i:j])
            i = j
            continue
        if ch == "#" and (i == 0 or seg[i - 1] in " \t"):
            nl = seg.find("\n", i)
            if nl < 0:
                break
            i = nl
            continue
        out.append(ch)
        i += 1
    return "".join(out)


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
    """Check command for forbidden bypass or writes. Return reason if
    blocked. The bypass, hooks-path, and fuzz scans read one command
    segment each. The write-target scan reads the whole command."""
    segments = [strip_comment(seg) for seg in command_segments(cmd)]
    for seg in segments:
        norm = normalize(seg)
        if HOOKS_PATH.search(norm) and norm != "git config core.hooksPath .githooks":
            return "blocked: core.hooksPath overrides are forbidden; use `make install-hooks`"
    for seg in segments:
        stripped = bypass_text(seg)
        if FUZZ.search(stripped) and not PARALLEL.search(stripped):
            return (
                "blocked: `go test -fuzz` with default parallelism spawns one worker per core"
                " with unbounded memory and OOM-kills the session that launched it;"
                " rerun capped: `go test -fuzz <Target> -parallel 2 -fuzztime 60s`"
            )
    for seg in segments:
        stripped = bypass_text(seg)
        if BYPASS.search(stripped) or (
            NO_VERIFY.search(stripped) and GIT_TOKEN.search(stripped)
        ):
            return "blocked: Git hook bypass is forbidden; fix the gate failure instead"
    for target in write_targets(normalize(cmd)):
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


# Probe cases. Each list is asserted on one reason word. A pin holds one
# verdict across the segment split. A discriminator changes verdict.
PROBE_FUZZ_BLOCKED = [
    # Pins: a fuzz run with no -parallel flag.
    "go test ./agentloop/ -run XXXX -fuzz FuzzCanonicalizeArgs -fuzztime 90s",
    "go test -fuzz=FuzzTruncateContent ./agentloop/",
    "go test ./... -fuzz FuzzDecode",
    # Discriminator: an unrelated -parallel must not exempt the run.
    "go test -fuzz FuzzDecode ./mcp/ | grep -parallel 2",
    # Pin: a comment ends at its own newline. An unbounded strip would
    # delete the fuzz run from this merged segment.
    "echo hi # n\ngo test -fuzz FuzzX ./p/ ; echo don't",
]
PROBE_BYPASS_BLOCKED = [
    # Pins: bypass shapes that must not move.
    "git commit -n -m x",
    "git commit -an -m x",
    "git commit --no-verify -m x",
    "git push --no-verify origin main",
    "git -c user.name=x commit -n -m y",
    "git commit \\\n-n -m x",
    "HUSKY=0 git commit -m x",
    "HUSKY_SKIP_HOOKS=1 git commit -m x",
    "SKIP_GIT_HOOKS=1 git commit -m x",
    "LEFTHOOK=0 git commit -m x",
    "(go build ./... ; git commit -n -m x)",
    "git commit -n -m x # note",
    "/usr/bin/git commit --no-verify",
    "env git commit --no-verify",
    # Discriminators against a splitter with no redirect rule.
    "git commit 2>&1 -n -m x",
    "git commit >&2 -n -m x",
    "git commit &>log -n -m x",
    # Discriminators against a splitter with no substitution tracking.
    "git commit -m $(echo a | head -1) -n",
    "git commit -m $(echo a; echo b) -n",
    "git commit -m $(true && echo b) -n",
    "git commit -m `echo a | head -1` -n",
    "git commit -m <(echo a | head -1) -n",
    # Discriminator: a true positive the segment split regains.
    "ls -lm\ngit commit -n -m x",
    # Pin: a comment ends at its own newline. An unbounded strip would
    # delete the commit from this merged segment.
    "echo hi # note\ngit commit -n -m x ; echo don't",
]
PROBE_HOOKS_BLOCKED = [
    # Pins: every hooks-path write stays blocked.
    "git config core.hooksPath /evil",
    "git config --unset core.hooksPath",
    "git -c core.hooksPath=/tmp commit -m x",
    "git -c core.hooksPath=/tmp commit -m '--get'",
    "git -c core.hooksPath=/tmp commit -m --list",
    "git config core.hooksPath /evil --get",
    "git config --global core.hooksPath /evil\n# git config --list",
    "git config core.hooksPath /evil\n# git config --get core.hooksPath",
    "git config --add core.hooksPath /evil\n# git config --get core.hooksPath",
    "git config core.hooksPath $(git config --get user.name)",
    "git config core.hooksPath `git config --get user.name`",
    "git config core.hooksPath /evil #",
    "git config --get core.hooksPath; git config core.hooksPath /evil",
    "git config --get core.hooksPath && git config core.hooksPath /evil",
    "git config --get $(git config core.hooksPath /evil)",
    "git config --get `git config core.hooksPath /evil`",
    "git config --get core.hooksPath\ngit config core.hooksPath /evil # don't",
    "git -c core.hooksPath=/tmp commit -m x -- --get.txt",
    # Pins: the read forms have no exemption and stay blocked.
    "git config --get core.hooksPath",
    "git config --get-all core.hooksPath",
    "git config --get-regexp core.hooksPath",
    "git config --local --get core.hooksPath",
    "FOO=1 git config --get core.hooksPath",
    # Pin: a comment ends at its own newline. An unbounded strip would
    # delete the hooks-path write from this merged segment.
    "echo hi # n\ngit config core.hooksPath /evil ; echo don't",
]
PROBE_ALLOWED = [
    # Discriminators: false positives the segment split fixes.
    "git commit -m x\ngit rev-list -n 1 HEAD",
    "git commit -m x\nhead -n 5 file",
    "git commit -m x && go build -n ./...",
    "git commit -m x | tee log\nhead -n 2 log",
    "git commit -m \"don't\"\nhead -n 5 f",
    "git commit -m x 2>&1\nhead -n 5 f",
    "(git commit -m x; head -n 5 f)",
    "make install-hooks\ngit config core.hooksPath .githooks",
    "git config --get user.name\n# git config core.hooksPath /evil",
    "git commit -m 'wip $( fix'\nhead -n 5 f",
    "git commit -m \"wip $( fix\"\nhead -n 5 f",
    "git commit -m \"line one\nline two\"\nhead -n 5 f",
    "go test ./...\necho use -fuzz next time",
    # Discriminators: the git anchor clears the bare flag in prose.
    "cat <<'EOF'\nnever use --no-verify\nEOF",
    "echo \"never pass --no-verify\"",
    "command grep -r \"--no-verify\" docs/",
    "python3 -c \"print('--no-verify')\"",
    "cat notes/--no-verify.md",
    # Pins: false positives that must stay fixed.
    "git commit -m \"wip; --no-verify\"",
    "git commit -m 'wip; --no-verify'",
    "git commit -m \"wip \\\" ; --no-verify\"",
    "git commit -m \"fix; git commit -n later\"",
    "git commit -m 'fix; git commit -n later'",
    "git commit -m \"wip #1\"\nhead -n 5 f",
    "git commit -m x\ncat foo#bar.txt",
    "git config --list | grep hooks",
    "go test ./agentloop/ -run XXXX -fuzz FuzzCanonicalizeArgs -fuzztime 90s -parallel 2",
    "go test -fuzz FuzzDecode -parallel=4 ./mcp/",
    "go test ./agentloop/... ",
    "go test ./agentloop/ -fuzztime 90s",
    "git commit -m 'run go test -fuzz next'",
]


def probe() -> int:
    """Assert every scan fires on its violations and stays silent on
    clean commands. Wired into make verify-fast, following the --probe
    convention of the other gates. The case tables are module-level."""
    tables = (
        ("fuzz", PROBE_FUZZ_BLOCKED),
        ("bypass", PROBE_BYPASS_BLOCKED),
        ("hooksPath", PROBE_HOOKS_BLOCKED),
    )
    for word, cases in tables:
        for cmd in cases:
            if word not in check_command(cmd):
                print(f"probe failed to block ({word}): {cmd!r}", file=sys.stderr)
                return 1
    for cmd in PROBE_ALLOWED:
        if reason := check_command(cmd):
            print(f"probe blocked a clean command ({reason}): {cmd!r}", file=sys.stderr)
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
