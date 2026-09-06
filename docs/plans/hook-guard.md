# Plan: hook guard command segmentation

Status: planned, not yet built. This plan covers
`scripts/agent_hook_guard.py` and one paragraph of
`docs/architecture.md`. It adds no Go package, no `api/` lock row, and
no `policy/layers.json` row.

## Goal

Make the guard's command-scanning patterns match per command segment
rather than across the whole script, and give the unanchored bypass
literal a `git` anchor that segmentation makes safe.

Today one `git commit` anywhere in a multi-command script, plus any
later token holding an `n`, blocks the whole script. Every bypass
shape the guard blocks today must stay blocked, except where this
plan names the exception and its reason.

## Scope

Inside:

- One raw-text line-continuation join in
  `scripts/agent_hook_guard.py`.
- One quote-aware, substitution-aware segment splitter in the same
  file.
- Per-segment matching for three patterns: `BYPASS`, `HOOKS_PATH`,
  and `FUZZ`.
- A `git` anchor for the bare `--no-verify` alternative.
- One `git config` narrowing: the sanctioned-command equality is
  compared per segment. No read form is exempted; see the rejected
  fix below.
- New probe cases in the file's own `probe()` function.
- One rewritten paragraph in `docs/architecture.md`.

Outside:

- The `git commit` alternative of `BYPASS` and the `HUSKY`,
  `HUSKY_SKIP_HOOKS`, `SKIP_GIT_HOOKS`, and `LEFTHOOK` alternative.
  Both stay as written.
- The write-target checks: `REDIRECT`, `TEE`, `SED_TARGET`,
  `write_targets`, `clean_token`, `target_paths`, and
  `locked_target`. Each keeps reading the whole normalized command.
  `REDIRECT`, `TEE`, and the path checks are per operator, so a
  command boundary costs them nothing. `SED_TARGET` is the exception
  and leaves one live false positive of the same class as defect one.
  Its `[^\n|;&>]*?` run is bounded by a newline, but `normalize`
  turned every newline into a space before it runs, so the run walks
  into the next command. A `sed -i` on one line, then
  `cat api/envelope.txt` on the next, reports the lock file as a
  write target. That case is blocked today and stays blocked after
  this change. Class one is therefore narrowed, not closed. The fix
  needs its own decision, because it changes which writes the guard
  sees.
- `bypass_text`'s message-stripping and option-cluster branches. The
  builder must not edit that function's body.
- `scripts/check_test_tampering.py` and
  `docs/plans/test-tampering.md`. A separate change owns both files.
- The heredoc-body defect already recorded in `docs/architecture.md`.
  Its body-extent problem is unsolved and the fix is not cheap. It
  stays open, exactly as that paragraph says.
- Any attempt to tell a real repository from a throwaway one. The
  guard reads a command string. A previous `cd` is invisible to it
  and `-C` is trivially spoofable. Such a test would be security
  theatre, so this plan does not attempt it.

### Defect class one: the scan crosses command boundaries

`bypass_text` ends by calling `normalize`, which collapses every
newline into one space. The whole script becomes one line. Three
patterns then read across command boundaries.

- `BYPASS`'s `git commit` alternative holds `(?:\S+\s+)*` between the
  subcommand and the flag. That run walks into later commands. A
  benign `git rev-list -n 1 HEAD` on a later line matches as if it
  were an option of the commit.
- `HOOKS_PATH` compares its exemption against the whole normalized
  string: `norm != "git config core.hooksPath .githooks"`. The
  sanctioned command is therefore rejected whenever it is not the
  entire input. `make install-hooks` on a first line, plus that exact
  command on a second line, is blocked today.
- `FUZZ` holds `[^|;&]*`, which is bounded by three characters only.
  It never stops at a line break, because `bypass_text` already
  turned every newline into a space before the pattern runs. A later
  line naming the fuzz flag blocks an unrelated `go test` run.
- `PARALLEL` is searched over the same whole string, so a
  `-parallel` token in an unrelated command falsely exempts a real
  fuzz run.

