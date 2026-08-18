# Phase 39: scheduler

Status: future. Plan-only; it has not yet gone through plan review. It
adds one new package, `scheduler`, with no dependency on a landed
phase. It ships independently of every other phase in this group,
including phase 38 (flow loop) and phase 40 (trigger).

## Goal

Give this SDK one generic, reusable way to invoke anything on a
schedule: an agent run, a flow run, a tool call, or any other closure
a caller supplies. `scheduler` decides when to run something; it never
decides what that something is.

## Scope

Inside: the `Job` function type, the `Schedule` interface, two
concrete schedules (`Every`, a fixed interval, and `At`, a fixed set
of times), the `Scheduler` type, and its `Add`, `Remove`, and `Run`
methods. `scheduler` is a leaf package for composition purposes: it
imports no other internal package's invocable type. It imports
`events` only for one typed event-name constant, matching
`heartbeat`'s precedent.

Outside:

- Any coupling to `agent`, `flow`, or `tools`. `Job` is
  `func(ctx context.Context) error`, the same decoupled shape
  `machine.Guard` and phase 30's `RetryPolicy.Sleep` already use for a
  caller-supplied closure. A caller wraps `agent.Run`, `flow.Run`, or
  `tools.Registry.Run` in a closure matching that signature; `Job`
  itself never imports any of the three. `scheduler`'s
  `policy/layers.json` row lists `events` only.
- A cron-expression parser or any string-based schedule syntax. No
  caller in this module needs one today. A caller who wants cron
  syntax writes their own type that satisfies `Schedule`; `scheduler`
  ships two schedules and no parser, following the same
  no-speculative-generality reasoning `docs/plans/channel.md` and
  phase 23's rejected `Catch` field already use in this repo.
- A distributed or persistent job store. `Scheduler` is in-process,
  like `flow`'s runner; a job list lives in memory only and does not
  survive a process restart. A caller who needs durability re-`Add`s
  every job after `New`, the same way a caller re-supplies `flow`'s
  `Definition` on every process start.
- Holding an `events.Bus` as a `Scheduler` field. `heartbeat.Monitor`
  never holds a `Bus`; `flow.Run` takes `bus *events.Bus` as a
  parameter, not a field. `scheduler.Scheduler.Run` follows the same
  parameter shape, so a `Scheduler` value stays bus-agnostic between
  calls and a caller can run it with a different bus, or no bus, on
  each call.
- Retrying a failed `Job`. A `Job` failure is a fact about one firing,
  not a reason to stop or replay the schedule; see the failure-policy
  section below. A caller who wants a `Job` to retry its own internal
  work wraps `flow.Run` and phase 30's `RetryPolicy`, or writes retry
  logic inside the closure itself.

### Prior art: the `Schedule` interface shape

`type Schedule interface { Next(after time.Time) time.Time }` matches
the shape the Go community's `robfig/cron` package uses for its own
`Schedule` interface, cited here as prior art the way phase 10's
`a2aclient` plan cites the a2a-go client library it wraps. This SDK
ships no cron-expression parser; `robfig/cron`'s parser is exactly the
piece this plan declines to build, per the Scope section above. The
interface shape alone, one pure function from "after what time" to
"when next," is a proven, minimal way to decouple the calendar
question from the firing mechanism, and this plan reuses only that
shape.

### The failure policy

Three options were weighed for a `Job` that returns a non-nil error.

- Stop the `Scheduler`. Rejected. One job's failure would silently
  cancel every other job's future firings, with no way for a caller
  to tell the difference between "the process is shutting down" and
  "one job broke." A recurring scheduler that stops on the first
  error defeats the purpose of scheduling independent, recurring
  work.
- Swallow the error with no signal. Rejected. A caller with no
  visibility into a failing job cannot fix it or alert a human.
  `flow.Run`'s own `Confirm` and `Fire` failures already surface
  through a returned error; silently dropping a `Job` error would be
  a strictly worse contract than an already-shipped package in this
  module.
- Emit a typed event on an optional `*events.Bus`, and keep running
  every other job's schedule. Chosen. This matches `flow`'s own
  `emitStep`: a nil `bus` silently skips the emit, and `Emit`'s own
  return value is ignored, because "the caller owns the bus and
  decides what to log," in `flow/runner.go`'s own words. It also
  matches `heartbeat`'s justification for its `events` import: one
  typed `Name` constant, no held `Bus`. A caller who wants a `Job`
  failure to escalate further subscribes a handler to
  `JobFailedEvent` and does the escalating itself, for example
  through phase 37's `channel.Notifier`, composed entirely in caller
  code with no import from `scheduler` to `channel`.

