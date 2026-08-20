# Phase 75: mutation kit hardening

Status: shipped. All three gaps closed. This phase closes the three gaps that
`docs/plans/agents/phase74_mutation_coverage_rollout.md` recorded and
deferred. Phase 74 shipped as commit `24a1938`. Its "Tool gap" and
"Verify wiring" sections are the source for all three gaps below.

## Goal

Make the mutation kit reliable, honest, and enforced. Reliable means a
sweep leaks no runaway test binary. Honest means the operator set finds
boundary and arithmetic bugs. Enforced means a scheduled job runs
`make mutation-gate` on a fixed cadence.

## Scope

Inside:

- Gap 1: process-group start and kill in the kit's `go test` call, plus
  progress output during a sweep.
- Gap 2: a wider `OPERATOR_MUTATIONS` set, plus one new baseline sweep
  for each of the ten packages that gain a site, and a rewritten floor
  in those ten `scripts/mutation_denylist/*.json` files.
- Gap 3: a scheduled GitHub Actions workflow that runs
  `make mutation-gate`.
- Doc updates: the mutation paragraphs in `docs/architecture.md`, and a
  status note in this plan after each gap lands.

Outside:

- Closing any survivor the wider operator set reveals. A survivor is
  follow-up work with its own plan. One exception applies: the user
  directed the `schema` survivor work that "Gap 2 result" records.
- New floors for packages that hold no denylist file today. Phase 74
  lists them as future work; that list does not change here.
- Any change to `make verify` or `make verify-fast`. The `--probe` step
  stays the only mutation step inside `make verify`.
- Any Go package, Go source file, or `api/` lock.

## Ordering

The orchestrator fixed the order: gap 1, then gap 2, then gap 3. This
plan accepts that order. The reasoning follows.

- Gap 1 first. Gap 2 needs ten full sweeps. A leaking sweep starves
  every later mutant of CPU and makes the measurement slow and noisy.
  Fixing the leak first makes the gap 2 baseline trustworthy.
- Gap 2 second. Gap 3 schedules an unattended run of every floor. A
  floor that the widened operator set will invalidate must not enter a
  scheduled job first. Otherwise the first scheduled run fails on stale
  floors.
- Gap 3 last. An unattended sweep on a runner with no watcher must not
  accumulate orphan processes. Gap 1 removes that risk.

This plan proposes no reorder. Each gap lands as its own commit.

## Gap 1: process-group leak

### Defect

`run_mutant` in `scripts/check_mutation.py` calls `subprocess.run` with
`timeout=TEST_TIMEOUT_SECONDS` (60). On expiry Python kills the direct
child only. That child is the `go test` driver. The driver has already
spawned a separate compiled binary at `<tmp>/b001/<pkg>_test.test`. The
binary keeps running, CPU-bound, until its own `-test.timeout` expires.
That limit is ten minutes for `ledger`.

Phase 74 confirmed the leak twice. Its measurement pass left eleven
orphans. Its shipped `ledger` re-sweep left six orphans after the sweep
exited normally. The success path leaks, not the timeout path alone.
The orchestrator reaped about eighteen runaway binaries by hand.

### Fix

Add `scripts/mutation_process.py`, a sibling of
`scripts/mutation_tokenize.py`. `check_mutation.py` is 435 lines today;
a new module keeps both files short. The module exports two functions.

```
kill_group(pid) -> None
run_test_group(argv, cwd, timeout) -> str
```

`kill_group` sends `SIGKILL` to the whole group with `os.killpg`. It
swallows `ProcessLookupError`, so a second call on a dead group is a
no-op. It is idempotent by design, because every path below calls it.

`run_test_group` returns `"pass"`, `"fail"`, or `"timeout"`. It obeys
these rules.

- Start the child with `subprocess.Popen(..., start_new_session=True)`.
  The child then leads its own process group.
- On timeout, call `kill_group`, then reap.
- On normal completion, call `kill_group` again. This closes the
  confirmed success-path leak.