### Defect class two: the bypass literal has no anchor

`BYPASS`'s first alternative is a bare `--no-verify` with no `git`
context. It matches that literal anywhere: inside a heredoc body, a
quoted string, a comment, a search pattern, or a file path.
Segmentation alone does not fix it, because the literal sits inside
one segment.

### Rejected fix: a read-form exemption

`HOOKS_PATH` also matches the `git config` read forms, which write
nothing. An exemption for them was designed four times, and each
version produced a verified `core.hooksPath` bypass under attack. It
is dropped. The reasons are recorded under known defects, so a later
reader does not rebuild it.

## API

No exported Go symbol changes, so `make api-update` produces no diff.
`scripts/` holds no Go package, so `scripts/check_deps.py`,
`scripts/check_api.py`, and `scripts/go_packages.py` never see the
file. Three module-level Python functions and two module-level
patterns are added to `scripts/agent_hook_guard.py`. No pattern is
added for the hooks-path check; it keeps the patterns it has today
and only changes the text it reads.

- `join_continuations(cmd: str) -> str` — replaces every backslash
  followed by an optional carriage return and a newline with one
  space. It runs on the raw command text.
- `command_segments(cmd: str) -> list[str]` — returns the command's
  separate shell commands, in order.
- `NO_VERIFY` — the bare literal, moved out of `BYPASS`.
- `GIT_TOKEN` — `\bgit\b`, the anchor for `NO_VERIFY`.
- `strip_comment(seg: str) -> str` — drops a trailing shell comment
  from one segment.

`BYPASS` loses its first alternative and keeps the other two.

### The comment strip

`strip_comment` walks one segment and tracks quote state. It removes
each `#` that sits outside every quote and is preceded by whitespace
or by nothing. A `#` inside single or double quotes is literal. A `#`
with a non-blank character before it, such as `foo#bar`, is literal.

The removal must stop at the comment's own newline. A shell comment
ends at the line end, so text on a later line is executed code. The
strip must keep that text and resume its walk there. It removes the
rest of the segment only when no newline follows the `#`.

The strip runs per segment, after `command_segments` and before
`normalize` or `bypass_text`. It must see the raw segment, because
`normalize` deletes the quotes that decide whether a `#` is a comment.

The strip applies to every per-segment pattern, not only to the
hooks-path check. A comment carries attacker-chosen text into all of
them today. Removing that text loses no true positive once the strip
is line-bounded, because a shell never executes a comment. Without
that bound the claim is false: the strip would delete executed
commands on later lines. It removes false positives in the other
patterns for the same reason.

The strip does not apply to `write_targets`, which still reads the
whole command. That check is out of scope here, as recorded above.

The strip runs after the split, so an apostrophe inside a comment
still opens a quote state that never closes, and the unterminated-
quote fallback still returns the whole script as one segment. A merged
segment holds many lines, so the line bound above is load-bearing
there. An unbounded strip deletes every command after the first
comment and under-blocks. With the line bound, a merged segment can
only over-block, which is no worse than today. The strip therefore
removes comment text from the patterns and never adds reach to them.

### Order of operations

The split must run on the raw text, before any normalization.
`normalize` destroys newlines, so a split after it has no boundary
left to cut on. The call chain becomes:

`check_command` -> `command_segments` (raw text) -> per segment
`strip_comment` -> `normalize` or `bypass_text` -> pattern search.

`join_continuations` runs first, inside `command_segments`.

### Segment rules

`command_segments` walks the joined text one character at a time. It
tracks quote state, command-substitution depth, and backtick state.

- Outside quotes, a backslash escapes the next character. Both
  characters stay in the segment. The escaped character never acts as
  a separator.
- Outside quotes, `'` opens a single-quoted span. Only the next `'`
  closes it. A backslash has no special meaning inside that span.
- Outside quotes, `"` opens a double-quoted span. A backslash inside
  that span escapes the next character, so an escaped quote stays
  inside the span. Only an unescaped `"` closes it.
