# Plan: hook guard command segmentation

Status: the segmentation change shipped in commit `33fa632`. This
revision fixes the two defects that commit recorded as open. It covers
`scripts/agent_hook_guard.py`, one new sibling case-table module, and
one sentence of `docs/architecture.md`. It adds no Go package, no
`api/` lock row, and no `policy/layers.json` row.

## Goal

Make the guard's command-scanning patterns match per command segment
rather than across the whole script, and give the unanchored bypass
literal a `git` anchor that segmentation makes safe.

Today one `git commit` anywhere in a multi-command script, plus any
later token holding an `n`, blocks the whole script. Every bypass
shape the guard blocks today must stay blocked, except where this
plan names the exception and its reason.

This revision closes the last two members of that class. The
write-target scan moves to the same per-segment rule. The hooks-path
pattern learns git's newer `git config` subcommand syntax. Both
changes only add blocked shapes or remove false positives. The only
shapes that stop blocking are the false positives named under defect
class three. No real write and no real hooks-path override stops
blocking.

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
- Per-segment matching for the write-target scan, which reads
  `REDIRECT`, `TEE`, and `SED_TARGET`. This is defect class three
  below.
- One widened `HOOKS_PATH` pattern that reaches git's newer
  `git config` subcommand syntax. This is defect class four below.
- New probe cases in the guard's own probe tables.
- One new module, `scripts/agent_hook_guard_cases.py`, holding those
  tables. The guard file cannot hold them and stay at or below 500
  lines.
- One rewritten paragraph in `docs/architecture.md`.

Outside:

- The `git commit` alternative of `BYPASS` and the `HUSKY`,
  `HUSKY_SKIP_HOOKS`, `SKIP_GIT_HOOKS`, and `LEFTHOOK` alternative.
  Both stay as written.
- The four write-target patterns themselves. `REDIRECT`, `TEE`,
  `SED_TARGET`, and `API_LOCK` keep the text they have today. Only
  the text they read changes. `clean_token`, `target_paths`, and
  `locked_target` are untouched.
- The one sanctioned-command exemption,
  `git config core.hooksPath .githooks`. It stays one exact string,
  compared per segment. No second spelling is exempted, so
  `git config set core.hooksPath .githooks` blocks. Use
  `make install-hooks`.
- `bypass_text`'s message-stripping and option-cluster branches. The
  builder must not edit that function's body.
- `scripts/check_test_tampering.py` and
  `docs/history/test-tampering.md`. A separate change owns both files.
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

### Defect class three: the write-target scan crosses boundaries

`check_command` calls `write_targets(normalize(cmd))` on the whole
command. `normalize` turns every newline into one space. The scan
therefore reads one line that holds every command.

`SED_TARGET` holds a lazy run, `[^\n|;&>]*?`, between the `sed -i`
flag and the captured path. The run looks newline-bounded. That bound
is dead, because no newline survives `normalize`. The run walks into
the next command.

The defect has two measured symptoms.

- A false positive. `sed -i 's/a/b/' foo.go` on one line, then
  `cat api/envelope.txt` on the next, reports the lock file as a write
  target. The whole script is blocked. `head -5` in place of `cat`
  behaves the same way.
- A lost true positive. `sed -i 's/a/b/' api/envelope.txt` on one
  line, then any command on the next line, is allowed today. The lazy
  run backtracks past the real target. It then captures the last token
  of the script, because the trailing `\s*(?:[|;&()>]|$)` anchor only
  matches at the end of the joined line. A semicolon or an
  and-operator in place of the newline still blocks, because the
  anchor finds its separator there.

The fix is the rule the other three scans already follow. Run the
scan once per segment, on that segment's normalized text.
`check_command` already holds the comment-stripped segment list.

### Why per-segment costs `REDIRECT` and `TEE` nothing

Both patterns are per operator. Each starts at its own operator and
captures the next path token. `TOKEN` excludes whitespace, quotes,
`;`, `|`, `&`, and parentheses, so a captured path can never hold a
segment separator. No genuine redirect can straddle a boundary.

The `&` of `2>&1`, `>&2`, and `&>log` is not a separator, by the
`_separates` rule that shipped in `33fa632`. Those three shapes stay
in one segment, so their targets stay visible.

This was proved, not reasoned. A differential sweep ran 1535 generated
commands through the shipped guard and the prototype. It paired 15
real write shapes with 8 benign commands across 5 separators, in both
orders. Four verdicts changed from blocked to allowed. All four are
the `sed -i` false positive above. Sixteen changed from allowed to
blocked. All sixteen are the `sed -i` lost true positive above. Every
real write still blocks, alone and inside a two-command script.

### Why `SED_TARGET` keeps its character class

