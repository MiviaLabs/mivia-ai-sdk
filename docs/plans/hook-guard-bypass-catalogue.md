# Hook-guard heredoc bypass catalogue

This file records security research, not work to be done. An attempt to
clear a false positive in `scripts/agent_hook_guard.py` was abandoned.
Three hostile review rounds found sixteen real hook-bypass
constructions in three successive designs. The sixteenth is structural.
The approach was judged not to converge, so the fix was dropped and the
false positive stays.

Read this file before you try the same fix again.

## The workaround

The false positive has two clean workarounds. Both work today, and
neither needs a guard change.

- Pass the commit message with `-m`. The guard already strips `-m` and
  `--message` values before the bypass matcher runs.
- Write the message to a file, then use `git commit -F <path>`. Only
  the stdin form `git commit -F -` with a heredoc is affected.

## The false positive

`bypass_text` at `scripts/agent_hook_guard.py:62-94` removes `-m` and
`--message` values from the command. It does not remove heredoc bodies.
So `git commit -F -` with a heredoc feeds the whole commit message to
the `BYPASS` matcher at `scripts/agent_hook_guard.py:25-30`.

Measured rejections on the current guard:

- a body that names the long bypass flag in prose.
- a body that names the short bypass flag in prose.
- a body line that starts with a hyphen and holds the letter `n`, for
  example a bullet written `-new nudge`.
- a body with an apostrophe. `shlex.split` raises on the unbalanced
  quote, and the fallback then scans the raw command, body included.
- the same bodies under `<<'EOF'`, `<<"EOF"`, `<<-EOF`, `<< 'EOF'`,
  `<<\EOF`, `<<E-F`, and `<<E'O'F`.
- the same bodies under `git commit --file=-` and `git tag -F -`.

One earlier report blamed a hyphenated word such as `one-liner`. That
theory is wrong. Measurement disproves it. The token must start with a
hyphen for the n-bearing alternative to match.

## The catalogue

Sixteen constructions defeated three successive designs. The rounds
were: round one, which stripped every heredoc body; round two, which
allowlisted the consumer command; round three, which allowlisted the
whole `git commit -F -` shape under nine conditions.

Each snippet below carries a real hook bypass in the heredoc body,
unless the entry says otherwise. The literal bypass flag is written as
`<flag>` here, because the guard blocks a shell command that holds it.

### Interpreter consumers that read stdin (round one)

A naive strip removes the body of every heredoc. Any consumer that
executes stdin then runs the removed text.

1. `bash <<EOF` with the bypass in the body.

2. `sh -s <<EOF`, and the same body under `sudo sh <<EOF`.

3. `python3 - <<PY`. The body calls `subprocess.run` with the flag. The
   same shape also holds the flags as plain string fixtures, and no
   parser can tell a fixture from a call.

4. `ssh host <<EOF`, and `docker exec -i c sh <<EOF`. Both send the
   body to a shell on the far side.

5. `xargs <<EOF`, and `node <<EOF`.

6. `cat <<EOF | bash`. The consumer word is harmless. The pipe carries
   the body to an interpreter.

### Delimiter and terminator parser traps (round one)

The strip must find where the body ends. A wrong end deletes real
command text, and a real bypass hides in the deleted span.

7. Exotic delimiter words. `<<E-F`, `<<E'O'F`, `<<E.F`, `<<"EO"F`, and
   `<<\EOF` each name a terminator that a simple word reader misparses.
   The body then runs past its true terminator and swallows the next
   line, which holds a real bypass.

8. A missing terminator line. `cat <<EOF` with no terminator, then a
   real bypass on a later line. A strip that runs to the end of input
   deletes the bypass and the guard sees nothing.

### Operators that are not heredocs (round one)

9. A `<<` that is not a heredoc operator, followed by a real bypass on
   the next line. Three forms: the arithmetic shift `echo $((1<<3))`;
   the quoted literals `grep 'x <<EOF' f` and `printf "a<<b\n"`; and
   the herestring `cat <<<'x'`. Each makes a naive strip consume the
   bypass line as a heredoc body.

### Command-substitution and expansion forms (rounds one and two)

An unquoted delimiter leaves expansion on. The shell then executes text
inside the body before any consumer reads it.

10. A `$( ... )` span or a backtick span inside an unquoted body. The
    nested form `$( ... $( ... ) ... )` also defeats the guard's
    `SUBST` pattern, because that pattern stops at the first `)`.

11. The bash 5.3 function substitutions `${ cmd; }` and `${| cmd; }`.
    Both execute. Neither holds `$(` and neither holds a backtick, so a
    test for those two markers misses them. Found in round two.

### Consumer-resolution spoofs (round two)

Round two allowlisted a consumer command word. These forge that word.

12. A quoted `;` that spoofs the segment start:

        bash -s 'a ;cat x' <<EOF
        git commit <flag>
        EOF

    The segment scan reads backwards to the nearest `;`, lands inside
    the quoted argument, and resolves the consumer as `cat`. Verified
    by execution in bash 5.3.9; the marker file was created.

13. A git alias that routes the body to a shell:

        git -c alias.z='!sh' z <<EOF
        git commit <flag>
        EOF

    The command word is `git`, which the allowlist accepted. The alias
    makes `sh` the real consumer. Verified by execution in bash 5.3.9;
    the marker file was created.

### Write-then-execute (round two)

14. `cat > runme <<EOF` with the bypass in the body, then `bash runme`
    on the next line. The strip removes the flag literal from the
    guard's view. The file is still written, and the next command runs
    it. Verified by execution in bash 5.3.9; the marker file was
    created.

