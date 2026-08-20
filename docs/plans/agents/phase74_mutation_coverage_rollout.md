# Phase 74: mutation coverage rollout, step 2

Status: shipped. All eight floors, the `make mutation-gate` target,
and all eight package-plan pointers landed. `ledger`'s re-measurement
confirmed its floor of 91 and closed the last outstanding item. Builds
on the shipped
mutation kit (phase 54,
`scripts/check_mutation.py`) and its five measured floors: `envelope`
(95), `ledger` (91), `machine` (100), `contextstate` (100), and
`secretpath` (100). This phase adds floors for the next-highest-risk
packages and states the verify-wiring decision the phase 54 rollout
left open.

## Why this plan exists

Phase 54 shipped the kit and floored three packages. The module now
holds 40 top-level packages; only five carry a floor. The gap is
worst where the risk is highest: `agentloop`, `workspace`, and
`subagent`'s file tools are the newest model-reachable and
filesystem-touching surfaces, and none carries a floor. `ledger`'s
own floor predates its latest admission-closure fixes. This plan
closes that gap for the highest-risk packages first, not the
easiest ones.

## Goal

Measure and lock a mutation-kill floor for the packages most likely
to hide a real bug behind a passing test suite: filesystem
confinement, the model tool-calling loop, the file-tool wrappers
around confinement, the ledger admission path, and the four
network- or remote-transport-facing edges (`mcp`, `dispatch`,
`a2aclient`, `schema`). State whether `make verify` should run these
sweeps, and fix any kit gap the measurement work surfaces.

## Scope

Inside:

- Shipped: new `scripts/mutation_denylist/<pkg>.json` floor files for
  `workspace`, `subagent`, `agentloop`, `mcp`, `dispatch`,
  `a2aclient`, and `schema`.
- Shipped: a re-measured floor for `ledger`, reflecting the
  admission-closure fixes landed after its phase 54 floor was set (see
  item 4 under Priority order below). The re-measurement confirmed the
  floor of 91, so `scripts/mutation_denylist/ledger.json` stays
  unchanged.
- Shipped: a `make mutation-gate` target that loops over every package holding
  a `scripts/mutation_denylist/<pkg>.json` file and runs
  `python3 scripts/check_mutation.py --pkg <pkg>` for each, failing if
  any package drops below its own floor. The target ends with
  `git diff --exit-code`, failing loudly if any tracked file changed,
  so a killed or crashed sweep never leaves a live mutant merged. See
  Tool gap below for why this check is mandatory, not optional.
- A one-line addition to each touched package's own plan. Shipped for
  all eight plans. The plans
  are (`docs/plans/workspace.md`, `docs/plans/subagent.md`,
  `docs/plans/agentloop.md`, `docs/plans/mcp.md`,
  `docs/plans/dispatch.md`, `docs/plans/a2aclient.md`,
  `docs/plans/schema.md`, `docs/plans/ledger.md`) under Verification,
  naming the package's mutation floor and pointing at
  `make mutation-gate`.

Outside:

- Floors for packages with no floor today that this plan does not
  name: `contextplan`, `contextbudget`, `envfile`, `diff`, `spool`,
  `channel`, `scheduler`, `providerregistry`, `usage`, `trace`,
  `hooks`, `trigger`, `skills`, `discovery`, `heartbeat`, `identity`,
  `a2a`, `a2aack`, `room`, `flow`, `machine` (already floored),
  `agent`, `agentrun`, `taskrun`, `runconfig`, `tools`, `provider`,
  `events`, `memory`, `durablefence`, `e2e`. These stay open future
  work; a later phase floors them in risk order, the same way this
  phase follows phase 54.
- Widening the kit's operator set (see Tool gap below). A wider
  operator set changes every existing measured rate, including the
  five already-locked floors. That is a separate, reviewed change,
  not a byproduct of adding new floors.
