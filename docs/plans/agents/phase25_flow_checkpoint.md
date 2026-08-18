# Phase 25: flow checkpoint pause and resume

Status: ready to build. Builds on the current runner loop in
`flow/runner.go`, after the outcome refactor (`Run` returns `Report`,
completion tracked in `outcomes map[string]Outcome`, resolved through
`markOutcome` and `runSingletonAndMark`). Independent of phases 21
through 24; checkpointing needs no admission, route, or machine-accessor
change. See `docs/plans/agents/PHASES.md`.

## Goal

Let a caller pause a run and resume it later from the last completed
step. `Run` reports its progress through a checkpoint hook. `Resume`
restarts a graph walk from a stored checkpoint. The caller owns
storage; `flow` owns only the in-memory state and the hook.

## Scope

Inside: the `Checkpoint` type, its `Encode` and `Decode` pair, the
`onCheckpoint` hook added to `Run`, the new `Resume` function, and the
context-cancellation pause check.

The `Run` signature change touches every existing call site. Each one
gains a trailing `nil` or `onCheckpoint` argument in this change:

- `flow/runner.go:195` — `runChild`'s inner `Run` call. Today it reads
  `report, err := Run(ctx, child, m, machine.InOut{}, confirm, nil)`,
  a six-argument call whose trailing `nil` is the existing `bus`
  argument; a chained step's child workflow already runs with a nil
  bus. This change appends a second trailing `nil` and moves the call
  to seven arguments:
  `report, err := Run(ctx, child, m, machine.InOut{}, confirm, nil, nil)`.
  The new seventh argument is `onCheckpoint`. A chained step's child
  workflow is not independently resumable; only the parent step's
  completion is captured.
- All 73 `flow.Run(...)` call sites across 14 files in
  `flow/flow_test/`: `chain_integration_test.go` (3),
  `outcomes_bench_test.go` (2), `chain_test.go` (12),
  `chain_bench_test.go` (5), `chain_new_test.go` (1), `emit_test.go`
  (6), `run_bench_test.go` (2), `outcomes_test.go` (9),
  `panel_bench_test.go` (2), `outcomes_integration_test.go` (2),
  `panel_integration_test.go` (4), `panel_test.go` (9), `run_test.go`
  (11), `run_integration_test.go` (5). This count and file list is
  current as of the outcome refactor (commit 36d2c67); the builder
  re-greps `flow\.Run\(` in `flow/flow_test/` before editing, since a
  concurrent change could add or remove a site.
- The documented example at `docs/packages/flow.md:216`:
  `report, err := flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm, bus)`
  gains the trailing argument and moves to
  `report, err := flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm, bus, nil)`.
- `agent/run.go:115` — `Agent.Run`'s own call to `flow.Run`, in the
  downstream `agent` package. Today it reads
  `report, err := flow.Run(ctx, a.plan, m, in, confirm, bus)`, a
  six-argument call. This change appends a trailing `nil`, the same
  pattern the `runChild` site above uses:
  `report, err := flow.Run(ctx, a.plan, m, in, confirm, bus, nil)`.
  `agent.Run`'s own exported signature does not change; only its
  internal call to `flow.Run` gains the new argument.
- `docs/examples/flow-runner.md:51` — the walkthrough program's
  `flow.Run` call. Today it reads
  `report, err := flow.Run(context.Background(), graph, m, machine.InOut{Input: "review request"}, confirm, nil)`,
  a six-argument call whose trailing `nil` is the existing `bus`
  argument. This change appends a second trailing `nil`:
  `report, err := flow.Run(context.Background(), graph, m, machine.InOut{Input: "review request"}, confirm, nil, nil)`.

The builder updates every site above in this change. A missed site
fails to compile; `go build ./...` catches it before `make verify`
runs the rest of the gate.

Outside, and why:

- Retries. The caller re-invokes `Resume` on the same checkpoint after
  a step failure. `flow` adds no retry count and no backoff policy.
- Scheduling. The caller invokes `Resume` from a cron job, a queue, or
  a webhook. `Resume` being a plain resumable function call is
  scheduling's only prerequisite; `flow` adds no scheduler.
- History replay. Rejected in
  `docs/research-state-machine.md:236-238`: persistence stores one row,
  not an event log. A caller who persists every checkpoint already
  holds a replayable log; `flow` does not build one.