Leave `[^\n|;&>]*?` as written. The class still does work in the
merged fallback segment. An unterminated quote returns the whole
script as one segment, and that segment holds every command. The
`|`, `;`, and `&` members bound the run there.

The `\n` member is inert, because `normalize` runs first in both
paths. Removing it changes no verdict. Leave it, and do not widen the
class. A wider class would reopen the walk inside the fallback
segment.

### The comment strip now reaches the write-target scan

The segments `check_command` holds are already comment-stripped. The
write-target scan therefore stops seeing a write named only inside a
`#` comment. `echo hi # > api/envelope.txt` is blocked today and
allowed after the change. A shell never runs a comment, so no true
positive is lost. This is the same argument the comment strip already
carries for the other three scans.

### Defect class four: the hooks-path pattern misses a syntax

`HOOKS_PATH` holds the run `(?:-\S+(?:\s+\S+)?\s+)*` between `config`
and the key. Every token in that run must start with `-`. Git 2.46
added a subcommand syntax whose first token is a bare word. The run
cannot cross it, so the pattern never reaches the key.

Three shapes are allowed by the shipped guard and were confirmed
against real git: `git config set core.hooksPath /evil`,
`git config set --global core.hooksPath /evil`, and
`git config unset core.hooksPath`. The first exits zero and sets the
key. This is a live hook-path override, which is the exact action the
guard exists to prevent.

Two more holes of the same shape were found by attacking the fix.

- A config key name is case-insensitive in git. `git config set
  core.HooksPath /evil` and `git -c CORE.hooksPath=/tmp` both work.
  The shipped pattern is case-sensitive, so both are allowed today.
- A section drop takes the key with it. `git config remove-section
  core` and `git config rename-section core mine` both remove
  `core.hooksPath`, which restores the default hooks path. Both the
  classic `--remove-section` spelling and the subcommand spelling are
  allowed today.

### The fix: one widened pattern

Replace the option run after `config` with a generic token run, add
the section alternative, and compile the pattern case-insensitively.

```python
HOOKS_PATH = re.compile(
    r"\bgit\s+(?:-\S+(?:\s+\S+)?\s+)*"
    r"(?:-c\s+\S*core\.hooksPath|"
    r"config\s+(?:\S+\s+)*(?:core\.hooksPath|(?:--)?(?:remove|rename)-section\s+core\b))",
    re.IGNORECASE,
)
```

The generic run `(?:\S+\s+)*` accepts any token between `config` and
the key. No token spelling can escape it. A future git subcommand
needs no further change here.

The run only fires in a segment that already names the key, so it
adds no reach of its own. The segment must also name `git`. The scan
is per segment, so a later command cannot lend the key to an earlier
one.

The run costs no time. A no-match segment holding 160 option tokens
and 160 word tokens takes 1.1 milliseconds. The measured growth is
linear across 20, 40, 80, and 160 tokens.

### Correction: the token run had to nest inside the section clause too

The first version of the pattern put the generic token run only
between `config` and the whole alternation, not between the section
subcommand and `core` itself:

```
config\s+(?:\S+\s+)*(?:core\.hooksPath|(?:--)?(?:remove|rename)-section\s+core\b)
```

`git config remove-section --global core` is real git syntax, a flag
placed after the subcommand rather than before it, and it escaped
this version outright: nothing sits between `remove-section` and
`core` for the run to cover. Verified against git 2.53.0 in an
isolated repository: the command removes the whole `core` section,
core.hooksPath included, and the shipped pattern let it through.

The section alternative now carries its own token run:

```
(?:--)?(?:remove|rename)-section\s+(?:\S+\s+)*core\b
```

A flag before the subcommand, after it, or on both sides all match.
`git config remove-section alias` and its flagged forms stay allowed,
because the run still requires the literal `core` after every token it
consumes.

### The `sed -i` anchor missed the GNU long form

`SED_TARGET` matched only `-i`, so `sed --in-place=.bak -e '...' api/
envelope.txt` wrote to a lock with no detection. The anchor now
accepts either form: `-i\S*` or `--in-place(?:=\S*)?`. This is the
same generic-token philosophy as the hooks-path fix: name the shapes
that write, not the ones that do not.

### Decisions this fix records

- The read subcommand `get` blocks. The classic `--get core.hooksPath`
  is blocked today as an accepted cost. Blocking
  `git config get core.hooksPath` is the consistent choice. The read
  forms write nothing, so each is a false positive, and the project
  keeps them. See the rejected read-form exemption below.
- `remove-section` and `rename-section` block, and only on the `core`
  section. `git config remove-section alias` stays allowed. Dropping
  the `core` section is a hooks-path change by another name.
- The pattern is case-insensitive. `GIT CONFIG` is not a real command,
  so the widened case costs nothing but an unreachable over-block.
- No exemption is added. The sanctioned command keeps its one exact
  spelling.