- Fixing `run_mutant`'s process-group handling so a timed-out `go
  test` call kills its spawned test binary, not only its own driver
  process (see Tool gap below). That is a targeted code change to
  `scripts/check_mutation.py` itself and needs its own review.
- Wiring any mutation sweep into `make verify` or `make verify-fast`.
  See Verify wiring below for why, and the alternative this plan
  picks instead.
- Fixing the three surviving mutants in `schema/corrective.go` as
  part of locking `schema`'s floor. The builder locks the floor at
  the measured rate first; closing those three survivors is separate
  follow-up work a reviewer can scope on its own, not bundled into a
  floor-locking commit.

## Priority order and measured floors

Every rate below is a real, single, full sweep run with
`python3 scripts/check_mutation.py --pkg <pkg>` against the tree at
commit `6319ae6`, on 2026-08-20. Items 4, 6, and 7 were re-measured later,
during their own build; their entries carry the landed numbers, not
the 2026-08-20 ones. Items 1, 2, 3, 5, and 8 shipped at the rates
listed. The floor is the measured rate
rounded down to the nearest integer, matching the existing
`envelope`/`ledger`/`machine` convention of locking at the measured
value, not below it with an arbitrary buffer.

Workspace, subagent, agentloop, mcp, dispatch, a2aclient, and schema
each run goroutines against the package under test in their own test
suites, alongside ledger's stress and eviction suites. A mutant near
a race-sensitive boundary can flip killed or survived by scheduling
timing alone, not by a true coverage gap. Any future re-sweep of any
of these eight packages, including ledger's re-measurement below,
must run twice and lock a new floor only when both runs agree on
survivor count. If the two runs disagree, the re-sweeper locks the
lower of the two rates as the floor and records the flake in this
plan or a follow-up. The floors this plan locks today came from one
sweep each, per the measured-rate table below; this two-run rule
governs every re-sweep from this plan forward. One run is enough for a
first lock, because a first lock only needs a floor the suite clears
today, and a flaky-low first measurement locks conservatively low and
self-corrects on the next sweep. A re-sweep that raises a floor is the
dangerous case, because it can ratchet the gate above what the suite
reliably clears, so re-sweeps need two agreeing runs.

1. `workspace` — filesystem confinement. Five recent fixes closed
   real `os.Root` escape and symlink-alias holes
   (`f3adff3`, `78e1934`, `f8ec0fb`, `3bf7496`, `f2f6028`). An
   escaped mutant here means a sandbox breakout goes untested.
   Measured: 57 killed, 2 survived, rate 96.61%. Floor: **96**.
   Survivors: `confine.go:1695 == '==' -> '!='`,
   `confine.go:1741 && '&&' -> '||'`.
2. `subagent` — wraps `workspace` behind five model-reachable file
   tools (`WorkspaceReadTool`, `WorkspaceWriteTool`,
   `WorkspaceListTool`, `WorkspaceStatTool`, `DiffTool`) plus the
   ledger, room, and provider tool wrappers. Largest package by file
   count (23) and the broadest model-reachable surface in the
   module. Measured: 55 killed, 3 survived, rate 94.83%. Floor:
   **94**. Survivors: `astool.go:3049 && '&&' -> '||'`,
   `heartbeattool.go:1347 != '!=' -> '=='`,
   `roomtool.go:2081 != '!=' -> '=='`.
3. `agentloop` — the model tool-calling loop. Recently fixed a real
   bug in its own budget check (`d5a93a2`, `MaxTotalTokens` trusting
   a possibly-zero `TotalTokens`) and validates every model-chosen
   tool argument before `RunScoped` runs it. Measured: 67 killed, 1
   survived, rate 98.53%. Floor: **98**.
4. `ledger` — re-measure. Its phase 54 floor of 91 predates three
   later admission-closure fixes (`325a2c6`, `39e6062`, `1c6d411`)
   and the stress and eviction hardening in `a5f0630` and `2212a92`.
   A full sweep exceeded ten minutes and did not finish inside this
   plan's own measurement pass; the concurrency and stress suites
   make each mutant's `go test` run slow. The builder re-runs
   `python3 scripts/check_mutation.py --pkg ledger` in the background,
   under an explicit 3-hour wall-clock bound, and sets the new floor
   at the measured rate rounded down, never below the current 91. If
   the sweep does not finish inside the 3-hour bound, the builder
   keeps the floor at 91, records the timeout in this plan, and opens
   a follow-up. If the re-measured rate is below 91, the builder does
   not lock a new floor. The builder keeps 91 and opens a follow-up to
   investigate the regressed kill rate, since a drop implies weaker
   test coverage against the same production code, not a tooling
   artifact. After the sweep ends, by any path, the builder runs
   `pgrep -fl ledger_test.test` and kills every survivor process, and
   runs `git status --short` to confirm no leftover mutant on disk.
   Outcome, shipped: the re-sweep measured 91.67% (121 killed, 11
   survived, 0 discarded) in about 29 minutes of wall time, well
   inside the 3-hour bound. The rate rounds down to 91, which equals
   the stored floor, so `scripts/mutation_denylist/ledger.json` stays
   byte-identical. The two-run re-sweep rule did not require a second
   run here, because the floor does not move. That rule exists to stop
   a re-sweep from ratcheting a floor up above what the suite reliably
   clears; this re-sweep raises nothing, so one run is enough.
   Survivors: `claim.go:2349 != '!=' -> '=='`,
   `claim.go:5027 != '!=' -> '=='`,
   `complete.go:6301 < '<' -> '<='`,
   `ledger.go:4103 != '!=' -> '=='`,
   `ledger.go:5909 CONTINUE 'continue' -> ''`,
   `ledger.go:6984 || '||' -> '&&'`,
   `store.go:5137 || '||' -> '&&'`,
   `store.go:5155 || '||' -> '&&'`,
   `store.go:5413 || '||' -> '&&'`,
   `store.go:5441 || '||' -> '&&'`,
   `store.go:6169 CONTINUE 'continue' -> ''`. After the sweep,
   `git status --short` showed no leftover mutant, and every orphaned
   `ledger_test.test` process was killed. See the Tool gap section for
   the orphan count.
5. `mcp` — the MCP tool-calling client: a subprocess or a remote
   HTTP server maps its own tool set into this SDK's registry.
   Remote-supplied schema and content cross a trust boundary here.
   Measured: 17 killed, 0 survived, rate 100.00%. Floor: **100**.
6. `dispatch` — the NDJSON envelope endpoint. Its receive ladder
   runs Decode, VerifySignature, and Room.Accepts against
   caller-untrusted POST bodies before any handler runs. Shipped.
   The sweep found a real `isReplay` bug, fixed in `c823428`. The
   landed lock is 95, from a measured 95.24% (40 killed, 2 survived).
   See the mutation-floor entry in `docs/plans/dispatch.md`.
7. `a2aclient` — the a2a-go client adapter. Re-verifies the message
   signature after every remote hop over a real gRPC transport.
   Shipped. The landed lock is 96, from a measured 96.43% (27 killed,
   1 survived). See the addendum in `docs/plans/a2aclient.md`.
8. `schema` — JSON Schema compile, validate, and corrective-message
   admission control over untrusted payload bytes. Measured: 15
   killed, 3 survived, rate 83.33%. Floor: **83**. This is the
   weakest measured rate in this batch; the three survivors sit in
   `corrective.go`, the message-shaping path a caller reads after a
   validation failure. Survivors:
   `corrective.go:3060 <= '<=' -> '<'`,
   `corrective.go:3194 && '&&' -> '||'`,
   `corrective.go:3232 CONTINUE 'continue' -> ''`. Locking a floor
   here catches regression today; closing the three survivors is
   separate follow-up work, named in Scope above, not required
   before this floor lands.

Already covered, no action: `envelope` (95), `machine` (100),
`contextstate` (100), `secretpath` (100, measured in the same commit
as its own fix, `a06b296`).

## Verify wiring

A full sweep is too slow for `make verify` or `make verify-fast`.
Measured wall time on this run: `mcp` 1m40s, `dispatch` 38s,
`a2aclient` 4m17s, `ledger` over 10 minutes and still running when
this plan's measurement pass gave up. Eight packages in this batch
alone would add many minutes to every `make verify` call, and the
pre-commit hook already runs `verify-fast` on every commit. A slow
gate on every commit trains the team to skip it, which is worse than
no gate.

This plan keeps `--probe` as the only mutation step inside
`make verify`, unchanged from phase 54. It adds `make mutation-gate`
as a new, separate target: a full sweep of every floored package,
run on demand. Options considered:

- **A new `make mutation-gate` target, manual cadence for now**
  (chosen). The builder or a reviewer runs it before a change that
  touches a floored package's source, and the team runs it on a
  standing cadence: weekly, or before a release.
  Reason: the runtime cost measured above rules out an every-commit
  gate, and a named target lets a CI workflow call the sweep
  directly with no rework.
- A step in the existing CI job. Rejected. CI exists today in
  `.github/workflows/ci.yml`, landed in commit `6bfa6a2`, and it runs
  `make verify` on every push and pull request to `main`. A sweep near
  30 minutes per package does not belong in a per-push job. `make
  mutation-gate` stays callable from a future scheduled workflow, with
  no change to the target.
- Wiring into `make verify`. Rejected: the runtime cost measured
  above turns every `make verify` call, including ones that touch no
  floored package, into a multi-minute wait. AGENTS.md requires
  `make verify` before reporting done on every task; this would slow
  every task, not just ones touching floored packages.

## Tool gap

`check_mutation.py`'s `test_target` already resolves a package with
its own `<pkg>_test` subdirectory correctly: `ledger`'s and every
other floored package's sweep already tests against
`./<pkg>/<pkg>_test` when that directory exists, confirmed by
`_probe_test_target` and by this plan's own `ledger` and `subagent`
measurement runs, which exercised each package's real test
directory. No gap there.

A second real gap, found while measuring `ledger` for this plan: an
external `timeout` wrapper around a long sweep can still leave a
mutated file on disk. The `ledger` sweep ran past ten minutes under
`timeout 590 python3 scripts/check_mutation.py --pkg ledger`; the
wrapper's SIGTERM ended the process, and `ledger/store.go` was left
holding a live mutant (an `!=` flipped to `==`) instead of its
original bytes. `sweep`'s own `finally` block restores every path in
its `originals` dict on a normal `KeyboardInterrupt`, and the module
docstring explains the SIGTERM-to-`KeyboardInterrupt` remap exists
for exactly this case; this run shows that remap does not always win
the race against an external hard kill. The builder restored the
file with `git checkout -- ledger/store.go` before this plan landed.
Recorded here, not fixed here: the fix (a `trap`-equivalent restore
path, or a pre-sweep git-clean check plus a mandatory post-sweep
`git diff --exit-code` in `make mutation-gate` itself) is its own
small, reviewable change. Until it lands, `make mutation-gate`'s own
definition must run `git diff --exit-code` after every sweep and
fail loudly if any tracked file changed, so a killed run never
merges a live mutant silently. The pre-existing `make mutation
PKG=<pkg>` target carries the same risk and gets no such automatic
check. Until the underlying SIGTERM and timeout bugs in
`check_mutation.py` are fixed by a separate change, every caller of
`make mutation PKG=<pkg>` must run `git status --short` after the
sweep finishes and restore any changed tracked file before
continuing. This is a standing caution for every future caller, not
a one-time step for this plan's own build.

A third real gap, also found while measuring `ledger`: `run_mutant`'s
`go test` call passes `timeout=TEST_TIMEOUT_SECONDS` (60) to
`subprocess.run`. On expiry, `subprocess.run` kills only the direct
child, the `go test` driver process. `go test` itself has already
spawned a separate compiled test binary
(`<tmp>/b001/<pkg>_test.test`); killing the driver does not kill that
binary, which keeps running, CPU-bound, until it hits its own
internal `-test.timeout` (ten minutes for `ledger`, matching its
stress suite). Measuring `ledger` for this plan left eleven orphaned
`ledger_test.test` processes running in the background, each pinned
at 100% CPU, well after the mutation sweep that spawned them had
moved on to later mutants or had itself been killed. The builder
found and force-killed all eleven before this plan's measurement
data could be trusted, and confirmed `git status` was clean
afterward. This compounds the runtime-cost finding above: a
long-running sweep can leave a growing pile of runaway test binaries
that starve every later mutant's own `go test` call of CPU, making
the sweep progressively slower the longer it runs. The fix is to run
`go test` with its own process group (a negative PID) and kill the
group, not the single PID, on timeout; that is a small, targeted
change to `run_mutant`, but it changes the kit's process-management
code, so it is its own reviewed change, not folded into this plan's
denylist and Makefile edits.

The `ledger` re-sweep in item 4 repeated the same symptom. It left six
orphaned `ledger_test.test` processes running after it exited, plus
more reaped during the run. The builder killed all six. This is a
second, independent confirmation of the process-group gap above. The
fix stays out of scope for this phase.

A fourth real gap: `OPERATOR_MUTATIONS` in `scripts/mutation_tokenize.py`
covers `==`, `!=`, `<`, `<=`, `&&`, `||`, a dropped `!`, and a
dropped `continue`. It has no `>`/`>=` pair and no arithmetic
operator (`+`, `-`, `*`, `/`) mutation. A boundary bug written with
`>` instead of `>=`, for example, is invisible to every floor this
kit measures today, including the five already locked. This plan
does not fix it: widening the operator set changes every existing
measured rate, so it needs its own plan and its own review pass over
the five already-locked floors, not a silent side effect of adding
new ones. Recorded here as the next kit change to plan, after this
phase locks its own floors.

## API

No new exported Go symbols. `scripts/mutation_denylist/*.json` gains
seven new files. `ledger.json` was re-measured, and its stored value
stays unchanged at 91. A denylist file is not a Go API surface, so
`api/` locks are unaffected.

## Tests

- Each new denylist file's floor is proven by the sweep that
  produced it: `python3 scripts/check_mutation.py --pkg <pkg>` run
  once, output captured, floor set from the printed rate.
- `make mutation-gate`, once added, is itself exercised by running it
  against the current tree: every floored package must pass at its
  own stored floor the day the target lands.
- No new Go test file. This phase changes tooling and JSON data
  files, not package behavior.

## Verification

- `python3 scripts/check_plan.py` — unaffected; this phase touches no
  new top-level Go package.
- `python3 scripts/check_deps.py` — unaffected; no new import edge.
- `make verify` — must still pass, unchanged in this phase, since
  `--probe` is the only mutation step it runs.
- `make mutation-gate` — new target; must pass against every floored
  package the day it lands, including the seven new floors and the
  re-measured `ledger` floor, and must end clean under
  `git diff --exit-code`.
- `git status --short` after any manual sweep run during this phase's
  build, confirming no floored package's source carries a leftover
  mutant before the builder commits.
- `pgrep -fl ledger_test.test` after any manual sweep run, killing
  every survivor process before the builder continues.
- The `ledger` re-sweep stays inside a 3-hour wall-clock bound. On
  timeout, the builder keeps the floor at 91, records the timeout
  here, and opens a follow-up. Done: the re-sweep took about 29
  minutes and kept the floor at 91. See item 4.
- Each touched package's plan gains its one-line Verification pointer
  to its floor and to `make mutation-gate`, per Scope above.