- Compensation. No named caller needs rollback-on-failure yet. Adding
  it now is speculative generality under the Building blocks rule in
  `AGENTS.md`.

## API

- `type Checkpoint struct { Status machine.Status; Record machine.InOut; Done []string }`
  — the full resumable state of a run. `Status` and `Record` are the
  same field types `Run`'s `Report` carries (`machine.Status` and
  `machine.InOut`). `Done` lists the lexicographically sorted step IDs
  of every `OutcomeSucceeded` entry in `outcomes` at the moment the
  checkpoint is built. `Done`'s order is a sort, not a completion
  order: two steps that complete in one order can appear in `Done` in
  the opposite order, if their IDs sort the other way.
- **Invariant, stated explicitly**: `Checkpoint.Done` lists only step
  IDs that resolved `OutcomeSucceeded`. `Run` today aborts immediately
  on the first `OutcomeFailed` step, and no code path yet produces
  `OutcomeSkipped` (see `flow/outcome.go`: "No producer exists yet").
  A checkpoint is therefore captured only along a fully-succeeding
  prefix of the graph; there is no `OutcomeFailed` or `OutcomeSkipped`
  entry to lose by storing `Done` as a plain ID list instead of an
  `id -> Outcome` map. This invariant holds only while these two facts
  hold. If a future phase adds an `OutcomeSkipped` producer, or changes
  `Run` to continue past a failed step, `Checkpoint.Done`'s shape needs
  re-examination: a plain ID list would then conflate "succeeded" with
  "skipped" on resume. That re-examination is out of scope for this
  phase; this paragraph is the flag for the phase that changes either
  fact.
- `func (c Checkpoint) Validate() error` — rejects an empty `Status`.
  `Encode` and `Decode` both call it, matching the `machine.Definition`
  pattern.
- `func (c Checkpoint) Encode() ([]byte, error)` — validates, then
  marshals with `encoding/json`. No registry: `Record.Input` and
  `Record.Output` are caller-owned `any` values. `encoding/json`
  decodes an `any` field back to `map[string]interface{}`, never the
  original concrete type. Phase 25 introduces a new caller contract:
  a caller whose `Input` or `Output` must survive a `Checkpoint`
  round-trip is responsible for using JSON-primitive-compatible types,
  or for re-hydrating its own concrete type after `Decode` (for
  example, through its own wrapper that re-asserts and converts the
  decoded map). `flow` performs no type-fidelity handling and no
  registry lookup; `Checkpoint` stores and returns whatever
  `encoding/json` produces.
- `func Decode(data []byte) (Checkpoint, error)` — unmarshals, then
  validates the result. Mirrors `machine.Decode`'s shape without its
  registry, since `Checkpoint` binds no guard or action.
- `Run` gains a trailing parameter, added after the existing
  `bus *events.Bus` parameter and before the existing return types:
  `func Run(ctx context.Context, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm, bus *events.Bus, onCheckpoint func(Checkpoint)) (Report, error)`
  — a breaking signature change to an exported function. The return
  type stays `(Report, error)`, unchanged from the current signature;
  only the parameter list grows. `onCheckpoint` is nil-safe, matching
  the existing nil-tolerant `bus *events.Bus` parameter. `runChild`
  passes a nil `onCheckpoint` for a chained step's inner `Run` call.
- `Checkpoint.Done` lists only step IDs that resolved
  `OutcomeSucceeded`. `Resume` seeds every listed ID as succeeded and
  never reconstructs `OutcomeFailed` or `OutcomeSkipped` state, because
  a checkpoint is never captured on a failure or skip path.
- `func Resume(ctx context.Context, d *Definition, m *machine.Definition, checkpoint Checkpoint, confirm Confirm, bus *events.Bus, onCheckpoint func(Checkpoint)) (Report, error)`
  — seeds `outcomes` from `checkpoint.Done` (every listed ID set to
  `OutcomeSucceeded`), `cur` from `checkpoint.Status`, and `rec` from
  `checkpoint.Record`, then continues the same graph walk `Run` uses.
  `Resume` returns a `Report`; its `Outcomes()` reflects the seeded
  IDs plus every step `Resume` itself resolves. "The resume guarantee"
  section below states `Resume`'s entry-check order, checks 1 through
  5, run once before any seeding happens.