## API

The surface below lands in `api/scheduler.txt` once this phase builds.

- `type Job func(ctx context.Context) error` — a caller-supplied
  invocable: an agent run, a flow run, a tool call, or any closure.
  `scheduler` ships no implementation and imports no package whose
  type a `Job` might wrap.
- `type Schedule interface { Next(after time.Time) time.Time }` — the
  next fire time strictly after `after`. A `Schedule` that has no
  further firing returns the zero `time.Time`; `Scheduler` reads a
  zero result as "this entry never fires again" and stops scheduling
  it, without an error.
- `func Every(d time.Duration) Schedule` — a fixed-interval schedule.
  `Next(after)` returns `after.Add(d)`. `d` must be positive; `Every`
  panics on a non-positive `d`, matching the standard library's own
  `time.NewTicker`, which panics on a non-positive duration for the
  same reason: a zero or negative interval has no sane firing rate.
- `func At(times ...time.Time) Schedule` — a fixed, one-shot set of
  fire times. `Next(after)` returns the earliest entry in `times`
  strictly after `after`, or the zero `time.Time` once every entry is
  spent. `At` copies `times` and sorts the copy, so caller mutation of
  the input slice cannot change the schedule.
- `type Scheduler struct` — holds a job set. Built only through `New`.
  Safe for concurrent `Add`, `Remove`, and `Run`; a mutex guards the
  job map, matching `tools.Registry`'s concurrency shape.
- `func New() *Scheduler` — creates an empty `Scheduler`.
- `func (s *Scheduler) Add(id string, sched Schedule, job Job) error`
  — registers `job` under `id`, to fire at each time `sched.Next`
  reports, starting from `time.Now()` at `Add`'s call time. Rejects a
  blank `id` (empty after `strings.TrimSpace`), a nil `sched`, a nil
  `job`, and a duplicate `id`, each with its own sentinel error,
  mirroring `tools.Registry.Add`'s per-field rejection shape.
- `func (s *Scheduler) Remove(id string) bool` — removes `id`.
  Returns whether `id` was present, matching
  `tools.Registry.Remove`'s exact contract: removing an absent `id`
  is not a fault.
- `func (s *Scheduler) Run(ctx context.Context, bus *events.Bus) error`
  — blocks, firing each registered job at its next scheduled time,
  until `ctx` is canceled or expires. Returns `ctx.Err()` on that
  exit. `Run` fires each due job in its own goroutine and waits for
  every in-flight goroutine to finish before it returns, so `Run`
  never leaks a goroutine past its own return. A `bus` of nil means no
  event emits; see the failure policy above. `Run` is safe to call
  once per `Scheduler` value at a time; a second concurrent `Run` call
  on the same `Scheduler` is caller error and is not defended against,
  matching `flow.Run`'s own single-caller assumption.
- `const JobFailedEvent events.Name = "scheduler.job_failed"` — the
  event `Run` emits when a `Job` returns a non-nil error. `Run` emits
  it with `Data` naming the failing job's `id` and the error text,
  mirroring `flow.StepCompletedEvent`'s one-line `Data` shape.
- Sentinel errors: `ErrBlankID`, `ErrNilSchedule`, `ErrNilJob`,
  `ErrDuplicateID` — returned by `Add`, tested with `errors.Is`.

## The `Run` loop, in prose

`Run` computes the earliest `Next` across every registered entry and
sleeps until that time or until `ctx.Done()`, whichever comes first,
using a `time.Timer`. A due entry fires in its own goroutine, tracked
by a `sync.WaitGroup`; the goroutine calls `job(ctx)`, and on a
non-nil error, emits `JobFailedEvent` on `bus` when `bus` is non-nil.
Firing a due entry recomputes that entry's own next fire time through
`sched.Next(now)` before the loop sleeps again; an entry whose `Next`
returns the zero time is dropped from future scheduling, matching
`At`'s exhaustion behavior. `Add` called while `Run` is blocked in its
sleep wakes the loop early, through a small buffered signal channel,
so a newly added entry with an earlier fire time than the current
sleep is not missed. On `ctx.Done()`, `Run` stops sleeping, stops
firing new entries, and returns only after its `sync.WaitGroup.Wait()`
call unblocks, guaranteeing every already-started `Job` goroutine has
returned before `Run` itself returns.

## Tests

Test files live in `scheduler/scheduler_test/`, following `PHASES.md`'s
flat test layout.

