# Plan: mutation kit internal-test blind spot

## Goal

Make the mutation gate run a package's own internal test files. Today
it runs only the external test directory when one exists. The gate
therefore scores mutants as surviving even when a real test kills them.
Re-measure every floor upward after the fix.

## The defect, reproduced

`test_target` at `scripts/check_mutation.py:127-133` returns
`./<pkg>/<pkg>_test` when that directory exists and holds any `.go`
file. Otherwise it returns `./<pkg>`. It never returns both.

So for a package with an external test directory, every white-box test
in the package's own directory is never executed under mutation.

The planner reproduced the confirmed instance. `ledger/store_eviction.go`
holds `rotations >= evictScanBudget` at byte offset 1055.
`ledger/store_eviction_test.go` is `package ledger` and holds
`TestEvictionScanBudgetStops`.

```text
mutant: store_eviction.go:1055  '>=' -> '>'
  go test ./ledger                -> rc=1   (killed)
  go test ./ledger/ledger_test    -> rc=0   (scored survived)
  go test ./ledger ./ledger/ledger_test -> rc=1   (killed)
```

The gate chose the middle line. The sweep reported
`SURVIVED: store_eviction.go:1055`. The gate under-reports.

`go test -list '.*' ./ledger` lists nine test functions under the
default build. None of them ever ran under mutation.

## Affected packages, counted

Thirty-nine packages hold an external test directory. Ten of those also
hold at least one test file in the package directory. Those ten are the
affected set. The coordinator reported nine; the measured number is ten.

```text
package      internal test files
agent        run_budget_internal_test.go
agentloop    wire_fuzz_test.go
agentrun     wire_internal_test.go
diff         hunk_bench_test.go
dispatch     defaults_internal_test.go ladder_internal_test.go replay_internal_test.go
flow         immutability_test.go wave_select_internal_test.go
ledger       store_eviction_test.go plus six ledger_sqlite tag-gated files
runconfig    steptool_internal_test.go
scheduler    run_internal_test.go
workspace    confine_internal_test.go
```

`diff` holds only a benchmark file. `go test ./diff` reports "no tests
to run". It cannot change any score.