### File layout

`Checkpoint` and its `Validate` live in a new file, `flow/checkpoint.go`.
`Encode` and `Decode` live in a separate new file, `flow/wire.go`,
matching the `machine` package's own split: `machine.Definition` and
its `Validate` live in `machine/definition.go`, while
`machine.Definition`'s `Encode` and `Decode` live in `machine/wire.go`.
`wire.go` is one of the filenames `semgrep/sdk-standards.yml`'s
`sdk.go.marshal-via-encode` rule excludes from its `json.Marshal` ban;
`checkpoint.go` is not. Putting `Encode`'s `json.Marshal(c)` call in
`flow/wire.go` keeps the marshal call inside an excluded file, the
same way `machine/wire.go` does for `Definition`. `Resume` and the
shared loop-body refactor stay in `flow/runner.go`, next to `Run`,
`runSingletonAndMark`, and `nextReadyGroup`, which they call directly.

`flow/runner.go` is 437 lines before this change, against the 500-line
file cap `scripts/check_structure.py` enforces. If adding `Resume` and
the shared loop-body refactor pushes `flow/runner.go` past 500 lines,
the builder moves `Resume`, the shared loop function, or both into a
new file, `flow/resume.go`, next to `flow/checkpoint.go`. This follows
the same precedent that gave `Checkpoint` its own file instead of
growing `flow/runner.go` further.

### The shared loop

`Run` and `Resume` factor into one unexported loop function that takes
the seed `cur`, `rec`, and `outcomes`, plus the same `confirm`, `bus`,
and `onCheckpoint` parameters. `Run` seeds `outcomes` empty and `cur`
at `m.Initial()`; `Resume` seeds `outcomes` from `checkpoint.Done`
(every entry `OutcomeSucceeded`) and `cur`/`rec` from
`checkpoint.Status`/`checkpoint.Record`. Neither function duplicates
`nextReadyGroup`, `runSingletonAndMark`, or `runWave`.

Every path that resolves a step or a wave already routes through one
of two call sites: `runSingletonAndMark` for a singleton (the
one-step short-circuit and both singleton branches inside the loop),
and the `markOutcome(outcomes, group, OutcomeSucceeded)` call after a
successful `runWave`. `onCheckpoint` fires immediately after each of
these two call sites, with a fresh
`Checkpoint{Status: cur, Record: rec, Done: <sorted keys of outcomes where value == OutcomeSucceeded>}`.
A nil `onCheckpoint` skips the call; the loop pays no cost building the
checkpoint value when the hook is nil. Since `Run` already aborts on
the first `OutcomeFailed` (returns before continuing the loop), and no
path produces `OutcomeSkipped` today, filtering to `OutcomeSucceeded`
entries is equivalent to listing every key of `outcomes` at the point
`onCheckpoint` fires — but the filter is written explicitly, so the
code states the invariant `Checkpoint.Done` promises instead of
relying on it as an accident of today's abort behavior.

`Run`'s existing one-step short-circuit
(`flow/runner.go:76-80`) calls `runSingletonAndMark` unconditionally
today; it never checks `outcomes` first, unlike the general loop's
`nextReadyGroup` call. This change adds that check to the
short-circuit: before calling `runSingletonAndMark`, the short-circuit
tests whether `d.steps[0].ID` is already a key of the seeded
`outcomes` map. `Run` always seeds `outcomes` empty, so the check is
always false for `Run` and the short-circuit behaves exactly as
before. `Resume` can seed `outcomes` from a `checkpoint.Done` that
already lists the single step's ID, for a one-step `Definition` whose
checkpoint covers the whole graph. When the check is true, the
short-circuit skips `runSingletonAndMark` entirely — no `Fire`, no
`confirm` call, no `onCheckpoint` call — and returns
`Report{status: cur, record: rec, outcomes: outcomes}, nil` straight
away, matching "The resume guarantee" section below. When the check is
false, the short-circuit calls `runSingletonAndMark` as before and
still fires `onCheckpoint` once after the single step resolves, for
the same one-call-per-completion invariant. A one-step graph is
resumable like any other.

The zero-step short-circuit (`flow/runner.go:73-75`) never fires
`onCheckpoint`. There is no step to complete, so there is nothing to
checkpoint; an empty `Done` checkpoint before any step runs is not a
meaningful resume point. This applies regardless of whether
`onCheckpoint` is nil.

