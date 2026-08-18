# Phase 38: flow loop

Status: future. Depends on phase 23
(`docs/plans/agents/phase23_flow_fallback.md`) landing first. Phases
21 and 22 shipped: `Outcome`, `Report`, `Admission`, and `Route`
already exist in `flow`'s code and locked API; see
`docs/plans/flow.md`. Phase 23 still adds the fallback path. As of
this writing, `flow`'s tree (`flow/runner.go`, `api/flow.txt`,
`flow/flow_test/`) carries no fallback vocabulary; it exists only in
the phase 23 plan file, marked "Status: ready to build", but not yet
shipped. This phase is not independently buildable today. Per
`docs/plans/agents/PHASES.md`, a phase does not pull in a later
phase, so a builder must not start this phase until phase 23 is
shipped and `FailureFrom` exists in `flow`'s code and locked API.
Phase 30 (flow retry, plan-only, `docs/plans/agents/phase30_flow_retry.md`)
shares the same dependency and may land before or after this phase;
the two phases touch disjoint step fields and neither blocks the
other.

## Why repeated chaining, not a graph cycle

A review-style loop needs a step to run again after a guard decides
the work is not done: draft, review, revise, review again, until an
approval or a cap. Two shapes were weighed.

- A back-edge in `flow.Definition`'s step graph. Rejected. The
  cycle-check in `flow/validate.go`'s `findRoots` stays exactly as
  is; this is a deliberate, reasoned call, not an oversight. Kahn's
  algorithm proves the graph acyclic before any step runs, and the
  topological wave scheduler in `flow/runner.go` assumes that proof.
  A back-edge breaks topological execution: `nextReadyGroup` can no
  longer decide when a step is ready, because "ready" today means
  every `Needs` entry already resolved to one terminal `Outcome`, and
  a step in a cycle never resolves. The `Report.Outcome`-per-step
  model breaks too: `Report` records one `Outcome` per step ID, and a
  step that fires many times has no single terminal outcome to
  record. Reworking both would touch every phase built on top of
  `Report` and `Outcome`, including phases 22, 23, 25, and 30.
- Repeated invocation of `Step.Sub`, the chaining mechanism phase 7
  already shipped. Chosen. `runSingleton` in `flow/runner.go` already
  runs a step's `Sub` child `Definition` to completion, then fires the
  parent's own transition from the child's final status through
  `fireFromChild`. A loop step calls that same sequence more than
  once, then fires `Confirm` once, after the sequence stops. No new
  graph primitive. No change to `findRoots`, `nextReadyGroup`, or the
  wave scheduler. Each loop step still resolves to exactly one
  `Outcome`, matching every phase built on `Report` today.

## Goal

Let a step run its `Sub` child workflow more than once, gated by a
caller-supplied guard, before the parent step's transition and
`Confirm` fire. `Max`, the iteration cap, defaults to zero, which
means explicitly, deliberately unbounded: the loop runs until the
guard clears or `ctx` is canceled or expires, full stop, with no
engine-imposed ceiling. The only bound on an unbounded loop is the
`ctx` deadline the caller already passes to `Run`. This phase adds no
hidden safety limit and no default cap.

## Scope

Inside: the `LoopPolicy` type, its `Validate` method, the `Step.Loop`
field, the `LoopState` type, the `LoopStateFrom` context accessor,
and the loop-driving change inside `runSingleton`. This phase extends
the existing `flow` package. It adds no new top-level package, so
`policy/layers.json` gains no new row; the `flow` row already lists
`events` and `machine`, and this phase needs no import beyond those
two plus the standard library.

Outside:

- A graph cycle. See the reasoning above. `flow.Definition` stays a
  DAG; `New`'s cycle rejection in `flow/validate.go` is unchanged.
- A new predicate type for the loop condition. `LoopPolicy.Guard`
  reuses `machine.Guard`'s exact signature,
  `func(ctx context.Context) (bool, error)`, verbatim. `machine`'s own
  convention already treats a nil `Guard` as "no check"; `LoopPolicy`
  keeps that convention rather than inventing a new one.
- A hidden or default iteration ceiling. `Max` zero means unbounded,
  not capped. A caller who wants a cap sets `Max` above zero. This
  phase never substitutes an engine-chosen number for a caller's
  explicit zero.
- Loop support for a panel member. A wave fires every member's
  transition inside its own goroutine on one shared clock tick, from
  phase 30's own reasoning about retry; a per-member loop would
  desynchronize the wave's shared transition row the same way a
  per-member retry would. `New` rejects the shape.
- Loop combined with `Retry` on the same step. Phase 30 already
  rejects `Retry` on a step with a non-nil `Sub`. `Loop` requires a
  non-nil `Sub`, so the two fields are already mutually exclusive
  through phase 30's existing rule; this phase adds no new check for
  that combination.