15. A line continuation that hides the pipe:

        cat <<EOF \
        | bash
        git commit <flag>
        EOF

    The operator line ends with a backslash, so the pipe sits on the
    next physical line. A per-line pipe test misses it. Verified by
    execution in bash 5.3.9; the marker file was created.

### The same-line two-operator case (round three)

16. Two heredoc operators on one line, in two segments:

        bash -s <<A; git commit -F - <<B
        git commit <flag>
        A
        msg
        B

    Verified by execution in bash 5.3.9. The prototype in
    `proto3.py` allowed it, and `exec_case.sh` created the marker file.

## Why enumeration does not converge

Construction sixteen is the load-bearing finding. It is not another
missing condition. It is a property of the strip algorithm itself.

Walk the mechanism. The line carries two operators, `<<A` and `<<B`.
The algorithm reads them left to right, and holds one body cursor for
the line. Bash assigns bodies in that same order, so the shared cursor
is correct.

Operator one, `<<A`, belongs to `bash -s`. The allowlist rejects it,
which is the wanted verdict. But "reject" means strip nothing for that
operator. The cursor does not advance. The algorithm has no other
choice, because it did not parse body A.

Operator two, `<<B`, belongs to `git commit -F -`. That is the
genuinely allowlisted shape. It passes every one of the nine
conditions. It then searches for its terminator from the un-advanced
cursor. It finds line `A` is not `B`, keeps going, finds `B`, and
deletes every line between the cursor and `B`. Body A is inside that
span.

The guard now scans a command with body A removed. Body A is exactly
the text that `bash -s` executes.

The fix is not another condition. Consider what a fix must compute: the
extent of a rejected operator's body. That extent is frequently
unknowable. An unparseable delimiter word gives no terminator to search
for. A missing terminator line gives no end at all. In both cases there
is no defensible answer to "where does that body end", so the cursor
cannot be advanced correctly and the next operator inherits the error.

A shell can route a body to a program in more ways than a list can
hold. Three rounds and sixteen constructions are the evidence. Living
with the false positive is the correct outcome for a security guard.

## What the final design got right

Do not discard the round-three design wholesale. Its nine-condition
allowlist blocked all fifteen prior constructions. It also blocked
eighteen further spoofs built against it. Only the structural case
above escaped.

The design allowlisted one shape: `git commit` or `git tag`, reading
its message from stdin through `-F -` or `--file=-`. It stripped
nothing else. The nine conditions were:

1. Mask quoted spans on the operator's physical line first. Replace the
   contents of every balanced `'...'` and `"..."` span with spaces, and
   record an unterminated quote position. Read the delimiter word from
   the unmasked text.
2. The command is `git commit` or `git tag`. The segment starts after
   the nearest `;`, `|`, `&`, newline, `(`, or `{`. The first token
   must be exactly `git`, with no path and no leading assignment. Skip
   global options; `-c`, `-C`, `--git-dir`, `--work-tree`,
   `--namespace`, and `--exec-path` each consume the next token.
3. The message comes from stdin. The tokens after the subcommand must
   hold `-F` followed by `-`, or must hold `--file=-`. The joined form
   `-F-` is not accepted.
4. No output redirect. Strip nothing when the masked line holds `>`.
   This closes write-then-execute.
5. No pipe and no line continuation. Strip nothing when the masked line
   holds `|`, or when the line ends with a backslash.
6. The operator is a real heredoc. Accept `<<` and `<<-`. Reject `<<<`,
   reject an operator preceded by `(` or `<`, and reject an operator
   that the mask blanked or that follows an unterminated quote.
7. The delimiter is a whole word. Skip spaces and tabs after the
   operator. Read to the first whitespace character or one of `;`, `|`,
   `&`, `<`, `>`, `(`, `)`. Remove quotes and backslashes to get the
   terminator. Expansion is off when the word holds any quote or
   backslash.
8. The terminator must exist. The body ends at the first line equal to
   the terminator, compared after leading tabs are removed for `<<-`.
   Strip nothing when no such line exists.
9. Expansion cannot reach the body. When expansion is on, strip nothing
   if the body holds a backtick or any `$`. Test for any `$`, not for
   `$(`, so the bash 5.3 function substitutions are covered.

The prototypes are `proto.py` (round one and two) and `proto3.py`
(round three). The case tables are `run1.py`, `run2.py`, and `run3.py`.
The structural case reproduces through `confirm.py` and `exec_case.sh`.

## A tractable alternative, untried

One alternative was identified and never tried. Record it here as an
option, not as a recommendation.

Narrow the input instead of parsing the shell. Skip the `BYPASS` scan
only when the whole command is a single simple segment. A single simple
segment holds no `;`, no `|`, no `&`, no `>`, and no second `<<`.
Everything else keeps the current behavior.

Every construction in this catalogue needs at least one of those
characters, or a second operator. The check is a character test, not a
parser, so it has no body-extent problem.

This alternative is untried and unreviewed. It needs its own plan and
its own hostile review before anyone writes code for it. Do not treat
this section as approval to act.

## Correction to a false claim

A claim circulated through this work that write-then-execute is already
possible through `printf` and a redirect. That claim is wrong.

The command `printf 'git commit <flag>' > x` is blocked today. The flag
literal sits in argv, which is exactly where `BYPASS` reads. Nothing
strips it.

Stripping a `cat` heredoc body is different. The strip removes the flag
literal from the guard's view, while the file still gets written and a
later command still runs it. That asymmetry is why `cat` and `tee` were
dropped from the round-three allowlist.