### The pause rule

At the top of each loop iteration, before `nextReadyGroup` runs, the
loop checks `ctx.Err()`. A non-nil error means the caller canceled the
context. The loop returns
`Report{status: cur, record: rec, outcomes: outcomes}, errorf("run paused: %w", ctx.Err())`
immediately; no new step starts. This matches the existing
`Report{status: cur, record: rec, outcomes: outcomes}, err`-shaped
construction every other abort path in `Run` already uses (see the
stalled-graph return, the singleton-error return, and the wave-error
return in `flow/runner.go`); the pause path is one more `Report`
literal built the same way, not a new return shape. A caller pauses a
run by canceling `ctx`. The last checkpoint `onCheckpoint` delivered
is the resume point; `flow` adds no separate pause API.

The check sits between steps, not inside one. A step already running
when the context cancels keeps running to its own completion or
failure; `Fire`, `Guard`, and the step's actions decide their own
cancellation behavior from the `ctx` they already receive. The loop
only refuses to start the *next* step or wave after a cancellation it
observes at the top of an iteration.

### The resume guarantee

`onCheckpoint` fires only after a step's or wave's outcome is marked
`OutcomeSucceeded`, so a checkpoint never captures a step mid-flight.
Every step ID in `checkpoint.Done` finished before the checkpoint
existed. `Resume` never re-runs a step already in `Done`, because
`nextReadyGroup` skips any step ID already present in `outcomes`
(seeded from `Done` as `OutcomeSucceeded`).

The one step `Resume` can re-run is the step or wave that was in
progress, not yet recorded done, when the run stopped: a crash mid-step,
or a context cancellation the process observed inside a step rather
than at the loop's top-of-iteration check. That step's side effects
must tolerate a second attempt. This is a caller contract on step
implementations with external effects, not a runtime check `flow`
performs. No other step carries this requirement.

For a chained step specifically: if a checkpoint is captured
immediately after the chained step's parent transition fires (inside
`fireFromChild`, after `runSingleton` returns for that step), the
chained step's ID is already in `Done` as `OutcomeSucceeded`. A
subsequent `Resume` skips the whole step, including its child
workflow: `nextReadyGroup` never re-selects it, so `runChild` and the
child's `confirm` calls do not run again. `Resume` has no visibility
into a child workflow's own internal progress; the parent step's
completion is the only granularity a checkpoint records.

`Resume` on a checkpoint whose `Done` already covers every step in `d`
returns `Report{status: checkpoint.Status, record: checkpoint.Record, outcomes: outcomes}, nil`
without calling `confirm`, firing an event, or invoking `onCheckpoint`
again. There is no remaining work. This guarantee holds for a
one-step `Definition` too: "The shared loop" section's guard on the
one-step short-circuit checks `d.steps[0].ID` against the seeded
`outcomes` before calling `runSingletonAndMark`, so a `Resume` call
whose `checkpoint.Done` already names that single step skips the
short-circuit's `Fire`, `confirm`, and `onCheckpoint` calls and
returns the seeded `Report` directly, the same as the general loop
does for a multi-step all-done checkpoint.

`Resume` runs its entry checks in one fixed order, stated here once.
Every other section of this plan refers back to this order by name
instead of restating it. Each check runs only after every earlier
check passes; the first failing check returns an error immediately,
and no step runs:

1. `d` is nil.
2. `m` is nil.
3. `confirm` is nil.

   Checks 1 through 3 match `Run`'s own nil-check order
   (`flow/runner.go:59-67`). Both run before `Resume` dereferences
   `d` or `m`.
4. `checkpoint.Validate()` fails.

   An invalid checkpoint cannot seed a walk, regardless of the graph.
   This check runs only after checks 1 through 3 pass.
5. `checkpoint.Done` names a step ID absent from `d.steps`.

   `Resume` walks `checkpoint.Done` and confirms every ID names a
   step in `d.steps`. The first unmatched ID fails `Resume`, naming
   that ID in the error. This check exists because `nextReadyGroup`
   scans only `d.steps`. An ID in `Done` naming no step in `d` would
   otherwise sit in the seeded `outcomes` map, inert. `Resume` would
   then silently walk a graph the checkpoint does not fully describe.