- On `KeyboardInterrupt`, call `kill_group` in a `finally` block, then
  re-raise. The module already remaps SIGTERM to `KeyboardInterrupt`.
- Use `os.killpg` only. POSIX is the supported platform. On a platform
  without `os.killpg`, raise `MutationError` with a clear message.

`run_mutant` then calls `run_test_group` instead of `subprocess.run`.
Its `classify` call and its `finally` restore stay unchanged.

### Progress output

`check_mutation.py` buffers stdout, so a 30-minute sweep shows nothing
until it ends. This plan fixes that in the same commit, because it is
cheap. `sweep` prints one line per mutant to stderr with
`flush=True`. The line holds the index, the total, the file name, the
offset, and the verdict. Existing `SURVIVED:` lines stay on stdout and
keep their exact format, so no reader of the sweep output breaks.

### Proof

A fix to kill handling that no test exercises is worthless. Gap 1 is
proven by a new probe inside `check_mutation.py --probe`, which
`make verify` already runs. `scripts/check_structure.py` scans `*.go`
only, so no Go structure limit applies; keep both Python files under
500 lines anyway.

`run_test_group` takes one more parameter for this proof: an injectable
wait, defaulting to `proc.wait(timeout)`. The probe passes a wait that
raises, so the interrupt path runs with no real signal.

Add `_probe_process_group`. It runs five checks and stays under one
second in total.

1. Timeout path. Start a shell that spawns a background `sleep 120`,
   writes the grandchild PID to a file, and then sleeps itself. Call
   `run_test_group` with a 0.3-second timeout. Assert the return value
   is `"timeout"`. Assert the grandchild is dead.
2. Success path. Start a shell that spawns a background `sleep 120`,
   writes the grandchild PID, and then exits at once. Assert the return
   value is `"pass"`. Assert the grandchild is dead.
3. Exit code mapping. Assert a shell exiting non-zero returns
   `"fail"`.
4. Interrupt path. Start the same shell plus grandchild as check 1.
   Call `run_test_group` with an injected wait. That wait first polls
   for the PID file to its 2-second deadline, and only then raises
   `KeyboardInterrupt`. The poll order is mandatory. Without it, the
   kill can run before the shell forks the grandchild, and the check
   becomes vacuous. Assert two things. First, the
   `KeyboardInterrupt` propagates out of `run_test_group`. Second, the
   grandchild is dead inside the 2-second poll deadline. This is the
   path that failed in production: phase 74's external SIGTERM left
   `ledger/store.go` holding a live mutant.
5. Idempotence. Call `kill_group` twice on an already-dead group.
   Assert neither call raises, so `ProcessLookupError` stays swallowed.

Check 4 fails if the builder deletes the `finally` block. Without that
block, the injected `KeyboardInterrupt` propagates before any kill
runs. The grandchild then stays alive for its full `sleep 120`, so the
second assertion fails at the 2-second deadline. The first assertion
alone would pass, so the liveness assertion carries the proof.

The probe must never flake, because `make verify` runs `--probe` on
every commit. Two timing rules make it deterministic.

- Poll for the PID file in a loop, to a 2-second deadline. Never read
  the file after a fixed sleep. The probe cannot poll before calling
  `run_test_group`, because that call starts the shell. Check 4 polls
  inside its injected wait, as described above. Checks 1 and 2 poll
  after the call returns, and both are safe: check 2's shell writes the
  file and then exits, and check 1's 0.3-second timeout is far longer
  than the write needs.
- Poll `os.kill(pid, 0)` in a loop, to a 2-second deadline, and pass as
  soon as it raises `ProcessLookupError`. Never assert liveness after a
  fixed sleep. Every deadline is a ceiling, not a wait, so the normal
  run costs milliseconds.

The probe uses `/bin/sh` and the stdlib only. It plants no checked-in
fixture file, matching the existing probe convention.

### Gap 1 result

