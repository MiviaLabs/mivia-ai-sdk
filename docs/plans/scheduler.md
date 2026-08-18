# Plan: scheduler

Status: shipped. One new package, `scheduler`, importing `events`
only, for one typed event-name constant, matching `heartbeat`'s
precedent.

## Goal

Give this SDK one generic, reusable way to invoke anything on a
schedule: an agent run, a flow run, a tool call, or any other closure
a caller supplies. `scheduler` decides when to run something. It
never decides what that something is.

## Scope

Inside: the `Job` function type, the `Schedule` interface, two
concrete schedules (`Every`, a fixed interval, and `At`, a fixed set
of times), the `Scheduler` type, and its `Add`, `Remove`, and `Run`
methods. `scheduler` is a leaf package for composition purposes. It
imports no other internal package's invocable type. It imports
`events` only, for one typed event-name constant, matching
`heartbeat`'s precedent.

Outside:

- Any coupling to `agent`, `flow`, or `tools`. `Job` is
  `func(ctx context.Context) error`, the same decoupled closure shape
  `machine.Guard` already uses: `type Guard func(ctx context.Context)
  (bool, error)`. A caller wraps `agent.Run`, `flow.Run`, or
  `tools.Registry.Run` in a closure matching that signature. `Job`
  itself never imports any of the three. `scheduler`'s
  `policy/layers.json` row lists `events` only.
- A cron-expression parser or any string-based schedule syntax. No
  caller in this module needs one today. A caller who wants cron
  syntax writes their own type that satisfies `Schedule`. `scheduler`
  ships two schedules and no parser, following the same
  no-speculative-generality reasoning as `docs/plans/channel.md`.
- A distributed or persistent job store. `Scheduler` is in-process,
  like `flow`'s runner. A job list lives in memory only and does not
  survive a process restart. A caller who needs durability re-`Add`s
  every job after `New`, the same way a caller re-supplies `flow`'s
  `Definition` on every process start.
- Holding an `events.Bus` as a `Scheduler` field. `heartbeat.Monitor`
  never holds a `Bus`. `flow.Run` takes `bus *events.Bus` as a
  parameter, not a field. `scheduler.Scheduler.Run` follows the same
  parameter shape, so a `Scheduler` value stays bus-agnostic between
  calls. A caller can run it with a different bus, or no bus, on each
  call.
- Retrying a failed `Job`. A `Job` failure is a fact about one firing,
  not a reason to stop or replay the schedule. See "The failure
  policy" below. A caller who wants a `Job` to retry its own internal
  work writes retry logic inside the closure itself, or wraps
  `flow.Run` and its `RetryPolicy` (shipped by phase 30; see
  `docs/plans/flow.md`).

### Prior art: the `Schedule` interface shape

`type Schedule interface { Next(after time.Time) time.Time }` matches
the shape the Go community's `robfig/cron` package uses for its own
`Schedule` interface, the way `a2aclient`'s plan cites the a2a-go
client library it wraps. This SDK ships no cron-expression parser.
`robfig/cron`'s parser is exactly the piece this plan declines to
build, per the Scope section above. The interface shape alone, one
pure function from "after what time" to "when next," is a proven,
minimal way to decouple the calendar question from the firing
mechanism. This plan reuses only that shape.

### The failure policy

Three options were weighed for a `Job` that returns a non-nil error.

- Stop the `Scheduler`. Rejected. One job's failure would silently
  cancel every other job's future firings, with no way for a caller
  to tell "the process is shutting down" from "one job broke." A
  recurring scheduler that stops on the first error defeats the
  purpose of scheduling independent, recurring work.
- Swallow the error with no signal. Rejected. A caller with no
  visibility into a failing job cannot fix it or alert a human.
  `flow.Run`'s own `Confirm` and `Fire` failures already surface
  through a returned error. Silently dropping a `Job` error would be
  a strictly worse contract than an already-shipped package in this
  module.
- Emit a typed event on an optional `*events.Bus`, and keep running
  every other job's schedule. Chosen. This matches `flow`'s own
  `emitStep`: a nil `bus` silently skips the emit, and `Emit`'s own
  return value is ignored, because the caller owns the bus and
  decides what to log, in `flow/runner.go`'s own words. It also
  matches `heartbeat`'s justification for its `events` import: one
  typed `Name` constant, no held `Bus`. A caller who wants a `Job`
  failure to escalate further subscribes a handler to
  `JobFailedEvent` and does the escalating itself, for example
  through `channel.Notifier`, composed entirely in caller code with
  no import from `scheduler` to `channel`.

## API

The surface below is locked in `api/scheduler.txt` by `make api-update`.

- `type Job func(ctx context.Context) error` — a caller-supplied
  invocable: an agent run, a flow run, a tool call, or any closure.
  `scheduler` ships no implementation and imports no package whose
  type a `Job` might wrap.