### Rejected fix: a subcommand allowlist

An allowlist of the `git config` subcommands was rejected. Git names
seven today: `list`, `get`, `set`, `unset`, `rename-section`,
`remove-section`, and `edit`. A new git release adds a row and the
guard silently loses it. The generic token run needs no maintenance
and cannot be outspelled. Prefer the run.

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

### This revision's surface

No function signature changes. `write_targets`, `clean_token`,
`target_paths`, and `locked_target` keep their signatures and their
bodies. `check_command` changes one loop and one docstring sentence.
`HOOKS_PATH` gains the pattern text above and the `re.IGNORECASE`
flag.

One module is added: `scripts/agent_hook_guard_cases.py`. It holds
the five probe tables and nothing else. `probe()` imports it inside
the function body, so the hook path never loads it. The guard runs on
every tool call, and a probe-only import must not cost that path a
file read.

The split is required, not preferred. The guard file is 479 lines
today. The new cases add about 40 lines and the code changes add
about 6. Moving the tables out leaves the guard at 376 lines and the
new module at 165. Both stay under the 500-line limit. Do not raise
the limit.

`scripts/check_names.py` and `scripts/check_structure.py` read Go
files only, so neither gate sees either Python file. The 500-line
limit is the AGENTS.md rule, applied by hand.

`scripts/check_test_tampering.py` already imports sibling modules by
bare name, so the import style is the repository's own. The guard is
invoked by absolute path from `.claude/settings.json` and by relative
path from the `Makefile`. Both put the script's directory first on
`sys.path`, so the bare import resolves.

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

This revision adds a fourth blocked list, `PROBE_LOCK_BLOCKED`,
asserted with `"locks" not in check_command(cmd)`. The word `locks`
appears in the api-lock reason only. It is absent from the fuzz, the
bypass, and the `core.hooksPath` reasons, so the four tables stay
separable.

Keep `probe()` at or below 80 lines. The case tables move to
`scripts/agent_hook_guard_cases.py` in this revision, so the function
holds the loop and nothing else.

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

### This revision's probe cases

Every row below was run against the shipped guard and against a
prototype of this design. The role is measured, not asserted. The
prototype passed all 36 new rows and every row already in the tables.

Twenty-four rows are discriminators. Twelve are pins.

Defect class three, `PROBE_ALLOWED`. Each row is blocked today and
allowed after the change.

- `sed -i` on `foo.go`, then `cat` of an api lock. DISCRIMINATOR.
- The same with `head -5` in place of `cat`. DISCRIMINATOR.
- The same with `grep foo` in place of `cat`. DISCRIMINATOR.
- `echo hi`, then a `#` comment holding a redirect to an api lock.
  DISCRIMINATOR. The comment strip now reaches this scan.

Defect class three, `PROBE_LOCK_BLOCKED`. Each row must block after
the change.

- `sed -i` writing an api lock, then `go build`. DISCRIMINATOR. Today
  the lazy run backtracks past the target and the write escapes.
- `sed -i.bak` writing an api lock, then `make verify`.
  DISCRIMINATOR, for the same reason with a flag suffix.
- `sed -i` writing an api lock, alone. PIN.
- A redirect into an api lock. PIN.
- An appending redirect into `.semgrepignore`. PIN.
- A redirect into an api lock after `2>&1`. PIN. This pins the
  redirect rule inside one segment.
- A benign `echo`, a semicolon, then a redirect into an api lock.
  PIN. This proves a later segment is still scanned.
- A redirect whose path holds a `$( )` span. PIN.
- `make api`, then a `tee` write to an api lock on the next line.
  PIN. This proves `TEE` loses nothing at a boundary.

Defect class four, `PROBE_HOOKS_BLOCKED`. Each row must block after
the change.

- `git config set core.hooksPath /evil`. DISCRIMINATOR.
- The same with `--global` before the key. DISCRIMINATOR.
- `git config unset core.hooksPath`. DISCRIMINATOR.
- `git config unset --all core.hooksPath`. DISCRIMINATOR.
- `git config get core.hooksPath`. DISCRIMINATOR. The read form
  blocks, as the classic `--get` does.
- `git config set --type path core.hooksPath /evil`. DISCRIMINATOR.
  A flag sits between the subcommand and the key.
- `git -C /tmp/repo config set core.hooksPath /evil`. DISCRIMINATOR.
  A global option with a value sits before `config`.
- `git config --file .git/config set core.hooksPath /evil`.
  DISCRIMINATOR. An option with a value sits before the subcommand.
- An environment assignment, then the same write. DISCRIMINATOR.
- The same write with the key in single quotes. DISCRIMINATOR.
- The same write with the value in double quotes. DISCRIMINATOR.
- The same write, then a `#` comment naming a read. DISCRIMINATOR.
- A `get` of the key, a semicolon, then the subcommand write.
  DISCRIMINATOR.