Only after all five checks pass does `Resume` seed state: `outcomes`
from `checkpoint.Done` (every listed ID set to `OutcomeSucceeded`),
`cur` from `checkpoint.Status`, and `rec` from `checkpoint.Record`.
`Resume` fails loud, before any step fires, rather than let a
caller-supplied mismatch corrupt a walk silently.

`Resume` performs no topology check across `Done`: it never confirms
that a step named in `Done` has every one of its own `Needs` also
named in `Done`. A topologically-inconsistent checkpoint — for
example, `Done` naming a downstream step while a step it needs is
absent from `Done` — is not rejected at entry. `Resume`'s defense
against this case is indirect: `nextReadyGroup` treats the missing
prerequisite as still unresolved and selects it to run again, and the
resulting `pickTransition` or `machine.Fire` call fails, because
`checkpoint.Status` (`cur`) no longer names a status the seeded
walk can reach that step from. `Resume` returns that failure as an
ordinary error; it performs no separate topology check of its own.

The `policy/layers.json` row for `flow` is unchanged:
`"flow": ["events", "machine"]`. `Checkpoint` uses only
`encoding/json`, which is stdlib and not an internal edge.

## Tests

Test files live in `flow/flow_test/`:

- `checkpoint_test.go` — the red-green cases. Red step: the file does
  not compile on the empty phase, because `Checkpoint`, `Decode`, and
  `Resume` do not exist, and `Run`'s call sites are missing the new
  parameter. Cases:
  - `Checkpoint.Validate` rejects an empty `Status`.
  - `Encode` then `Decode` round-trips `Status`, `Record`, and `Done`.
  - `Decode` rejects malformed JSON.
  - `Decode` runs `Validate` on the parsed result and rejects an empty
    `Status` read from the wire.
  - `Encode` a `Checkpoint` whose `Record.Input` holds a concrete
    struct, then `Decode` it. Assert the decoded `Record.Input` is a
    `map[string]interface{}`, not the original struct type, so a
    direct type assertion to the original type fails. Assert the
    caller must convert the map itself to recover the value,
    documenting that raw `any` type identity does not survive the
    round-trip.
  - `Run` on a zero-step `Definition` with a non-nil `onCheckpoint`
    never calls it.
  - `Run` with a non-nil `onCheckpoint` fires once per singleton step;
    `Done` contains exactly the sorted IDs of the steps completed so
    far, after each call. Use step IDs that are not already
    alphabetical in completion order (for example, a step named `z`
    that completes before a step named `a`), so the test fails if the
    implementation lists `Done` in completion order instead of sorted
    order. `outcomes_test.go`'s existing fixtures use alphabetical
    step IDs (`a`, `b`, `c`, `d`) and cannot distinguish the two
    orderings; this case needs its own non-alphabetical fixture.
  - `Run` with a non-nil `onCheckpoint` fires once per wave, with every
    wave member present in `Done` after that one call.
  - `Run` with a nil `onCheckpoint` behaves exactly as before this
    phase: same `Report`, same error.
  - `Run` returns the pinned pause error when `ctx` is already canceled
    before the loop's first iteration.
  - `Run` returns the pinned pause error mid-graph, after at least one
    step completed and its checkpoint fired, when `ctx` cancels before
    the next step starts.
  - `Resume` seeds `outcomes`, `cur`, and `rec` from a checkpoint
    captured mid-graph and completes the remaining steps to the same
    final `Report` a single uninterrupted `Run` call would reach.
  - `Resume` on a checkpoint captured right after a chained step's
    parent transition fires: cancel `ctx` right after that checkpoint,
    then call `Resume`. Assert the chained step's ID appears exactly
    once in the checkpoint's `Done`. Assert the resumed run does not
    re-invoke the child workflow's `confirm` closure (count calls with
    a step-local counter; the count after `Resume` equals the count
    at checkpoint time). Add this case to
    `checkpoint_integration_test.go`'s test list below, since it needs
    a full `Definition` with a `Sub` step, matching the fixture shape
    `chain_test.go` already builds.
  - `Resume` on an all-done checkpoint returns the checkpoint's status
    and record, and calls neither `confirm` nor `onCheckpoint`.
  - `Resume` on an all-done checkpoint for a one-step `Definition`:
    build a `Checkpoint` whose `Done` already names the single step's
    ID, call `Resume`, and assert it returns the checkpoint's status
    and record without calling `confirm` or `onCheckpoint`. This pins
    the one-step short-circuit's guard from "The shared loop" section,
    separate from the multi-step case above.
  - `Resume` rejects a nil `d`, a nil `m`, and a nil `confirm`, in
    that order: checks 1 through 3 of "The resume guarantee" section.
  - `Resume` rejects a checkpoint that fails `Validate`, once `d`, `m`,
    and `confirm` are all non-nil: check 4 of "The resume guarantee"
    section.
  - `Resume` rejects a checkpoint whose `Done` names a step ID absent
    from `d`: build a `Checkpoint` with a `Done` entry naming a step ID
    that no step in `d` uses, and assert `Resume` returns an error
    naming that ID before any step runs (assert `confirm` and
    `onCheckpoint` are never called). Check 5 of "The resume
    guarantee" section.
  - `Resume` called with both a nil `d` and a checkpoint that fails
    `Validate`: assert the returned error is the nil-`d` error, not a
    `Validate` error. Checks 1 through 3 run before check 4, so the
    nil-`d` check wins.
  - `Resume` on a checkpoint whose `Done` names a real step in `d`
    while that step's own `Needs` entry is absent from `Done`: build
    such a `Checkpoint` against a graph with a real prerequisite edge
    (for example, `join` in `Done` without its `Needs` entries `left`
    and `right`), call `Resume`, and assert it returns an error rather
    than a `Report` with a silently wrong `Done` set. `Resume` has no
    dedicated check for this case; "The resume guarantee" section
    states that `pickTransition` or `machine.Fire` rejects the
    resulting out-of-order transition attempt instead.