Gap 1 shipped. All five checks pass in about 0.5 seconds, so the added
cost to `make verify` is negligible.

Three planted mutations confirm the checks are live. Deleting the
`finally` block gives `interrupt path: the grandchild process
survived`. Deleting every kill gives `success path: the grandchild
process survived`. Flipping `code == 0` to `code != 0` gives
`success path: want 'pass', got 'fail'`. Only the kill inside the
`TimeoutExpired` branch survives mutation, because the `finally` still
enforces the same invariant. That is redundancy, not a gap.

## Gap 2: operator set too narrow

### Defect

`OPERATOR_MUTATIONS` in `scripts/mutation_tokenize.py` covers `==`,
`!=`, `<`, `<=`, `&&`, `||`, a dropped `!`, and a dropped `continue`.
It holds no `>` or `>=` pair and no arithmetic operator. A boundary bug
written with `>` instead of `>=` is invisible to every floor the kit
measures today.

### Exact additions

Add four entries to `OPERATOR_MUTATIONS` and the same four token texts
to the `wanted` map inside `TOKENIZER_SRC`.

| Token | Replacement | Reason |
| --- | --- | --- |
| `>` | `>=` | boundary off-by-one, the missing mirror of `<` |
| `>=` | `>` | boundary off-by-one, the missing mirror of `<=` |
| `+` | `-` | index, offset, and budget arithmetic |
| `-` | `+` | index, offset, and budget arithmetic |

### Multiplication and division stay out

An earlier draft added `*` and `/`. Both are dropped, for one measured
reason. In Go source, `*` is mostly pointer syntax: a receiver, a field
type, or an element type such as `[]*T`. That token mutates to `/T`,
fails `go build`, and lands as discarded, which the rate excludes. The
six-operator set added 412 sites, of which 314 were `*` and one was
`/`. `mcp` gained 30 `*` sites and no new information. `secretpath`
gained two, both useless. `subagent` gained 114 guaranteed build
failures for six informative sites.

Multiplication coverage needs a binary-versus-unary site guard in
`sites_from_tokens`, so the kit mutates `a * b` and skips `*T`. That
guard is its own change, with its own review, and this plan defers it.
Division stays out with it: exactly one `/` token exists module-wide,
in `agentloop`. Dropping `/` also removes a scoring concern. A mutated
divisor can panic on zero, which scores as a kill by crash, not a kill
by assertion.

Two further notes hold for the four operators that stay:

- A `+` on strings mutates to `-` and fails the build. The kit
  classifies that mutant as discarded. A discarded mutant costs one
  build and no test run.
- No new guard is needed. `sites_from_tokens` keeps its `!` adjacency
  guard, and `go/scanner` merges `+=` and `--` into their own tokens.
  A compound assignment therefore never yields a bare `+` site.

### Re-measurement cost

A stored floor becomes stale the moment the package gains a site.
`scripts/mutation_denylist/` holds twelve files, confirmed by listing
the directory: `a2aclient`, `agentloop`, `contextstate`, `dispatch`,
`envelope`, `ledger`, `machine`, `mcp`, `schema`, `secretpath`,
`subagent`, `workspace`.

Ten of the twelve need one new sweep. `mcp` and `secretpath` gain zero
new sites, so their mutant sets are identical to the sets behind their
current floors. A re-sweep of an identical mutant set buys nothing.
Both files stay byte-identical, exactly as phase 74 kept `ledger.json`
byte-identical after its own re-measurement. `mcp` therefore leaves the
margin list below, so its floor stays at 100. Cutting it to 99 with no
measurement behind it would weaken a gate by rule alone.

No package with even one new site is skipped. A new site can move a
rate either way, so a stale floor could turn the first scheduled
`make mutation-gate` run red. That is the failure the ordering section
exists to prevent.

Every count below comes from running the tokenizer alone, under the
current set and under the four-operator set. No sweep ran during
planning. The 97 new sites are 43 `>`, 5 `>=`, 39 `+`, and 10 `-`.