- Outside quotes, a `$(`, a `<(`, or a `>(` raises the substitution
  depth by one. Outside quotes, a `)` lowers it, but only while the
  depth is above zero. Inside either quote the walker must not touch
  the depth. An unbalanced `$(` inside a message would otherwise
  raise a depth that never closes, collapse the rest of the script
  into one segment, and bring back the whole false-positive class
  this change exists to remove.
- Outside quotes, a backtick toggles backtick state. Inside either
  quote the walker must not touch that state.
- Outside quotes, at depth zero, and outside backticks, each of `;`,
  `&`, the newline, and the carriage return ends the current
  segment. A `|` ends it under the same conditions.
- An `&` is not a separator when the preceding non-blank character is
  `>`, or when the next character is `>`. This keeps `2>&1`, `>&2`,
  and `&>log` inside one command.
- A run of separators yields empty segments, which the function
  drops.
- A still-open quote at the end of the text means the input is not
  parseable. The function then returns the joined text as one
  segment. This matches today's whole-text behavior, so no true
  positive is lost.
- Whitespace-only segments are dropped. An empty result returns the
  joined text as one segment.

Scanning single characters covers the two-character operators with no
extra rule. Each yields one boundary plus one dropped empty segment.

### Unexecuted text inside a segment

Text can enter a segment without the shell ever running it: a `#`
comment, a `$(...)` or backtick span, a quoted string, a heredoc
body, and an operand after a bare `--`. None of them can now weaken a
check, because no check has an exemption a token can arm. The comment
channel is removed by `strip_comment`, which must stop at the
comment's own newline. Without that bound the strip deletes executed
commands and loses true positives. The rest can only add text to a
segment, which can only over-block.

### Negative control: bare parentheses are not tracked

The walker raises depth only for `$(`, `<(`, and `>(`. It must never
raise depth for a bare `(`. Tracking a subshell group would merge the
commands inside it into one segment and create a new false positive.
Two probe cases pin this. A subshell group holding a commit and a
later `head -n 5` stays allowed. A subshell group holding a real
`git commit -n` stays blocked.

### Why quote state is tracked

`bypass_text` strips a `-m` value with `shlex`, so message text
cannot match the pattern. A quote-blind split breaks that guarantee.
A commit whose message holds a semicolon and the bypass literal would
split inside the message. The `-m` value would no longer be one
token, so the stripper could not remove it, and the second segment
would match. Quote tracking keeps every message in one segment.
Escaped quotes need the double-quote rule above for the same reason.

A message naming the bypass literal does not discriminate here. The
`git` anchor rescues the trailing fragment, so such a case holds one
verdict under both splitters. The discriminating case is a message
that names the flag path: a commit whose message reads
`fix; git commit -n later`. That case is allowed under this design
and blocked under a quote-blind splitter, in both quote styles. It
was run both ways to confirm it.

### Why command substitution spans are tracked

An earlier draft of this plan claimed substitution tracking was
unnecessary. That claim was tested and is false. A substitution that
sits between `git commit` and its own `-n` splits the command and
loses the true positive. Five shapes were confirmed blocked today and
allowed under the path-blind splitter: a pipe, a semicolon, and an
and-operator inside `$(...)`, the same inside backticks, and a pipe
inside `<(...)`. The redirect rule above was confirmed the same way,
against three redirect shapes. The path-blind splitter is therefore
rejected, and the record of that rejection stays here.

### Why the `git` anchor is safe only with segmentation

Counting the bare literal only in a segment that also names `git`
depends on segments being real commands. Without segmentation, one
`git` token anywhere in a script re-arms the literal everywhere, so
the anchor would buy nothing. This is why the anchor rides with this
change instead of a later one.

The anchor is a gate-strength change, not only a bounding change. Two
probe cases prove the strength holds: a commit with the literal, and
a push with the literal, both stay blocked.

### The fuzz exemption must be same-segment

`FUZZ` and `PARALLEL` must both be evaluated against the same
segment. A whole-command `PARALLEL` search would let an unrelated
`grep -parallel 2` in a later command exempt a real fuzz run. That
case is allowed today and must be blocked after the change.