- `checkpoint_integration_test.go` — run a multi-step graph end to end
  with a real `onCheckpoint` that appends `Encode`d bytes to an
  in-memory slice, simulating caller-owned storage. Cancel `ctx` after
  the first checkpoint lands. Decode the last stored checkpoint and
  call `Resume`. Assert the resumed run reaches the same final
  `Report` (status, record, and outcomes) a plain, uninterrupted `Run`
  reaches on the same graph. Assert the step before the pause point
  runs exactly once, counted through a step-local counter. Repeat the
  pause-and-resume sequence across a wave boundary, resuming after a
  captured wave checkpoint. Add the chained-step case described above:
  a graph with a `Sub` step, checkpoint captured right after the
  chained step's parent transition fires, cancel, resume, and assert
  the child's `confirm` is not called again and the chained step's ID
  appears once in `Done`.
- `checkpoint_bench_test.go` — benchmark `Run` with a non-nil
  `onCheckpoint` against `Run` with a nil `onCheckpoint`, on the same
  graph the phase 7 chain benchmark uses. Measure the baseline (nil
  hook) before this phase lands and record it in the file's leading
  comment. Report the allocs/op ratio; a benchmark may skip a fixed
  allocation budget when goroutine and closure overhead vary, per
  `docs/plans/agents/PHASES.md`.

## Verification

`make verify` passes. `go test -race ./...` covers the panel-wave
checkpoint path, since `onCheckpoint` reads `outcomes` and `cur` from
the same loop that already runs waves in goroutines. The coverage
floor for `flow` holds.

`api/flow.txt` gains `Checkpoint`, its `Validate`, `Encode`, and
`Decode`, `Resume`, and the changed `Run` signature, via
`make api-update`. Commit the `api/` diff in the same change.
`policy/layers.json` is unchanged; no new import edge. `api/machine.txt`
is unchanged.

`docs/architecture.md` and `docs/packages/flow.md` update their flow
sections in the same change, describing the checkpoint hook, `Resume`,
and the pause rule; `docs/packages/flow.md`'s `Run` entry and its
example at line 216 both move to the seven-argument signature.
`docs/examples/flow-runner.md`'s example at line 51 also moves to the
seven-argument signature, in the same change. `AGENTS.md` updates its
`flow/` layout bullet in the same change to name checkpoint, pause,
and resume alongside the existing runner vocabulary.

No conformance vector changes: `Checkpoint` carries no signed or
threaded wire form, so `envelope/testdata/vectors/` is untouched.
`docs/protocol-design.md` is untouched for the same reason.