| Package | Sites now | Sites after | Growth | Base sweep |
| --- | --- | --- | --- | --- |
| workspace | 59 | 67 | 1.14x | about 3 min |
| subagent | 58 | 64 | 1.10x | about 3 min |
| agentloop | 105 | 129 | 1.23x | about 6 min |
| dispatch | 42 | 43 | 1.02x | about 1 min |
| a2aclient | 28 | 30 | 1.07x | about 5 min |
| schema | 18 | 35 | 1.94x | about 2 min |
| ledger | 132 | 140 | 1.06x | about 30 min |
| envelope | 70 | 77 | 1.10x | about 4 min |
| machine | 51 | 52 | 1.02x | about 3 min |
| contextstate | 137 | 160 | 1.17x | about 8 min |
| mcp (skipped) | 17 | 17 | 1.00x | not swept |
| secretpath (skipped) | 14 | 14 | 1.00x | not swept |

Totals across all twelve: 731 sites now, 828 after, 1.13x growth. The
base column uses the phase 74 measured per-mutant rates where they
exist: `dispatch` 0.9 seconds, `a2aclient` 9.2 seconds, and `ledger` 13
seconds. Unmeasured packages use 3 seconds per mutant. The ten swept
packages total about 65 minutes.

`ledger` alone costs 30 of those 65 minutes for 8 new sites. It stays
in the sweep set anyway. It runs unattended, and its 3-hour bound caps
the risk.

Hang risk is near zero, and the evidence is direct. A `+` to `-` swap
hangs only when the mutated token is a loop's progress step. None of
the 49 new `+` and `-` sites is one. About 15 are string
concatenation, which fails the build and is discarded. The rest are
slice-capacity arithmetic, index arithmetic, and plain field
arithmetic, which panic or fail fast. The one loop site,
`machine/definition.go:123`, reads `for j := i + 1; j < len(...); j++`,
and still terminates, because the `j++` progress step is a separate
token the kit never mutates. This codebase writes progress steps as
`++` and `+=`, and `go/scanner` emits both as distinct tokens.

The 65-minute base therefore stands with headroom. Budget 95 minutes of
expected wall time and three hours in the worst case.

Each package gets a 60-minute wall-clock bound, except `ledger`, which
gets three hours, matching phase 74's own `ledger` bound. If a package
exceeds its bound, the builder stops that sweep, keeps the stored
floor, records the timeout in this plan, and opens a follow-up. The
builder does not lock a floor from a partial sweep.

### Baseline rule

A sweep under the widened operator set is a new baseline, not a
re-sweep of the same operator set. The phase 74 two-run rule therefore
does not apply. Each package gets one run.

The reasoning: the two-run rule exists to stop a re-sweep from
ratcheting a floor above what the suite reliably clears. A first
measurement under a new operator set cannot ratchet against a prior
number, because no prior number under that operator set exists.

One run alone does not cover a flaky-high run. A mutant killed by
scheduling luck would lock a floor the suite does not reliably clear.
Gap 3 then runs that floor unattended every week, so an intermittent
red run becomes the steady state. A safety margin is cheaper than ten
extra sweeps.

Criterion: a package earns the margin when its own test suite runs
goroutines against the code under test. Phase 74's roster is evidence,
not the rule. A package on that roster with no goroutine in its tests
gets no margin, and a package off it with goroutines does.

Rule: eight swept packages meet the criterion, so each locks at the
measured rate rounded down, minus one point. The eight are
`workspace`, `subagent`, `agentloop`, `dispatch`, `a2aclient`,
`schema`, `ledger`, and `contextstate`. One point absorbs one flaky
mutant at no extra runtime.

`contextstate` is on the list on its own evidence. Its tests hold two
`go func` calls, the same order as `subagent` (3) and `workspace` (4).
It also gains 23 new sites, the second-largest gain, so a borderline
mutant is likely. Its floor becomes 99 instead of 100, which is cheaper
than an intermittently red weekly job.

