# Phase 25: flow checkpoint pause and resume

Status: ready to build. Builds on the runner loop phase 19 shipped:
`Run`, `markDone`, `nextReadyGroup`, and the emit hook. Independent of
phases 21 through 24; checkpointing needs no outcome, admission,
route, or machine-accessor change. See `docs/plans/agents/PHASES.md`.

## Goal

Let a caller pause a run and resume it later from the last completed
step. `Run` reports its progress through a checkpoint hook. `Resume`
restarts a graph walk from a stored checkpoint. The caller owns
storage; `flow` owns only the in-memory state and the hook.

## Scope

Inside: the `Checkpoint` type, its `Encode` and `Decode` pair, the
`onCheckpoint` hook on `Run`, the new `Resume` function, and the
context-cancellation pause check.

The `Run` signature change touches every existing call site. Each one
gains a trailing `nil` or `onCheckpoint` argument in this change:

- `flow/runner.go:169` — `runChild`'s inner `Run` call, which passes
  `nil`.
- All 58 `flow.Run(...)` call sites across 11 files in
  `flow/flow_test/`: `chain_new_test.go`, `chain_integration_test.go`,
  `run_integration_test.go`, `run_test.go`, `chain_test.go`,
  `chain_bench_test.go`, `run_bench_test.go`,
  `panel_integration_test.go`, `panel_bench_test.go`, `emit_test.go`,
  `panel_test.go`.
- The documented example at `docs/packages/flow.md:218`:
  `status, out, err := flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm, bus)`
  gains the trailing argument and moves to
  `flow.Run(ctx, graph, statusMachine, machine.InOut{}, confirm, bus, nil)`.

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
  — the full resumable state of a run. `Done` lists completed step IDs,
  in the order `markDone` recorded them.
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
- `Run` gains a trailing parameter:
  `func Run(ctx context.Context, d *Definition, m *machine.Definition, in machine.InOut, confirm Confirm, bus *events.Bus, onCheckpoint func(Checkpoint)) (machine.Status, machine.InOut, error)`
  — a breaking signature change to an exported function. `onCheckpoint`
  is nil-safe, matching the existing nil-tolerant `bus *events.Bus`
  parameter. `runChild` passes a nil `onCheckpoint` for a chained
  step's inner `Run` call; a child workflow's progress is not
  independently resumable, only the parent step's completion is.
- `func Resume(ctx context.Context, d *Definition, m *machine.Definition, checkpoint Checkpoint, confirm Confirm, bus *events.Bus, onCheckpoint func(Checkpoint)) (machine.Status, machine.InOut, error)`
  — seeds `done` from `checkpoint.Done`, `cur` from `checkpoint.Status`,
  and `rec` from `checkpoint.Record`, then continues the same graph
  walk `Run` uses.

### File layout

`Checkpoint`, its `Validate`, `Encode`, and `Decode` live in a new
file, `flow/checkpoint.go`. `Resume` and the shared loop-body refactor
stay in `flow/runner.go`, next to `Run`, `markDone`, and
`nextReadyGroup`, which they call directly.

### The shared loop

`Run` and `Resume` factor into one unexported loop function that takes
the seed `cur`, `rec`, and `done`, plus the same `confirm`, `bus`, and
`onCheckpoint` parameters. `Run` seeds `done` empty and `cur` at
`m.Initial()`; `Resume` seeds both from `checkpoint`. Neither function
duplicates `nextReadyGroup`, `runSingleton`, or `runWave`.

Every path that marks a step or a wave done routes through `markDone`.
Today only the wave path calls `markDone`; the two singleton paths set
`done[id] = true` directly. This phase changes both singleton paths to
call `markDone` with a one-member group, so every completion has one
call site. `onCheckpoint` fires immediately after each `markDone` call,
with a fresh `Checkpoint{Status: cur, Record: rec, Done: <sorted keys of done>}`.
A nil `onCheckpoint` skips the call; the loop pays no cost building the
checkpoint value when the hook is nil.

`Run`'s existing one-step short-circuit, ahead of the loop, still
fires `onCheckpoint` once after its single step, for the same
one-call-per-completion invariant. A one-step graph is resumable like
any other.

The zero-step short-circuit (`flow/runner.go:63-65`) never fires
`onCheckpoint`. There is no step to complete, so there is nothing to
checkpoint; an empty `Done` checkpoint before any step runs is not a
meaningful resume point. This applies regardless of whether
`onCheckpoint` is nil.

### The pause rule

At the top of each loop iteration, before `nextReadyGroup` runs, the
loop checks `ctx.Err()`. A non-nil error means the caller canceled the
context. The loop returns `(cur, rec, errorf("run paused: %w", ctx.Err()))`
immediately; no new step starts. A caller pauses a run by canceling
`ctx`. The last checkpoint `onCheckpoint` delivered is the resume
point; `flow` adds no separate pause API.