## Tests

The file's own `probe()` function holds the case table.
`python3 scripts/agent_hook_guard.py --probe` runs it, and `make
verify-fast` runs that command.

Today `probe()` asserts one `blocked` list with
`"fuzz" not in check_command(cmd)`. That assertion only fits the fuzz
reason string. A bypass finding returns a different reason. Add a
second list, `bypass_blocked`, asserted with
`"bypass" not in check_command(cmd)`. The word `bypass` appears in
the bypass reason only, not in the fuzz reason and not in the
`core.hooksPath` reason. Add a third list for the hooks-path reason,
asserted with `"hooksPath" not in check_command(cmd)`.

Keep `probe()` at or below 80 lines. Move the case tables to
module-level constants if the function grows past that.

Two rules bind the probe's own text. Do not write a case name that is
a letter A through G followed by a digit; `scripts/check_labels.py`
scans every file in the tree, this one included. Do not write a probe
string that holds both `git` and the bypass literal in one segment
through an inline heredoc; see the known defects below.

Every case below was run against today's code and against a
prototype of this design. The role of each case is recorded. A
discriminator changes verdict between the two. A pin holds the same
verdict and guards against a regression.

### Discriminators: false positives fixed

Each row is blocked today and allowed after the change.

- A commit on one line, then `git rev-list -n 1 HEAD` on the next.
- A commit on one line, then `head -n 5 file` on the next.
- A commit, an and-operator, then `go build -n ./...`.
- A commit piped to `tee`, then `head -n 2 log`.
- A commit whose double-quoted message holds an apostrophe, then
  `head -n 5 f`.
- A commit with `2>&1`, then `head -n 5 f`.
- A subshell group holding a commit and a later `head -n 5 f`.
- `make install-hooks`, then the sanctioned
  `git config core.hooksPath .githooks` command.
- `git config --get x`, then a `#` comment holding a hooks-path
  write. The write is commented out, so the segment must be allowed.
  This case also proves the strip is not inverted.
- A commit whose single-quoted message holds an unbalanced `$(`, then
  `head -n 5 f`. This pins the quote scope of the depth rule.
- The same message in double quotes, for the same reason.
- A commit whose double-quoted message holds a real newline, then
  `head -n 5 f`.
- A commit whose message holds a `#` character, then `head -n 5 f`.
  This row was first recorded as a pin. It is a discriminator: the
  trailing `head -n 5 f` makes today's code block it.
- `go test ./...`, then a line naming the fuzz flag.
- A heredoc whose prose names the bypass literal.
- An `echo` whose text names the bypass literal.
- A `command grep` whose pattern is the bypass literal.
- A `python3 -c` whose printed string is the bypass literal.
- A `cat` of a path named after the bypass literal.

### Discriminators: true positives regained

Each row is allowed today and blocked after the change.

- `ls -lm`, then `git commit -n -m x` on the next line. Today
  `bypass_text` reads `-lm` as a message cluster and eats the
  following `git` token, so the pattern cannot match.
- A fuzz run piped into `grep -parallel 2`. Today the unrelated
  `-parallel` token falsely exempts the run.

### Discriminators against the rejected path-blind splitter

Each row is blocked today, blocked after the change, and allowed
under a splitter with no redirect rule and no substitution tracking.
Each therefore separates this design from the under-specified one.

- A commit with `2>&1` before an `-n` flag.
- A commit with `>&2` before an `-n` flag.
- A commit with `&>log` before an `-n` flag.
- A commit whose `-m` value is a `$(...)` span holding a pipe.
- The same with a semicolon inside the span.
- The same with an and-operator inside the span.
- The same with a backtick span.
- The same with a `<(...)` span.

### Pins: true positives that must not move

Each row is blocked today and after the change.

- `git commit -n -m x`.
- `git commit -an -m x`, an `n`-bearing option cluster.
- `git commit --no-verify -m x`, which proves the anchor keeps the
  literal armed.
- `git push --no-verify origin main`, the same proof for a second
  subcommand.