- `schedule_test.go` — red-green cases for `Every` and `At`:
  `Every(d).Next(t)` returns `t.Add(d)` for several `d` values;
  `Every` panics on a zero and a negative `d`, asserted with
  `recover`. `At(times...).Next(t)` returns the earliest entry after
  `t`, the zero time once every entry is behind `t`, and the correct
  entry when `times` is passed out of order, proving `At` sorts its
  copy. `At()` with no arguments always returns the zero time.
- `scheduler_add_test.go` — red-green cases for `Add`: a blank `id`
  returns `ErrBlankID`; a nil `sched` returns `ErrNilSchedule`; a nil
  `job` returns `ErrNilJob`; a duplicate `id` returns `ErrDuplicateID`;
  a fully valid call returns nil. `Remove` returns true for a present
  `id` and false for an absent one, in both orders (add-then-remove,
  remove-with-nothing-added).
- `scheduler_run_test.go` — red-green cases for `Run`, using a short
  `Every` interval and a recording `Job` that appends to a slice
  behind a mutex:
  - `Run` fires a job at least twice within a bounded wall-clock
    window sized to the test's own `Every` interval, then cancels
    `ctx` and asserts `Run` returns `context.Canceled`.
  - A `Job` that returns a non-nil error does not stop `Run`; a second
    `Job`, added separately, keeps firing on its own schedule.
  - A failing `Job`, with a subscribed bus, emits `JobFailedEvent`
    with `Data` naming the job's `id`; assert with a `Subscribe`d
    handler that records the event.
  - A failing `Job`, with a nil bus, causes no panic and no emit;
    `Run` keeps firing that job's later occurrences.
  - `Add` called after `Run` has started, targeting an `At` time
    earlier than any currently scheduled entry, still fires at its
    own time, proving the wake-on-`Add` signal reaches a blocked
    sleep.
  - An `At` schedule with every entry already in the past never
    fires; `Run` cancels cleanly on `ctx` with zero recorded calls
    for that entry.
- `scheduler_leak_test.go` — the goroutine-leak case `PHASES.md`'s
  benchmark contract calls out as a named risk for a Go scheduler.
  Records `runtime.NumGoroutine()` before `Run` starts, runs `Run`
  with several `Every` jobs and a short-lived `ctx`, and asserts the
  goroutine count returns within a small margin of the starting count
  shortly after `Run` returns, retrying the check a few times to
  absorb the Go runtime's own scheduling noise. This is the
  documented alternative check `PHASES.md` allows in place of a
  literal `AllocsPerRun` budget, because a goroutine count depends on
  runtime scheduling, not allocation.
- `scheduler_run_integration_test.go` — runs a `Scheduler` with two
  `Every` jobs and one `At` job against a real `events.Bus`,
  end to end, for a bounded wall-clock window. Asserts the `At` job
  fires exactly once, the two `Every` jobs each fire more than once,
  and a subscribed `JobFailedEvent` handler observes exactly the
  failing job's own failures, with no cross-job event leakage.
- `schedule_bench_test.go` — benchmark `Every(d).Next` and
  `At(times...).Next` against a fifty-entry `times` slice. Report
  ns/op and set an `AllocsPerRun` budget, since both are pure
  functions with no goroutine or channel overhead.

## Verification

`make verify` passes, once this phase's code lands. The coverage floor
for `scheduler` holds at 85 or above. `api/scheduler.txt` is created
by `make api-update` in this phase's own change, locking `Job`,
`Schedule`, `Every`, `At`, `Scheduler`, `New`, `Add`, `Remove`, `Run`,
`JobFailedEvent`, and the four sentinel errors. `policy/layers.json`
gains a `scheduler` row set to `["events"]`, added by this plan ahead
of the code, matching the gate's own rule that a new package needs a
row before it has code. `scripts/check_deps.py` passes with no edge
from `scheduler` to `agent`, `flow`, or `tools`, and no edge from any
of those three to `scheduler` (composition happens in caller code).
`scripts/check_plan.py` passes once `docs/plans/scheduler.md` exists,
written from `docs/plans/TEMPLATE.md`, folding this phase's design
into the package's own plan the way phase 14's plan absorbed phase
31's design once phase 31 landed.

`go test -race ./scheduler/...` passes for the concurrent `Run`,
`Add`, and `Remove` paths. `AGENTS.md`'s package layout list gains a
`scheduler/` bullet, matching the existing bullets' level of detail:
package name, one-sentence purpose, and its import edges (`events`).