`ledger`'s six `sqlite_store_*_test.go` files carry `//go:build
ledger_sqlite`. They never compile into the default build. Only
`store_eviction_test.go` runs.

Four of the ten hold a stored floor: `agentloop`, `dispatch`, `ledger`,
and `workspace`. Only those four floors can move.

## Scope

Inside:

- `test_target` becomes `test_targets` and returns a list.
- `run_mutant` passes every target to one `go test` invocation.
- `run_mutant` gains a `root` parameter so a probe can drive a
  throwaway module.
- A new probe module, `scripts/mutation_target_probe.py`.
- `_probe_test_target` at `scripts/check_mutation.py:337` is replaced by
  `_probe_internal_test_target`. It calls the removed `test_target` at
  lines 344 and 350. Delete the function. Remove its `run_probe` call at
  `scripts/check_mutation.py:397`. A leftover call raises `NameError` and
  fails `Makefile:56`.
- A fresh measured floor for every package in
  `scripts/mutation_denylist/`, raised only.
- The coverage-block finding, recorded below as a separate change.

Outside:

- Any change to `OPERATOR_MUTATIONS`, to `classify`, or to the
  kill-rate formula.
- Any new denylist entry. Any lowered floor. Any widened exclusion.
- Fixing the twelve real `ledger` survivors. They are follow-up work.
- The diff-scoped mode and the `make mutation` tree check. Both move to
  the Follow-up section.
- The Makefile coverage block. See the Coverage finding.
- Any Go package, Go source file, or `api/` lock.
- `policy/layers.json`. This change adds no Go package, so it adds no
  import edge. The file stays untouched.

## API

### `scripts/check_mutation.py`

```text
test_targets(pkg_dir: Path, pkg: str) -> list[str]
run_mutant(site, original, pkg, pkg_dir, root=ROOT) -> str
```

`test_targets` rules:

- Return `["./<pkg>"]` when no external test directory exists.
- Return `["./<pkg>", "./<pkg>/<pkg>_test"]` when one exists and holds
  a `.go` file.
- The package's own directory always comes first. Order is fixed, so
  output stays deterministic.
- Do not check whether the package directory holds a test file. `go
  test` on a directory with none exits 0 and prints "no test files".
  A branch there would add a case with no benefit.

`run_mutant` rules:

- Build one command: `["go", "test", *targets]`.
- One `go test` invocation covers both targets. A second invocation
  would double the process cost and need a combine rule.
- `go test` exits non-zero when any named package fails. That gives the
  "killed if either target fails" rule for free.
- `classify` stays unchanged. It still reads "pass", "fail", or
  "timeout" from `run_test_group`.
- The single 60-second timeout now covers both targets. That stays
  correct: a timeout still counts as killed.
- The `root` parameter defaults to `ROOT`. It sets the `cwd` for `go
  build` and `go test`. Only the probe passes a different value.

### Concurrency: the two test binaries run in parallel

`go test ./a ./b` runs the two test binaries concurrently, confirmed by
measurement. A package's internal and external tests now share one wall
clock. A future reader must not assume sequential execution.

The reviewer ran all ten affected packages in the two-target form.
Every one returned exit code 0. No collision exists today.

A collision would appear as a mutant scored killed for the wrong
reason. Two test binaries writing one fixed temp path is the likely
shape. Report any such failure rather than serializing the run.

### Mechanics chosen, and the one rejected

One invocation with two package paths is the chosen form. The rejected
form is two invocations whose results combine.

Two invocations would need their own combine rule, a second timeout
budget, and a second process group to reap. One invocation needs none
of that. The exit code already carries the answer.

### File-size budget

`check_mutation.py` is 446 lines. The change adds roughly twelve lines
there, and deletes `_probe_test_target`.

The 500-line limit on this file is convention, not a gate.
`scripts/check_structure.py:37` walks `*.go` only. AGENTS.md still sets
the limit, so honor it.

The new probe lives in `scripts/mutation_target_probe.py`. The reason is
the precedent phase 75 set with `mutation_process.py`, not gate
pressure.

The probe must build its site through `sites_for_file`, not
`collect_sites`. `collect_sites` and `DENYLIST_DIR` are bound to `ROOT`,
so neither can address a temp module.

## Tests

### The probe that is the whole point

Add `_probe_internal_test_target(tmp_path)` to `run_probe` in
`check_mutation.py`. It delegates to `scripts/mutation_target_probe.py`.

The probe builds a throwaway Go module in the temp directory:

```text
go.mod                              module probemod
probepkg/probepkg.go                F(n) is n >= 8; G(n) is n < 4
probepkg/probepkg_test/ext_test.go  asserts G(4) is false
probepkg/internal_test.go           package probepkg; asserts F(8) is true
```

`internal_test.go` fails when `>=` becomes `>`. It ignores `G`.
`ext_test.go` fails when `<` becomes `<=`. It ignores `F`.

Each test file therefore owns one site that only it can kill. That
mirrors the confirmed `ledger` instance and gives both directions of
the "killed if either target fails" rule.

Probe checks:

1. `run_mutant` on the `>=` site returns `KILLED`, with `root` set to
   the temp module. Only `internal_test.go` kills it. This proves the
   fix.
2. `go test ./probepkg/probepkg_test` alone passes under the same
   mutant. This proves the probe fixture is honest, and that the old
   single-target behavior scored the mutant as surviving.
3. `test_targets` returns both paths, package directory first, when the
   external directory exists.
4. `test_targets` returns one path when no external directory exists.
5. `test_targets` returns one path when the external directory exists
   but holds no `.go` file.
6. A two-package `go test` that hangs returns "timeout" and leaves no
   live process in the group. See the timeout check below.
7. `run_mutant` on the `<` site returns `KILLED`. Only `ext_test.go`
   kills it. This proves the other direction of the rule.
8. A target list naming a package with no test file does not turn a
   passing run into a failure. Delete `internal_test.go`, so `probepkg`
   holds no test file. Then assert two results with the same fixture.
   `run_mutant` on the `<` site still returns `KILLED`, because
   `ext_test.go` still fails. `run_mutant` on the `>=` site now returns
   `SURVIVED`, because no test kills it and the empty package must not
   fabricate a failure. Together these pin the invariant the
   `test_targets` rules rely on to skip a branch.

Check 8 destroys the fixture that checks 1 through 7 need. Give check 8
its own copied fixture directory. A copy removes the ordering
dependency. If the builder keeps one shared fixture, then check 8 must
run last, and a comment must say so.

### The timeout check, with two targets

Check 6 is the regression this work exists to prevent. Nothing in the
tree today asserts that a two-package `go test` leaves no orphan on
timeout.

`_probe_process_group` at `scripts/check_mutation.py:373` drives a
single-argv command. It cannot cover the two-target shape.

Add the check to `scripts/mutation_target_probe.py`. Reuse the shape of
`probe_group_outcomes` in `scripts/mutation_process_probe.py:53`.

- Add a sleeping test to each target of the probe module. Each test
  writes its own `os.Getpid()` to its own file, then sleeps past the
  timeout. Use a real `go test` fixture, never a `/bin/sh` fixture. A
  shell fixture simulates the process group but not the `go test`
  driver shape this subsection exists to pin.
- Pre-build the probe module before the timing check. Run `go test
  -run=NONE -count=1` over both targets, or `go build`. Compile time
  must sit outside the measured window.
- Call `run_test_group` with a four-element argv carrying two targets:
  `["go", "test", "./probepkg", "./probepkg/probepkg_test"]`.
- Give this check its own timeout constant in
  `scripts/mutation_target_probe.py`. Do not reuse
  `PROBE_TIMEOUT_SECONDS`.
- Assert the return value is "timeout".
- Assert every recorded PID is dead within the deadline. Reuse
  `_await_pid_file` and `_is_dead_within_deadline`. Do not lower
  `PROBE_DEADLINE_SECONDS`.

### Why check 6 needs its own timeout

`PROBE_TIMEOUT_SECONDS` is 0.3 at `scripts/mutation_process_probe.py:13`.
That value is too small for a `go test` fixture.

Measured time for a two-target `go test` to reach test-body execution:

```text
warm build cache:   first PID written at 0.09 s and 0.11 s
empty build cache:  first PID written at 1.93 s
```

At 0.3 seconds with an empty cache the failure chain is exact.
`run_test_group` kills the group before either binary starts. No PID
file is written. `_await_pid_file` polls `PROBE_DEADLINE_SECONDS`, which
is 2.0, and returns None. `_is_dead_within_deadline(None)` returns False
at `scripts/mutation_process_probe.py:41-42`. The check then reports
"the grandchild process survived".

That failure is worse than a plain failure. It passes warm on a
builder's machine at 0.11 seconds. It fails cold on a runner at 1.93
seconds. A probe green locally and red in
`.github/workflows/ci.yml` is the one outcome a regression probe must
not have.

Set the new constant to at least 5.0 seconds. It must exceed the 1.93
second cold measurement with margin. The pre-build step removes the
cold case, so the margin is large in practice.

The fixture hangs on purpose, so `run_test_group` always waits the full
timeout. Check 6 therefore adds its timeout value to every `make
verify` run. Five seconds is the accepted cost.

The reviewer proved the two-target timeout path works today. This check
stops a future refactor breaking it silently.

The probe runs `go build` and `go test` in a temp module. The existing
probe suite already shells out to `go run` for the tokenizer, so the
`go` tool is an accepted probe dependency. Measured cost of one such
invocation is under one second.

No conformance vector changes. This change touches no wire format and
no message semantics.

## Verification

### Commands that must pass

- `make verify`. It runs `check_mutation.py --probe`.
- `make mutation-gate`. Every package must meet its raised floor.
- `python3 scripts/check_plan.py`.
- `python3 scripts/check_deps.py`.
- `python3 scripts/check_prose.py`.
- `python3 scripts/check_labels.py`.
- `python3 scripts/check_names.py`.
- `python3 scripts/check_structure.py`.

### Floors: measured before, predicted after

The planner measured three of the four affected packages. The method
had two steps. First, run the current sweep and collect its survivors.
Second, plant each survivor and run `go test ./<pkg>` alone. A survivor
that fails that run is a mutant the blind spot hid.

```text
package    sites  before          hidden  after (predicted)  floor
ledger     142    129/142 90.85%  1 of 13  130/142 91.55%    90 -> 91
workspace   67     63/65 96.92%   2 of 2    65/65 100.00%    95 -> 100
dispatch    43     41/43 95.35%   2 of 2    43/43 100.00%    94 -> 100
agentloop  166    156/162 96.30%  0 of 6   156/162 96.30%    96 -> 96
```

The rate denominator excludes discarded mutants. `agentloop` discarded
4 of its 166 sites, and `workspace` discarded 2 of 67.

Three of the four affected packages gain. In `workspace` and
`dispatch` the blind spot hid every remaining survivor.

The `ledger` survivor the blind spot hid is
`store_eviction.go:1055 '>=' -> '>'`, exactly as reported.

`agentloop` gains nothing. Its one internal file is
`wire_fuzz_test.go`, which covers `wire.go`. All six survivors sit in
`compaction.go` and `run.go`. Its floor stays at 96.

The measured floor change is therefore three packages, not four.

The builder must confirm each predicted rate with a real sweep after
the fix. Set each floor to the measured rate, truncated to an integer.

### Floors: measured after the fix

The builder swept the four affected floored packages after the fix.
Each measured rate matched the prediction.

```text
package    sites  after            floor
workspace   67     65/65 100.00%   95 -> 100
dispatch    43     43/43 100.00%   94 -> 100
ledger     142    130/142 91.55%   90 -> 91
agentloop  166    156/162 96.30%   96 -> 96
```

`ledger/store_eviction.go:1055` is now scored killed. It no longer
appears in the survivor list.

The twelve `ledger` survivors and the six `agentloop` survivors match
the Follow-up lists exactly. No new survivor appeared.

The eight unaffected floored packages were not re-swept. That is a
deviation from the section below, directed to keep the session inside
its time budget. Their floors are unchanged, so
`make mutation-gate` still covers them.

### The eight unaffected floored packages

`a2aclient`, `contextstate`, `envelope`, `machine`, `mcp`, `schema`,
`secretpath`, and `subagent` hold no internal test file beside an
external directory. Their rates must not move.

Re-measure them anyway. A moved rate there means the change did
something unintended. Stop and report it.

### Never lower a floor

If any package's rate falls after the fix, stop and report it. A fall
means the change broke something rather than revealed something. Do not
lower the floor to make the gate pass.

### The cost of a floor of 100, accepted

`workspace` and `dispatch` reach 100. Keep that value. Three reasons
support it.

The repo already holds three floors of 100: `machine`, `mcp`, and
`secretpath`. It is house convention. The rule "floor equals the
measured rate, truncated" is mechanical. Deviating to 97 for headroom
would add a discretionary rule with no stated bound. Flakiness is
asymmetric in the safe direction, because a flaky test failure scores
the mutant as killed, so noise inflates the rate rather than deflating
it.

The cost is real and stated here. The first future mutant that survives
in `workspace` or `dispatch` fails the weekly job. Two remedies exist.
Write a test that kills it. Or add a denylist entry, which trips `TT12`
and needs an `Allow-Gate-Change` trailer.

Never lower the floor as the third remedy.

### Sweep time and staging

Measured per-mutant cost, from ten-mutant samples:

```text
package       s/mutant  sites  full sweep
agentloop        25.0     166  measured, about 75 minutes
ledger           10.4     142  measured, 1472 s (24m32s)
mcp               8.6      17  about 2 minutes
machine           6.2      52  about 5 minutes
subagent          2.25     68  about 3 minutes
dispatch          1.33     43  96 s measured
a2aclient         0.45     30  under 1 minute
contextstate      0.27    160  about 1 minute
workspace         0.26     67  13 s measured
envelope          0.25     77  about 1 minute
secretpath        0.15     14  under 1 minute
schema            0.10     35  under 1 minute
```

All twelve floored packages appear above. They hold 871 sites. The
estimated total is about 110 minutes, dominated by `agentloop`. That
fits one session. No staging is needed.

The five smallest rows were measured in the two-target form. They add
about 10 minutes. The estimate survives.

### Stop rule on sweep time

`.github/workflows/mutation.yml:13` sets `timeout-minutes: 330`. The
estimate leaves wide headroom.

If the full `make mutation-gate` exceeds 330 minutes, stop and report
it. Do not raise the workflow timeout. A sweep that outgrows its budget
is a design question, not a configuration one.

The marginal cost of the extra target is small. `go test ./<pkg>` on
each affected package measured under 0.2 seconds. Sweep time is
dominated by mutants that reach the 60-second timeout.

Run the full `make mutation-gate` once, after the fix, and record each
measured rate in this plan.

### Coverage finding: the same blind spot, deferred

The Makefile coverage block holds the same shape. It picks the external
test directory when one exists, and the package directory otherwise. So
coverage is measured from external tests alone for those ten packages.

Measured for `ledger`, with `-coverpkg=./ledger`:

```text
./ledger/ledger_test only    98.0%   277 blocks, 269 covered
./ledger only                 9.9%
both packages, one profile          554 blocks, 291 covered
```

Two facts follow.

First, the current block under-counts. Under-counting is the safe
direction: it makes the 85 floor harder to meet, never easier. No bad
code passes because of it.

Second, the naive fix corrupts the profile. `go test` with two package
paths and one `-coverprofile` writes both profiles end to end. The 554
blocks are the 277 real blocks listed twice. The Makefile's awk
aggregation would double-count them.

A correct fix needs a merge step that takes the highest count per
block. That is a new mechanism with its own probe.

Decision: keep the coverage fix out of this change. Two reasons. It
needs a merge mechanism this change does not, and mixing two
measurement changes in one commit makes a moved number impossible to
attribute. Record it in the Follow-up section.

### Test-tampering rules

`scripts/check_test_tampering.py` runs inside `make verify` and reads
the staged snapshot.

- `TT12` does not fire. The change adds no denylist entry and no
  denylist file.
- `TT13` fires only on a lowered floor or a removed gate invocation.
  Every floor here rises. No invocation moves. It stays silent.
- `TT14` does not fire. No checker source is deleted, no hook
  invocation removed, no finding-ID literal dropped.
- `TT11` fires when one commit holds a gate-infrastructure file beside
  any other file. `scripts/` is infrastructure. `docs/` is not.

Do not request an override trailer. Split the work into two commits.

1. Commit one stages `scripts/check_mutation.py`,
   `scripts/mutation_target_probe.py`, and every changed
   `scripts/mutation_denylist/*.json`. Every staged path is
   infrastructure, so `TT11` sees no unpaired file.
2. Commit two stages `docs/plans/mutation.md` and
   `docs/architecture.md`. No staged path is infrastructure.

If `TT11` still fires after this split, stop and report it. That is a
finding about the rule, not a reason to waive it.

### Doc edits

- `docs/architecture.md`: the mutation paragraph says the kit "runs
  that package's tests per mutant". State that it runs both the package
  and its external test directory. Add
  `.github/workflows/mutation.yml`, its weekly cron, and its manual
  dispatch trigger, which the CI paragraph omits.
- `docs/plans/gates.md` line 72 records a shipped plan's decision to
  treat mutation testing as future work. Do not rewrite the record.
  Append one status note: "Status: the mutation kit shipped. See
  `scripts/check_mutation.py` and `scripts/mutation_denylist/`."

`AGENTS.md` needs no edit. Lines 181 and 182 already name
`scripts/check_mutation.py` and its stored floors.

`docs/README.md` needs no edit. It names `docs/plans/` as a directory
and lists no individual plan file.

## Follow-up work

Recorded here, planned separately. Do not build any of it in this
change.

### The twelve real `ledger` survivors

These twelve survive even when the internal tests run. They are genuine
test gaps.

```text
claim.go:2455          != -> ==
claim.go:5133          != -> ==
complete.go:6301       <  -> <=
ledger.go:4294         != -> ==
ledger.go:6100         dropped continue
ledger.go:7175         || -> &&
store.go:5882          || -> &&
store.go:5900          || -> &&
store.go:5972          <  -> <=
store.go:6162          || -> &&
store.go:6190          || -> &&
store_eviction.go:1123 >  -> >=
```

### The six real `agentloop` survivors

These six survive with the internal test file running. They are genuine
test gaps.

```text
compaction.go:5292  + -> -
compaction.go:5900  + -> -
run.go:10648        && -> ||
run.go:10661        == -> !=
run.go:11124        <= -> <
run.go:15203        >  -> >=
```

### The coverage block

Give the Makefile coverage block both targets, with a merge step that
takes the highest count per block. Needs its own probe.

### Diff-scoped mode

Add `--range <rev-range>` so a reviewer sweeps only the lines a change
touched. Google's industrial study reports that a small per-line cap
holds reviewer attention without losing signal. Suggested caps: two
mutants per line, forty per change.

The mode must be additive. A diff-scoped run must never report a floor
as met. Use a distinct output token, `diff-kill=`, and reserve `rate=`
for the full sweep. Exit 0 whatever the rate.

The weekly `mutation.yml` job already covers unattended full sweeps, so
this mode is a reviewer convenience, not a gate.

### Tree check on `make mutation`

`make mutation-gate` ends with `git diff --exit-code`. `make mutation
PKG=<pkg>` does not. Add the same line, so a killed sweep that leaves a
mutant on disk fails loudly.

### Operator set

Widen `OPERATOR_MUTATIONS` beyond its current twelve mutations. Every
stored floor needs a fresh baseline sweep in the same change.

## Prior state, already shipped

Do not re-do these. Each was verified at `main` in this worktree.

- The process-group leak is fixed. Commit `c200bd6` added
  `scripts/mutation_process.py`. `run_test_group` starts `go test` with
  `start_new_session=True`. It calls `kill_group` on the timeout path,
  the success path, and the interrupt path. `_probe_process_group`
  already proves all three.
- The `ledger` sweep completes. It ran in 1472 seconds and left zero
  orphan test binaries. Progress goes to stderr, one line per mutant. A
  reader watching stdout alone sees nothing until the end.
- `.github/workflows/mutation.yml` runs `make mutation-gate` on a
  weekly cron and on manual dispatch, with a 330-minute timeout. Do not
  add the full sweep to `make verify` or `make verify-fast`.