- `git -c user.name=x commit -n -m y`, a global option before the
  subcommand.
- A commit, a backslash line continuation, then `-n -m x`. A shell
  continuation is one command and a real bypass. It is blocked today
  because `normalize` strips the backslash. A newline split without
  the continuation join would allow it, which was confirmed against
  the prototype. This case is the guard against that weakening.
- The four env forms: `HUSKY=0`, `HUSKY_SKIP_HOOKS=1`,
  `SKIP_GIT_HOOKS=1`, and `LEFTHOOK=0`, each before a commit.
- A subshell group holding `go build` and a real `git commit -n`.
- A `git config` write of `core.hooksPath` to another path.
- A `git config --unset` of `core.hooksPath`.
- A `git -c core.hooksPath=/tmp` commit.
- A `-c core.hooksPath=` override whose commit message names `--get`.
- A `-c core.hooksPath=` override whose commit message is `--list`.
- A `git config` write of `core.hooksPath` with a trailing `--get`.
- A `--global` hooks-path write, then a `#` comment naming
  `git config --list`. The shell runs the write, so it must block.
- A hooks-path write, then a `#` comment naming a read of the same
  key.
- A `--add` hooks-path write, then a `#` comment naming a read.
- A hooks-path write whose operand is a `$(...)` read of another key.
- The same write with a backtick read span.
- A hooks-path write with a trailing space and a bare `#`.
- A read of `core.hooksPath`, a semicolon, then a hooks-path write.
- A read, an and-operator, then a hooks-path write.
- A read whose operand is a `$(...)` span holding a hooks-path
  write. The span stays in one segment, so the write must block.
- The same shape with a backtick span.
- A read on one line, then a hooks-path write whose `#` comment
  holds an apostrophe. The apostrophe merges the script into one
  segment, which must still block.
- The read forms themselves: `--get`, `--get-all`, `--get-regexp`,
  `--local --get`, and a leading environment run before a read.
  Each stays blocked, exactly as today.
- A real `git commit -n`, then a `#` comment. The strip must not hide
  the bypass.
- A comment line, then a real bypass, then an apostrophe. The
  apostrophe merges the script into one segment. An unbounded strip
  deletes the bypass, so this row pins the line bound. One row per
  reason word: a commit, a hooks-path write, and a fuzz run.
- A `-c core.hooksPath=` override with a `--get.txt` operand after a
  bare `--`.
- A fuzz run with no `-parallel` flag.
- `/usr/bin/git commit --no-verify`, which proves the anchor survives
  a path prefix.
- `env git commit --no-verify`, which proves it survives a launcher.

### Pins: false positives that must stay fixed

Each row is allowed today and after the change.

- A commit whose double-quoted message holds a semicolon and the
  bypass literal.
- The same message in single quotes.
- The same message with an escaped double quote inside it.
- A commit whose double-quoted message reads
  `fix; git commit -n later`. This one discriminates against a
  quote-blind splitter; the three rows above do not.
- The same message in single quotes.
- A commit, then `cat foo#bar.txt`, where the `#` is literal.
- `git config --list` piped into `grep`.
- A fuzz run that already carries `-parallel 2`.
- The existing entry whose commit message names the fuzz flag.

## Known defects that stay open

- An invocation through a shell variable, an alias, or a wrapper name
  escapes the guard. `$GIT commit --no-verify`, `mygit commit
  --no-verify`, and `g commit --no-verify` are blocked today and
  allowed after the anchor lands. This opens no new class. The
  `git commit` alternative is already blind to exactly the same
  shapes: `$GIT commit -n -m x`, `mygit commit -n -m x`, and
  `g commit -n -m x` are all allowed by today's code, which was
  confirmed by running them. The anchor makes the literal path as
  blind as the flag path, and no blinder. A path prefix and a
  launcher both still block, because the word boundary holds after a
  slash and after a space. Do not attempt a fix here. Naming every
  wrapper is a different mechanism and needs its own decision.
