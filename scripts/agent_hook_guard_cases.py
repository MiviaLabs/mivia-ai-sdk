"""Probe case tables for scripts/agent_hook_guard.py --probe.
Imported only inside probe(), so the hot hook path on a normal
tool call never loads this file."""

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
PROBE_HOOKS_BLOCKED += [
    # Discriminators: a flag placed after the subcommand, not
    # before it, is real git syntax the earlier fix missed.
    "git config remove-section --global core",
    "git config remove-section -f /tmp/other core",
    "git config rename-section --global core mine",
    # Discriminators: git's subcommand syntax, missed by the old
    # dash-flag-only option run.
    "git config set core.hooksPath /tmp/evil",
    "git config set --global core.hooksPath /tmp/evil",
    "git config unset core.hooksPath",
    "git config get core.hooksPath",
    # Discriminator: git config keys are case-insensitive.
    "git config set core.HooksPath /evil",
    "git -c CORE.hooksPath=/tmp commit -m x",
    # Discriminators: dropping the whole section drops the key too.
    "git config remove-section core",
    "git config --remove-section core",
    "git config rename-section core other",
    "git config --global remove-section core",
    "git config -f /tmp/other remove-section core",
    "FOO=1 git config set core.hooksPath /evil",
]
# Pins: a write-target token from one command must never block another.
PROBE_API_BLOCKED = [
    # Discriminators: sed's GNU long-form flag was never matched.
    "sed --in-place=.bak -e 's/a/b/' api/envelope.txt",
    "sed --in-place -e 's/a/b/' api/envelope.txt",
    "sed -i \'s/a/b/\' api/envelope.txt",
    "sed -i \'s/a/b/\' api/envelope.txt\ngo build ./...",
    "make api\ngo doc x | tee api/envelope.txt",
    "cat x 2>&1 > api/envelope.txt",
]
PROBE_ALLOWED += [
    # Pins: a section drop on an unrelated section, with a flag
    # before or after the subcommand, stays allowed.
    "git config remove-section alias",
    "git config remove-section --global alias",
    # Discriminators: a write-target token must not cross a boundary.
    "sed -i \'s/a/b/\' foo.go\ncat api/envelope.txt",
    "sed -i \'s/a/b/\' foo.go\nhead -5 api/envelope.txt",
    # Pins: a config write on an unrelated key or section stays allowed.
    "git config remove-section branch.foo",
    "git config set some.other.key hooksPathIsNotThis",
]