`envelope` and `machine` stay out on the same criterion. Their test
suites hold zero `go func` calls and no concurrency marker. Both lock
at the measured rate rounded down, with no margin. `mcp` and
`secretpath` are not swept, so no margin applies to them.

### Floors are expected to drop, within a bound

New operators mean new survivors. A lower floor here is a more honest
measurement of the same test suite. It is not a gate weakening. It is
not a coverage regression. The production code did not change.

Three rules follow, and they are binding.

- Do not treat a lower measured rate as a build failure inside the
  bound below.
- Do not narrow the operator set back to keep an old number.
- Do not fix a survivor inside this phase. Record it and open follow-up
  work.

The bound stops the drop rule from becoming a loophole. Without it, a
package could fall from 96 to 40 and pass forever. Phase 74 held this
guard for `ledger`; this plan holds it for all ten swept packages.

Rule: if a new rate is more than 10 points below the package's stored
floor, the builder stops. The builder does not lock the new floor. The
builder records the full survivor list and escalates to the user, who
decides. A drop that large means a real coverage hole, not an operator
artifact.

The comparison uses the measured rate against the stored floor. It runs
before rounding down and before the one-point margin. Worked case:
`workspace` has a stored floor of 96 and measures 86.5. The gap is 9.5
points, which is inside the bound, so the builder locks 85 and does not
escalate.

The builder records every survivor in this plan, with file, offset, and
the token swap, exactly as phase 74 did. The builder also fills this
result table, so every drop stays auditable.

| Package | Old floor | Measured rate | Margin | New floor | Delta |
| --- | --- | --- | --- | --- | --- |
| workspace | 96 | 96.92 | -1 | 95 | -1 |
| subagent | 94 | 95.31 | -1 | 94 | 0 |
| agentloop | 98 | 91.20 | -1 | 90 | -8 |
| dispatch | 95 | 95.35 | -1 | 94 | -1 |
| a2aclient | 96 | 96.67 | -1 | 95 | -1 |
| schema | 83 | 86.21 | -1 | 85 | +2 |
| ledger | 91 | 91.43 | -1 | 90 | -1 |
| contextstate | 100 | 99.37 | -1 | 98 | -2 |
| envelope | 95 | 92.11 | 0 | 92 | -3 |
| machine | 100 | 100.00 | 0 | 100 | 0 |
| mcp | 100 | not swept | none | 100 | 0 |
| secretpath | 100 | not swept | none | 100 | 0 |

### Gap 2 result

Part A shipped. `OPERATOR_MUTATIONS` holds the four new entries, and the
`wanted` map holds the same four token texts. The measured site counts
match the table above exactly: 828 sites across the twelve packages, of
which 43 are `>`, 5 are `>=`, 39 are `+`, and 10 are `-`.

Part B first stopped on the bounded-drop rule. `schema` measured 72.41
against a stored floor of 83. The gap was 10.59 points, which is outside
the 10-point bound, so the builder locked no floor for any package.

All ten sweeps ran clean. `git status --short` showed no mutated source
file after any sweep, and `pgrep` found no orphan test binary. Total
wall time was about 29 minutes, with `ledger` at 24 of those minutes.
No package neared its wall-clock bound.

Part C closed the stop. The user reviewed the `schema` survivor list and
directed one exception to the "do not fix a survivor inside this phase"
rule: close the `schema` survivors with tests, then re-sweep `schema`
and lock all ten floors together. The exception covers `schema` only.
The other nine survivor lists stay open follow-up work.

The builder added tests to `schema/schema_test` and changed no
production code. Four of the eight survivors died. The other four are
equivalent mutants; the survivor table below records the reasoning for
each. The re-sweep measured 86.21, which is above the stored floor of
83, so the bounded-drop rule no longer applies to any package.

The ten floors then locked at the rates in the result table above. The
nine rates other than `schema` are the part B measurements; no package
was swept twice. `mcp.json` and `secretpath.json` stay byte-identical.