- `type Schedule interface { Next(after time.Time) time.Time }` — the
  next fire time strictly after `after`. A `Schedule` that has no
  further firing returns the zero `time.Time`. `Scheduler` reads a
  zero result as "this entry never fires again" and stops scheduling
  it, without an error. `Run` calls `Next` only from its own single
  loop goroutine. An implementation need not guard against concurrent
  `Next` calls.
- `func Every(d time.Duration) Schedule` — a fixed-interval schedule.
  `Next(after)` returns `after.Add(d)` for a positive `d`. A
  non-positive `d` has no sane firing rate, matching the standard
  library's own `time.NewTicker`, which panics for the same reason.
  This module's `no-panic-in-packages` gate forbids a panic in any
  package, so `Every` returns a `Schedule` whose `Next` always reports
  the zero `time.Time` instead: the entry registers but never fires.
- `func At(times ...time.Time) Schedule` — a fixed, one-shot set of
  fire times. `Next(after)` returns the earliest entry in `times`
  strictly after `after`, or the zero `time.Time` once every entry is
  spent. `At` copies `times` and sorts the copy, so caller mutation of
  the input slice cannot change the schedule.
- `type Scheduler struct` — holds a job set. Built only through `New`.
  Safe for concurrent `Add`, `Remove`, and `Run`. A mutex guards the
  job map, matching `tools.Registry`'s concurrency shape.
- `func New() *Scheduler` — creates an empty `Scheduler`.
- `func (s *Scheduler) Add(id string, sched Schedule, job Job) error`
  — registers `job` under `id`, to fire at each time `sched.Next`
  reports. `Add` never calls `sched.Next` itself; `Run`'s own loop
  goroutine computes the first `Next(time.Now())` the next time it
  wakes, so every `Next` call funnels through that one goroutine, even
  when a caller shares one stateful `Schedule` value across `Add`
  calls. Rejects a blank `id` (empty after `strings.TrimSpace`), a nil
  `sched`, a nil `job`, and a duplicate `id`, each with its own
  sentinel error, mirroring `tools.Registry.Add`'s per-field rejection
  shape.
- `func (s *Scheduler) Remove(id string) bool` — removes `id`.
  Returns whether `id` was present, matching
  `tools.Registry.Remove`'s exact contract: removing an absent `id`
  is not a fault.
- `func (s *Scheduler) Run(ctx context.Context, bus *events.Bus) error`
  — blocks, firing each registered job at its next scheduled time,
  until `ctx` is canceled or expires. Returns `ctx.Err()` on that
  exit. `Run` fires each due job in its own goroutine and waits for
  every in-flight goroutine to finish before it returns, so `Run`
  never leaks a goroutine past its own return. A nil `bus` means no
  event emits; see "The failure policy" above. `Run` is safe to call
  once per `Scheduler` value at a time. A second concurrent `Run` call
  on the same `Scheduler` is caller error and is not defended against,
  matching `flow.Run`'s own single-caller assumption.
- `const JobFailedEvent events.Name = "scheduler.job_failed"` — the
  event `Run` emits when a `Job` returns a non-nil error. `Run` emits
  it with `Data` set to `fmt.Sprintf("job %s failed: %v", id, err)`,
  mirroring `flow.emitStep`'s `fmt.Sprintf("step %s completed", id)`
  style.
- Sentinel errors, tested with `errors.Is`, mirroring
  `heartbeat.ErrNoID`'s and `tools.ErrBlankName`'s pinned-text style:
  - `ErrBlankID = errors.New("scheduler: id must not be blank")`
  - `ErrNilSchedule = errors.New("scheduler: schedule must not be nil")`
  - `ErrNilJob = errors.New("scheduler: job must not be nil")`
  - `ErrDuplicateID = errors.New("scheduler: id already registered")`

### The `Run` loop, in prose

`Run` computes the earliest `Next` across every registered entry and
sleeps until that time or until `ctx.Done()`, whichever comes first,
using a `time.Timer`. A due entry fires in its own goroutine, tracked
by a `sync.WaitGroup`. The goroutine calls `job(ctx)`, and on a
non-nil error, emits `JobFailedEvent` on `bus` when `bus` is non-nil.
Firing a due entry recomputes that entry's own next fire time through
`sched.Next(now)` before the loop sleeps again. An entry whose `Next`
returns the zero time is dropped from future scheduling, matching
`At`'s exhaustion behavior. `Add` called while `Run` is blocked in its
sleep wakes the loop early, through a wake channel of capacity 1. `Add`
sends on the wake channel with a non-blocking `select` guarded by a
`default` case, outside the mutex-held critical section. A dropped
wake is harmless: the sleep timer still expires on its own, at worst
waiting out the old, now-stale sleep duration before `Run` re-reads
the job set. This bounds the wake channel's blast radius to one late
wake, never a deadlock. A newly added entry with an earlier fire time
than the current sleep is not missed except by that bounded delay. On
`ctx.Done()`, `Run` stops sleeping, stops
firing new entries, and returns only after its `sync.WaitGroup.Wait()`
call unblocks, guaranteeing every already-started `Job` goroutine has
returned before `Run` itself returns.

### Source files