- Cross-run persistence of loop iteration state. An iteration count
  lives only in the `runSingleton` call stack for the run in
  progress, matching phase 30's retry-count scope. Phase 25's
  checkpoint plan is the durability path, not this phase.
- A record accumulator or reducer across iterations beyond passing
  the previous iteration's output forward as the next iteration's
  input. `flow` does not merge or diff records; a `Guard` closure that
  needs history keeps its own state, matching the existing
  panel-aliasing and retry-idempotency caveats that push caller state
  onto caller closures.

## The loop-driving change

`runSingleton` already handles a step with a non-nil `Sub` through
`runChild` and `fireFromChild`. This phase adds a second path for a
step whose `Sub` is non-nil and whose `Loop` is also non-nil.

1. `iteration` starts at zero. The loop body runs at least once.
2. Before each iteration, `runSingleton` checks `ctx.Err()`. A
   non-nil error stops the loop at once and reports the context error
   as the step's failure, wrapped `flow: step %q: %w`. This check runs
   in the loop driver itself, not inside `Sub`'s own `Run` call, so an
   unbounded loop (`Max` zero) still terminates even when the
   caller's own `Guard`, `OnEntry`, or `OnExit` closures never inspect
   `ctx` themselves. This mirrors phase 30's default `Sleep`, which
   also selects on `ctx.Done()` rather than trusting a closure to
   check it.
3. `runSingleton` runs the child workflow via a loop-aware variant of
   `runChild` that starts the child from the previous iteration's
   output record instead of phase 7's fresh `machine.InOut{}`. The
   first iteration starts from the parent step's own incoming record,
   the same record a non-looped chained step already starts its child
   from today. This carries review state, such as a revision note,
   from one iteration to the next.
4. `runSingleton` fires the parent's own transition from the current
   status to the child's final status through the existing
   `fireFromChild` logic, updating `cur` and `rec`.
5. `runSingleton` builds a `LoopState{Iteration: iteration, Record:
   rec}` and injects it into `ctx` for the next `Guard` call, through
   the same context-injection pattern phase 23 uses for `Failure`.
6. `iteration` increments.
7. When `Max` is non-zero and `iteration` has reached `Max`, the loop
   stops. This is the only engine-imposed stop; it never applies when
   `Max` is zero.
8. Otherwise `runSingleton` evaluates `LoopPolicy.Guard` with the
   updated `ctx`. A nil `Guard` reads as true, matching `machine.Fire`
   and `machine.Guard`'s own nil convention. A `Guard` error stops the
   loop at once and aborts the step, wrapped `flow: step %q: %w`,
   mirroring how `machine.Fire` already surfaces a `Guard` error.
   A `false` result stops the loop, but unlike `machine.Fire`, where a
   false `Guard` aborts the transition with an error, a false
   `LoopPolicy.Guard` ends the loop as a normal, successful exit: the
   already-fired transitions stand, and the step proceeds to
   `Confirm`. A `true` result repeats from step 2.
9. Once the loop stops for any reason other than a `ctx` or `Guard`
   error, `runSingleton` calls `Confirm` once, for the whole step, the
   same way today's non-looped chained step calls `Confirm` once
   after its single child run. `runSingleton` then emits one
   `StepCompletedEvent`, matching every other step kind.

A step that exhausts `ctx` or whose `Guard` errors ends `OutcomeFailed`
in `Report`, exactly like any other `Fire` or `Confirm` failure today.
Phase 23's fallback admission applies unchanged: a declared
`AdmissionOnFailed` dependent still catches a loop step's failure,
which is the reason this phase sequences after phase 23. Without a
fallback, an unbounded loop step that fails when `ctx` expires aborts
the whole `Run`, exactly like any other unhandled step failure.

## API

The surface below lands in `api/flow.txt` via `make api-update`.

- `type LoopPolicy struct { Guard machine.Guard; Max int }` — a
  step's loop rule. `Guard` reuses `machine.Guard`'s exact type; a nil
  `Guard` means "always continue," matching `machine`'s own
  convention. `Max` caps the iteration count; zero means unbounded,
  bounded only by the caller's own `ctx`. A negative `Max` is invalid.
- `func (p LoopPolicy) Validate() error` — rejects `Max < 0` with the
  pinned message below. `New` calls this for every step whose `Loop`
  is non-nil. `Validate` never rejects a nil `Guard` or a zero `Max`;
  both are deliberate, supported values, not defaults to warn about.
- `type LoopState struct { Iteration int; Record machine.InOut }` —
  the loop context a `Guard` closure reads. `Iteration` counts
  completed iterations, starting at zero before the first `Guard`
  call. `Record` carries the most recent child workflow's output.