### Gap 2 survivors

`schema` held 8 survivors over 29 build-clean mutants, with 6
discarded. Four of the eight are new-operator sites. Part C killed four
and classified four as equivalent. The re-sweep confirms the same four
survivors and no other.

| File | Offset | Swap | Part C outcome |
| --- | --- | --- | --- |
| corrective.go | 3060 | `<=` to `<` | equivalent, see below |
| corrective.go | 3119 | `>` to `>=` | equivalent, see below |
| corrective.go | 3194 | `&&` to `\|\|` | killed |
| corrective.go | 3232 | dropped `continue` | killed |
| schema.go | 5271 | `>` to `>=` | equivalent, see below |
| schema.go | 5401 | `>` to `>=` | equivalent, see below |
| schema.go | 5443 | `+` to `-` | killed |
| schema.go | 5974 | `>` to `>=` | killed |

The four kills come from three tests.
`TestCorrectiveTruncationBoundaries` fixes where `MaxCorrectiveBytes`
falls inside a rendered message, and asserts the exact kept bytes.
`TestCompileContainerDepthBoundary` drives both branches of the depth
scan at each side of `MaxSchemaDepth`; its nested-array cases reach the
array branch's own depth arithmetic, which the earlier fixtures never
did. `TestCompileEmptyRefAdmits` pins an empty `$ref` value, which the
mutated length guard indexes out of range.

The four equivalent mutants split into two kinds.

- `schema.go` at 5271 and 5401 are `if d > max { max = d }`. The mutant
  runs the assignment when `d` equals `max`, which writes the value
  already there. No input can distinguish the two forms.
- `corrective.go` at 3060 and 3119 differ from the original only on an
  input `truncateRunes` never receives. Site 3060 needs a message of
  exactly `MaxCorrectiveBytes` bytes ending in invalid UTF-8. Site 3119
  needs the trim loop to empty its buffer. Every corrective message is
  valid UTF-8 and starts with an ASCII byte, so neither input exists.
  `truncateRunes` is unexported, and the kit runs `schema_test` alone,
  so no test can reach the helper directly.

Part C found no defect in `schema`'s production code. Every mutant that
died, died against the behaviour the current code already had.

`agentloop` holds 11 survivors, the largest count after `schema`.

| File | Offset | Swap |
| --- | --- | --- |
| compaction.go | 3300 | `>` to `>=` |
| compaction.go | 5292 | `+` to `-` |
| compaction.go | 5900 | `+` to `-` |
| run.go | 3048 | `>` to `>=` |
| run.go | 6455 | `>` to `>=` |
| run.go | 6676 | `+` to `-` |
| run.go | 6733 | `+` to `-` |
| run.go | 6789 | `+` to `-` |
| run.go | 6841 | `+` to `-` |
| toolcall.go | 1031 | `<` to `<=` |
| wire.go | 1389 | `>` to `>=` |

`envelope` holds 6 survivors: `message.go` at 6623 (`<` to `<=`), 6643,
7249, and 7280 (each `>` to `>=`), plus `sign.go` at 1223 and 1511
(each `||` to `&&`).

`ledger` holds 12 survivors: `claim.go` at 2349, 5027, and 7197 (each
`!=` to `==`), `complete.go` at 6301 (`<` to `<=`), `ledger.go` at 4103
(`!=` to `==`), 5909 (dropped `continue`), and 6984 (`||` to `&&`),
`store.go` at 5137, 5155, 5413, and 5441 (each `||` to `&&`), and
`store.go` at 6169 (dropped `continue`).