- `git config set core.hooksPath /evil`, git's newer subcommand
  syntax, is allowed today and stays allowed. `HOOKS_PATH`'s option
  run cannot cross the bare `set` token. This was run against real
  git: the command exits zero and the key holds the new value. The
  hole is pre-existing and is not this change's to fix. Record it and
  leave it.
- Every `git config` read of `core.hooksPath` stays blocked: `--get`,
  `--get-all`, `--get-regexp`, and `--local --get`. None of them
  writes anything, so each is a false positive. An exemption was
  designed four times and attacked four times. Every version
  produced a working `core.hooksPath` override, confirmed against real
  git by reading the key back. Two shapes defeated the last and
  strictest version: a write inside a `$(...)` or backtick span
  after an exempt read, and an apostrophe inside a `#` comment,
  which merges the script into one segment whose head is the read.
  The exemption is dropped. It bought convenience and cost a
  reachable bypass every time. Do not rebuild it. The workaround
  for a probe repository is its own `.git/hooks/` directory, which
  needs no guard change.
- The bypass literal quoted beside a `git` token in one segment stays
  blocked. A probe table line naming both is the common shape. This
  guard cannot tell a command from data, so quoting its own command
  is not separable from issuing it. Do not attempt a fix. The
  workaround is to author such content with the Write tool instead of
  an inline heredoc. Three agents hit this in one day.
- A probe repository needs its own hooks. Writing to `.git/hooks/` is
  the accepted workaround and needs no guard change.
- An unpaired apostrophe in a later command leaves the whole text as
  one segment, so a commit followed by `head -n 5 don't` stays
  blocked. This is no worse than today. An apostrophe inside a
  double-quoted message is fixed, because the quote pairs.
- The heredoc-body defect recorded in `docs/architecture.md` stays
  open. Its cause and its unsolved body-extent problem are unchanged.

## Verification

Commands that must pass:

- `python3 scripts/agent_hook_guard.py --probe`
- `python3 scripts/check_prose.py`
- `python3 scripts/check_labels.py`
- `python3 scripts/check_plan.py`
- `make verify`

Gate findings this change triggers:

- `scripts/` matches `_GATE_INFRA_PREFIXES` in
  `scripts/test_tampering_rules_infra.py`, so TT11 fires when the
  diff also holds a file that is not a doc companion.
- `_is_doc_companion` accepts `AGENTS.md`, `docs/plans/*.md`, and
  `docs/packages/*.md`. This plan file is a companion.
  `docs/architecture.md` is not, so the doc edit makes TT11 fire.
- A TT11 waiver needs `Allow-Gate-Change` with at least 15
  significant words. `_GATE_CHANGE_MIN_WORDS` is 15 and
  `_TEST_CHANGE_MIN_WORDS` is 6, both in
  `scripts/test_tampering_override.py`. `Allow-Test-Change` cannot
  waive TT11.
- The builder must not author the trailer, and must not drop the doc
  edit to avoid it. The orchestrator verifies the diff and writes the
  trailer.
- TT14 does not fire. `_is_checker_source` covers
  `scripts/check_test_tampering.py`, the `scripts/test_tampering_*`
  modules, and `.githooks/commit-msg`. This change touches none of
  them.
- No `api/` lock changes and no `policy/layers.json` row. Confirmed
  by running `python3 scripts/check_api.py` and
  `python3 scripts/check_deps.py`.

Doc update:

- `docs/architecture.md` is the only file outside this plan that
  describes the guard's matching behavior. `command grep -rln
  "agent_hook_guard" docs/` returns that file and this plan.
- That file's gate-system section proposes, as an available but
  unreviewed mitigation, skipping the bypass scan when the whole
  command is a single simple shell segment. This change lands the
  opposite behavior: the scan runs per segment, and every segment is
  scanned.
- Rewrite that sentence rather than appending to it. State that the
  bypass, hooks-path, and fuzz scans each run once per shell command
  segment, that a backslash-newline continuation joins into one
  segment, and that the bypass literal counts only in a segment that
  names `git`.
- Do not claim the heredoc false positive is fixed. It stays open.