- `func LoopStateFrom(ctx context.Context) (LoopState, bool)` — reads
  the `LoopState` `runSingleton` injects before each `Guard` call. The
  boolean is false outside a loop step's `Guard` evaluation, matching
  phase 23's `FailureFrom` shape exactly.
- `Step` gains `Loop *LoopPolicy`. Nil means no loop, matching a
  plain chained step's single-run behavior exactly.

`New` gains three validations, with pinned messages:

- `flow: step %q loop: max must be at least 0` — a `LoopPolicy` with
  `Max < 0`.
- `flow: step %q has a loop policy but no sub-workflow` — `Loop` set
  on a step whose `Sub` is nil.
- `flow: panel %d names looped step %q` — `Loop` set on a panel
  member.

## Tests

Test files live in `flow/flow_test/`:

- `loop_test.go` — the red-green cases. Red step: the file does not
  compile on the empty phase, because `LoopPolicy`, `Step.Loop`,
  `LoopState`, and `LoopStateFrom` do not exist. Record the compiler
  error as the red. Cases:
  - `LoopPolicy.Validate` rejects `Max` of negative one, pinned
    message. Accepts `Max` of zero and `Max` of one.
  - `New` rejects a looped step with a nil `Sub`, pinned message.
  - `New` rejects a looped panel member, pinned message.
  - `New` accepts a looped step with a non-nil `Sub` and no panel.
  - A loop step whose `Guard` returns false on the second call runs
    its child workflow exactly twice and ends `OutcomeSucceeded`.
    Assert `LoopStateFrom` inside the second `Guard` call reports
    `Iteration` one and the first iteration's output record.
  - A loop step with `Max` two and a `Guard` that always returns true
    still stops after two iterations. Assert the `Guard` never runs a
    third time; the `Max` cap decides before the third `Guard` call.
  - A loop step with `Max` zero and a `Guard` that returns false on
    the fifth call runs exactly five iterations, proving zero imposes
    no engine ceiling.
  - A loop step with `Max` zero, a `Guard` that always returns true,
    and a `ctx` canceled from a second goroutine after the third
    iteration's child workflow completes: the loop stops at the next
    `ctx.Err()` check, before a fourth child run, and the step ends
    `OutcomeFailed` wrapping `context.Canceled`.
  - A loop step whose `Guard` returns a non-nil error on its first
    call: the loop stops after one iteration and the step ends
    `OutcomeFailed` wrapping that error.
  - A loop step's second iteration receives the first iteration's
    output record as its input; assert a value written to
    `Record.Output` on iteration one is visible to `Record.Input` (or
    the equivalent read path) on iteration two.
  - `LoopStateFrom` outside any loop step's `Guard` call returns false
    and a zero `LoopState`.
  - A loop step whose `ctx` is already canceled at `Run`'s entry ends
    `OutcomeFailed` after zero child workflow runs.
  - A loop step that exhausts `ctx` and has a phase 23 fallback
    declared: the run continues down the fallback path, and
    `FailureFrom` inside the fallback returns the context error.
- `loop_integration_test.go` — run a two-status graph where a single
  looped step's child workflow moves a counter in `Record.Output` by
  one per iteration, and `Guard` reads `LoopStateFrom` to stop once
  the counter reaches three. Assert the final record, the final
  status, and `Iteration` at each `Guard` call. Run a second case with
  `Max` zero and a short `ctx` deadline, and assert the run aborts
  once the deadline passes, with no `Max`-driven stop. Run a third
  case combining a loop step with a phase 23 fallback catching the
  deadline failure.
- `loop_bench_test.go` — benchmark a ten-iteration loop step, with a
  `Guard` that always returns true until `Max`, against ten separate
  non-looped chained steps performing the same child workflow.
  Measure the ten-separate-steps baseline on the phase 23 code before
  this phase lands. Record both in the file's leading comment. Report
  the ns/op and allocs/op ratio; the loop path's context injection per
  iteration varies with `LoopState` construction, so a fixed
  allocation budget applies as in phase 21's benchmarks.

## Verification

`make verify` passes. The coverage floor for `flow` holds.
`api/flow.txt` gains `LoopPolicy`, its `Validate` method, `LoopState`,
`LoopStateFrom`, and `Step.Loop` via `make api-update`. Commit the
`api/` diff in the same change. `policy/layers.json` is unchanged;
`flow`'s allowed imports (`events`, `machine`) already cover every
edge this phase needs. `api/machine.txt` is unchanged; the `machine`
package is untouched.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change as the code. Describe `LoopPolicy`,
`LoopState`, and the loop driver's place between a chained step's
single run and phase 30's retry loop. State the unbounded-by-default
contract in the package doc the same way this plan states it.
`AGENTS.md` updates its `flow/` layout bullet in the same change; the
bullet names the loop vocabulary next to outcome, admission, route,
fallback, and retry.