The remaining swept packages hold few survivors. `workspace` holds 2:
`confine.go` at 1695 (`==` to `!=`) and 1741 (`&&` to `||`).
`subagent` holds 3: `astool.go` at 3049 (`&&` to `||`),
`heartbeattool.go` at 1347 and `roomtool.go` at 2081 (each `!=` to
`==`). `dispatch` holds 2: `options.go` at 5750 (`==` to `!=`) and
`wire.go` at 660 (`!=` to `==`). `a2aclient` holds 1: `grpc.go` at 2345
(`||` to `&&`). `contextstate` holds 1: `checkpoint.go` at 960 (`-` to
`+`). `machine` holds none.

## Gap 3: no scheduled mutation-gate run

### Defect

`.github/workflows/ci.yml` runs `make verify` on push and pull request
to `main`. Nothing runs `make mutation-gate` on any cadence. Phase 74's
"manual cadence, weekly or before a release" has no enforcement.

### Fix

Add `.github/workflows/mutation.yml`. The workflow uses these settings.

- Triggers: `schedule` plus `workflow_dispatch`. No `push` trigger and
  no `pull_request` trigger. The sweep is far too long for either.
- Cron: `0 3 * * 1`, weekly on Monday at 03:00 UTC. Weekly matches the
  cadence phase 74 named. A nightly run would burn hours for a tree
  that often does not change.
- Job layout: one sequential job that runs `make mutation-gate`. Reason
  below.
- `timeout-minutes: 330`. That covers the three-hour worst case plus
  runner overhead. A hosted runner is slower than the measurement
  machine, and the sweep set grows as later phases add floors.
- Steps mirror `ci.yml`: `actions/checkout` and `actions/setup-go`,
  both pinned by SHA. Semgrep is not installed; `mutation-gate` does
  not need it.

Job layout options considered:

- **One sequential job** (chosen). Reason: `make mutation-gate` already
  loops over the denylist directory, and the final `git diff
  --exit-code` guard checks one clean tree once. A matrix would need
  either a new Makefile target or a per-package command in the
  workflow, which duplicates the loop.
- A per-package matrix. Rejected for now. It shortens wall time, but it
  splits the `git diff` guard across ten checkouts and adds a
  Makefile change this phase does not need. Revisit if the sequential
  job nears its timeout.

### Proof

A pre-merge `workflow_dispatch` run is impossible. GitHub exposes the
dispatch button only for a workflow already on the default branch. Gap
3 is therefore proven in two steps.

Before the merge: a YAML syntax check of `.github/workflows/mutation.yml`
with `python3 -c "import yaml,sys; yaml.safe_load(open(...))"`, plus one
local `make mutation-gate` pass. The local pass proves the command the
workflow calls.

After the merge to `main`: one manual `workflow_dispatch` run. The
builder records its result and its wall time in this plan. That run is
the first proof the scheduled path works end to end.

### Failure handling

AGENTS.md states that CI is informational: no branch protection rule
exists, so a failing check blocks nothing. The scheduled workflow keeps
that model. A failure shows as a red run on the Actions tab, and the
team treats it as work to triage. This plan adds no notification step
and no auto-filed issue. That stays open follow-up work, and the plan
names it here so the gap is recorded, not forgotten.

### Gap 3 result

Shipped. `.github/workflows/mutation.yml` matches the fix above:
`schedule` and `workflow_dispatch` triggers only, cron `0 3 * * 1`, one
sequential job, `timeout-minutes: 330`, and the same pinned
`actions/checkout` and `actions/setup-go` SHAs as `ci.yml`. No Semgrep
step.

The local proof step ran on the current tree: `python3 -c "import
yaml,sys; yaml.safe_load(open('.github/workflows/mutation.yml'))"`
passed, then a full `make mutation-gate` ran all twelve packages.
Every package passed at its locked floor, and the run ended clean
under `git diff --exit-code`. Wall time was over an hour, run outside
this tool's per-call time limit and completed under `nohup`.

Measured rates: a2aclient 96.67 (floor 95), agentloop 97.60 (floor
96), contextstate 99.37 (floor 98), dispatch 95.35 (floor 94), envelope
92.11 (floor 92), ledger 91.43 (floor 90), machine 100.00 (floor 100),
mcp 100.00 (floor 100), schema 86.21 (floor 85), secretpath 100.00
(floor 100), subagent 95.31 (floor 94), workspace 96.92 (floor 95).
Every rate matches its floor with margin intact. No orphaned test
binary survived the run, confirming gap 1 holds under a full twelve-
package sweep.