Following `heartbeat`'s precedent of listing files up front, so the
500/80-line structure gate is a design decision, not an afterthought:

- `scheduler/doc.go` — package doc and file map, mirroring
  `heartbeat/doc.go`'s style.
- `scheduler/schedule.go` — `Schedule`, `Every`, `At`.
- `scheduler/scheduler.go` — `Scheduler`, `New`, `Add`, `Remove`, the
  sentinel errors.
- `scheduler/run.go` — `Run` and its wake-channel sleep loop.
- `scheduler/events.go` — the `JobFailedEvent` constant, one line,
  mirroring `flow/events.go`'s and `heartbeat/events.go`'s style.

## Tests

Test files live in `scheduler/scheduler_test/`, following this
repo's flat test layout.

- `schedule_test.go` — red-green cases for `Every` and `At`:
  `Every(d).Next(t)` returns `t.Add(d)` for several `d` values;
  `TestEveryNonPositiveNeverFires` covers a zero and a negative `d`:
  `Every` returns a `Schedule` whose `Next` always reports the zero
  `time.Time`, so the entry registers but never fires, the smallest
  defensible deviation from `time.NewTicker`'s panic given this
  module's no-panic gate. `At(times...).Next(t)` returns the earliest
  entry after `t`, the zero time once every entry is behind `t`, and the correct
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
  - A `Job` that returns a non-nil error does not stop `Run`. A second
    `Job`, added separately, keeps firing on its own schedule.
  - A failing `Job`, with a subscribed bus, emits `JobFailedEvent`
    with `Data` naming the job's `id`; assert with a `Subscribe`d
    handler that records the event.
  - A failing `Job`, with a nil bus, causes no panic and no emit.
    `Run` keeps firing that job's later occurrences.
  - `Add` called after `Run` has started, targeting an `At` time
    earlier than any currently scheduled entry, still fires at its
    own time, proving the wake-on-`Add` signal reaches a blocked
    sleep.
  - An `At` schedule with every entry already in the past never
    fires. `Run` cancels cleanly on `ctx` with zero recorded calls
    for that entry.
  - `Remove` called mid-`Run` stops that job's future firings. Add a
    job on a short `Every` interval, let it fire once, then `Remove`
    it while `Run` is still blocked. Assert no further firings for
    that job before `ctx` is canceled.
  - `Run`, started under a real `context.WithTimeout`, returns
    `context.DeadlineExceeded` once the deadline passes. This is
    separate from the explicit-cancel case above, which asserts
    `context.Canceled`.
- `scheduler_concurrent_test.go` — a concurrency-stress test, matching
  `tools.Registry`'s and `heartbeat.Monitor`'s own dedicated
  `_concurrent_test.go` precedent. Many goroutines call `Add`,
  `Remove`, and a running `Run` concurrently on one `Scheduler`, under
  `go test -race`. Asserts no data race and no panic. This backs the
  Verification section's `-race` claim for `Run`, `Add`, and `Remove`.
- `scheduler_leak_test.go` — the goroutine-leak case a Go scheduler
  must guard against. Records `runtime.NumGoroutine()` before `Run`
  starts, runs `Run` with several `Every` jobs and a short-lived
  `ctx`, and asserts the goroutine count returns within a small
  margin of the starting count shortly after `Run` returns, retrying
  the check a few times to absorb the Go runtime's own scheduling
  noise. This stands in for a literal `AllocsPerRun` budget, because a
  goroutine count depends on runtime scheduling, not allocation.
- `scheduler_run_integration_test.go` — runs a `Scheduler` with two
  `Every` jobs and one `At` job against a real `events.Bus`, end to
  end, for a bounded wall-clock window. Asserts the `At` job fires
  exactly once, the two `Every` jobs each fire more than once, and a
  subscribed `JobFailedEvent` handler observes exactly the failing
  job's own failures, with no cross-job event leakage.
- `schedule_bench_test.go` — benchmark `Every(d).Next` and
  `At(times...).Next` against a fifty-entry `times` slice. Report
  ns/op and set an `AllocsPerRun` budget, since both are pure
  functions with no goroutine or channel overhead.

## Verification

`make verify` passes. The coverage floor for `scheduler` holds at 85
or above. `api/scheduler.txt` is
created by `make api-update` in the same change, locking `Job`,
`Schedule`, `Every`, `At`, `Scheduler`, `New`, `Add`, `Remove`, `Run`,
`JobFailedEvent`, and the four sentinel errors.

`policy/layers.json` carries a `scheduler` row set to `["events"]`,
added ahead of the code, matching the gate's own rule that a new
package needs a row before it has code. `scripts/check_deps.py`
passes with no edge from `scheduler` to `agent`, `flow`, or `tools`,
and no edge from any of those three to `scheduler`; composition
happens in caller code only.

`go test -race ./scheduler/...` passes for the concurrent `Run`,
`Add`, and `Remove` paths. `AGENTS.md`'s package layout list gains a
`scheduler/` bullet, matching the existing bullets' level of detail:
package name, one-sentence purpose, and its import edges (`events`).