The check sits between steps, not inside one. A step already running
when the context cancels keeps running to its own completion or
failure; `Fire`, `Guard`, and the step's actions decide their own
cancellation behavior from the `ctx` they already receive. The loop
only refuses to start the *next* step or wave after a cancellation it
observes at the top of an iteration.

### The resume guarantee

`onCheckpoint` fires only after `markDone` runs, so a checkpoint never
captures a step mid-flight. Every step ID in `checkpoint.Done`
finished before the checkpoint existed. `Resume` never re-runs a step
already in `Done`.

The one step `Resume` can re-run is the step or wave that was in
progress, not yet recorded done, when the run stopped: a crash mid-step,
or a context cancellation the process observed inside a step rather
than at the loop's top-of-iteration check. That step's side effects
must tolerate a second attempt. This is a caller contract on step
implementations with external effects, not a runtime check `flow`
performs. No other step carries this requirement.

`Resume` on a checkpoint whose `Done` already covers every step in `d`
returns `(checkpoint.Status, checkpoint.Record, nil)` without calling
`confirm`, firing an event, or invoking `onCheckpoint` again. There is
no remaining work.

`Resume` rejects a nil `d`, a nil `m`, and a nil `confirm`, in that
order, matching `Run`. It also rejects a `checkpoint` that fails
`Validate`, checked before `d` and `m`, since an invalid checkpoint
cannot seed a walk regardless of the graph.

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
  - `Run` with a non-nil `onCheckpoint` fires once per singleton step,
    with `Done` growing by one ID each time, in completion order.
  - `Run` with a non-nil `onCheckpoint` fires once per wave, with every
    wave member present in `Done` after that one call.
  - `Run` with a nil `onCheckpoint` behaves exactly as before this
    phase: same status, same record, same error.
  - `Run` returns the pinned pause error when `ctx` is already canceled
    before the loop's first iteration.
  - `Run` returns the pinned pause error mid-graph, after at least one
    step completed and its checkpoint fired, when `ctx` cancels before
    the next step starts.
  - `Resume` seeds `done`, `cur`, and `rec` from a checkpoint captured
    mid-graph and completes the remaining steps to the same final
    status a single uninterrupted `Run` call would reach.
  - `Resume` on an all-done checkpoint returns the checkpoint's status
    and record, and calls neither `confirm` nor `onCheckpoint`.
  - `Resume` rejects a nil `d`, a nil `m`, and a nil `confirm`, in that
    order.
  - `Resume` rejects an invalid checkpoint before it dereferences `d`
    or `m`.
- `checkpoint_integration_test.go` — run a multi-step graph end to end
  with a real `onCheckpoint` that appends `Encode`d bytes to an
  in-memory slice, simulating caller-owned storage. Cancel `ctx` after
  the first checkpoint lands. Decode the last stored checkpoint and
  call `Resume`. Assert the resumed run reaches the same final status
  and record a plain, uninterrupted `Run` reaches on the same graph.
  Assert the step before the pause point runs exactly once, counted
  through a step-local counter. Repeat the pause-and-resume sequence
  across a wave boundary, resuming after a captured wave checkpoint.
- `checkpoint_bench_test.go` — benchmark `Run` with a non-nil
  `onCheckpoint` against `Run` with a nil `onCheckpoint`, on the same
  graph the phase 7 chain benchmark uses. Measure the baseline (nil
  hook) before this phase lands and record it in the file's leading
  comment. Report the allocs/op ratio; a benchmark may skip a fixed
  allocation budget when goroutine and closure overhead vary, per
  `docs/plans/agents/PHASES.md`.

## Verification

`make verify` passes. `go test -race ./...` covers the panel-wave
checkpoint path, since `onCheckpoint` reads `done` and `cur` from the
same loop that already runs waves in goroutines. The coverage floor
for `flow` holds.

`api/flow.txt` gains `Checkpoint`, its `Validate`, `Encode`, and
`Decode`, `Resume`, and the changed `Run` signature, via
`make api-update`. Commit the `api/` diff in the same change.
`policy/layers.json` is unchanged; no new import edge. `api/machine.txt`
is unchanged.

`docs/architecture.md` and `docs/packages/flow.md` update their flow
sections in the same change, describing the checkpoint hook, `Resume`,
and the pause rule. `AGENTS.md` updates its `flow/` layout bullet in
the same change to name checkpoint, pause, and resume alongside the
existing runner vocabulary.

No conformance vector changes: `Checkpoint` carries no signed or
threaded wire form, so `envelope/testdata/vectors/` is untouched.
`docs/protocol-design.md` is untouched for the same reason.