A pre-merge `workflow_dispatch` run stays impossible, per the Proof
section above. The first real scheduled or dispatched run happens
after this change merges to `main`, and its result belongs in a
follow-up note, not in this plan.

## API

No new exported Go symbols. `api/` locks are unaffected, because this
phase touches no Go source file.

New Python surface, inside `scripts/` only:

- `scripts/mutation_process.py`: `kill_group(pid)` and
  `run_test_group(argv, cwd, timeout, wait=None)`. The `wait` parameter
  exists for the probe; `run_mutant` never passes it.
- `scripts/check_mutation.py`: `_probe_process_group(tmp_path)`, wired
  into `run_probe`.
- `scripts/mutation_process_probe.py`: the three check functions
  `_probe_process_group` calls, plus their polling helpers. They live
  in a third file because inlining them put `check_mutation.py` at 546
  lines, over the 500-line limit this plan restates above.
- `scripts/mutation_tokenize.py`: four new entries in
  `OPERATOR_MUTATIONS` and four new keys in the `wanted` map.

Data changes: ten `scripts/mutation_denylist/*.json` files get a new
`floor` value from the gap 2 baseline sweep. `mcp.json` and
`secretpath.json` stay byte-identical.

## Tests

- Gap 1 is proven by `_probe_process_group`, described above, and run
  by `python3 scripts/check_mutation.py --probe` inside `make verify`.
  The probe asserts the grandchild process is dead on the timeout path,
  on the success path, and on the interrupt path. The interrupt check
  injects a raising wait, so it fails if the `finally` block goes away.
- Gap 2 is proven by the ten baseline sweeps. The builder captures each
  sweep's printed rate and survivor list, and writes both into this
  plan's result table.
- Gap 2 also reuses the existing probes unchanged. `_probe_planted_site`
  and `_probe_not_guard` must still pass with the wider operator set.
- Gap 3 is proven before the merge by a YAML syntax check and a local
  `make mutation-gate` pass. The first `workflow_dispatch` run happens
  after the merge to `main`, and the builder records it here.
- Gap 2 part C adds Go test cases to `schema/schema_test` under the
  user-directed exception recorded in "Gap 2 result". No other package
  gains a test, and no production Go file changes.

## Verification

- `python3 scripts/check_plan.py` — unaffected. This phase adds no
  top-level Go package, so no package needs a new plan file.
- `python3 scripts/check_deps.py` — unaffected. This phase adds no
  import edge, so `policy/layers.json` needs no new row.
- `python3 scripts/check_prose.py` — must pass on this plan.
- `make verify` — must pass after each of the three commits. The new
  process-group probe runs inside it.
- `make mutation-gate` — must pass at the end of gap 2. All twelve
  packages run, the ten swept ones at their new floors and the two
  skipped ones at their unchanged floors. The target must end clean
  under `git diff --exit-code`.
- `pgrep -fl '_test.test'` after every sweep in gap 2. The count must
  be zero. A non-zero count means the gap 1 fix is incomplete, and the
  builder stops and reports.
- `git status --short` after every sweep. It must show no mutated
  source file.
- Each gap 2 sweep stays inside its wall-clock bound: 60 minutes per
  package, and three hours for `ledger`. On a timeout, the builder
  keeps the stored floor and opens a follow-up.
- Any package whose measured rate falls more than 10 points below its
  stored floor stops the phase. The comparison uses the measured rate
  against the stored floor, before rounding and before the margin. The
  builder escalates to the user instead of locking that floor.
- `.github/workflows/mutation.yml` must parse as YAML before the merge.
  Its first `workflow_dispatch` run must finish green inside its
  timeout after the merge.