- `git config set core.HooksPath /evil`. DISCRIMINATOR. The key name
  is case-insensitive in git.
- `git -c CORE.hooksPath=/tmp commit`. DISCRIMINATOR, for the same
  reason on the `-c` path.
- `git config remove-section core`. DISCRIMINATOR.
- `git config --remove-section core`. DISCRIMINATOR.
- `git config rename-section core mine`. DISCRIMINATOR.
- `git config --global set core.hooksPath /evil`. PIN. The
  option-with-value branch already swallowed `set` in this one shape,
  so it blocks today.

Defect class four, `PROBE_ALLOWED`. Each row is allowed today and
after the change.

- `git config set user.name mac`. PIN.
- `git config get user.email`. PIN.
- `git config list` piped into `grep hooks`. PIN.
- `git config remove-section alias`. PIN. Only the `core` section
  blocks.

Two rules still bind the probe text. Do not write a case name that is
a letter A through G followed by a digit. Do not write a probe string
that holds both `git` and the bypass literal in one segment through an
inline heredoc. The label scan was run over both prototype files and
reported no hit.

## Defects this revision closes

- The write-target scan crossed command boundaries. It read the whole
  normalized command, so `SED_TARGET`'s lazy run walked into the next
  command. Fixed by running the scan once per comment-stripped
  segment, the rule the other three scans already follow. This closes
  the false positive on a `sed -i` of a source file followed by a read
  of an api lock. It also regains the true positive on a `sed -i` of
  an api lock followed by any newline-separated command.
- `git config set core.hooksPath /evil`, git's newer subcommand
  syntax, escaped `HOOKS_PATH`. Fixed by replacing the option run
  after `config` with a generic token run. Two further holes found
  while attacking that fix are closed in the same pattern: a
  case-varied key name, and a `remove-section` or `rename-section` of
  the `core` section. See defect class four above.

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
- `git config --edit` opens an editor, so the command string never
  names the key. An editor set to a writing command changes the key.
  This is the wrapper class above, reached by another route. The
  guard reads a command string and cannot follow an editor. Do not
  attempt a fix.
- `git config get-regexp core.hooks` names a key prefix, not the key.
  The pattern needs the literal `core.hooksPath`, so the prefix read
  is allowed. A regex read writes nothing, so this is a read-form
  miss and not a bypass. The classic `--get-regexp` spelling of the
  same prefix is equally allowed, today and after the change.
- Every `git config` read of `core.hooksPath` stays blocked: `--get`,
  `--get-all`, `--get-regexp`, `--local --get`, and the `get`
  subcommand. None of them
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
- `_is_doc_companion` accepts `AGENTS.md`, `docs/history/*.md`, and
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

### This revision's verification

The diff holds four files: `scripts/agent_hook_guard.py`,
`scripts/agent_hook_guard_cases.py`, `docs/history/hook-guard.md`, and
`docs/architecture.md`.

Gate outcome, measured against the rule source:

- `_DOC_COMPANION_DIR_PREFIXES` in
  `scripts/test_tampering_rules_infra.py` is
  `("docs/history/", "docs/packages/")`. `check_self_reference_guard`
  fires only when the diff holds a file that is neither gate infra nor
  a doc companion. Two `scripts/` files plus this plan would therefore
  raise no TT11 finding and need no trailer.
- `docs/architecture.md` is not a doc companion, so it makes TT11
  fire. A trailer is needed. The plan does not author it. The
  orchestrator reads the diff and writes `Allow-Gate-Change` with at
  least 15 significant words.
- The doc edit is required, so the trailer is required. The sentence
  in `docs/architecture.md` names the bypass, hooks-path, and fuzz
  scans as the scans that run per segment. This change adds the
  write-target scan to that set. Leaving the sentence alone makes it
  stale by omission. Do not drop the doc edit to avoid the trailer.
- Rewrite that sentence to name four scans, not three. Change nothing
  else in the paragraph. The heredoc false positive stays open.
- TT14 does not fire. `_is_checker_source` covers
  `scripts/check_test_tampering.py`, the `scripts/test_tampering_*`
  modules, and `.githooks/commit-msg`. This change touches none of
  them.
- A branch trailer does not carry to a merge commit, and the gate is
  merge-aware. See
  `.agents/memories/override_trailers_dont_carry_to_merge_commits.md`.

No `api/` lock row changes and no `policy/layers.json` row changes.
`scripts/` holds no Go package, so `scripts/go_packages.py` never
enumerates either file. `python3 scripts/check_api.py` and
`python3 scripts/check_deps.py` were run and both pass.

File sizes after the change: the guard at 376 lines and the case
module at 165. Both are under 500. `probe()` stays under 80 lines.
Every other function is unchanged.
