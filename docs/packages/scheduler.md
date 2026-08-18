# Package reference: scheduler

The scheduler package invokes a caller-supplied `Job` on a schedule.
`scheduler` decides when to run something; it never decides what that
something is. The exported surface below mirrors `api/scheduler.txt`.

## Types

- `Job` — `func(ctx context.Context) error`, a caller-supplied
  invocable: an agent run, a flow run, a tool call, or any closure.
  `scheduler` ships no implementation and imports no package whose
  type a `Job` might wrap.
- `Schedule` — `interface { Next(after time.Time) time.Time }`, the
  next fire time strictly after `after`. A `Schedule` with no further
  firing returns the zero `time.Time`. `Run` reads a zero result as
  "this entry never fires again" and stops scheduling it, without an
  error.
- `Scheduler` — holds a job set. Built only through `New`. Safe for
  concurrent `Add`, `Remove`, and `Run`.

## Constants

- `JobFailedEvent` — the event kind `Run` emits when a `Job` returns a
  non-nil error. It is an `events.Name` constant.

## Functions and methods

- `Every(d)` — a fixed-interval `Schedule`. `Next(after)` returns
  `after.Add(d)` for a positive `d`. A non-positive `d` returns a
  `Schedule` whose `Next` always reports the zero `time.Time`, so the
  entry registers but never fires.
- `At(times...)` — a fixed, one-shot `Schedule`. `Next(after)` returns
  the earliest entry in `times` strictly after `after`, or the zero
  `time.Time` once every entry is spent. `At` copies `times` and sorts
  the copy, so caller mutation of the input slice cannot change the
  schedule.
- `New()` — creates an empty `Scheduler`.
- `Scheduler.Add(id, sched, job)` — registers `job` under `id`, to
  fire at each time `sched.Next` reports. Rejects a blank `id`, a nil
  `sched`, a nil `job`, and a duplicate `id`, each with its own
  sentinel error.
- `Scheduler.Remove(id)` — removes `id`. Returns whether `id` was
  present; removing an absent id is not a fault.
- `Scheduler.Run(ctx, bus)` — blocks, firing each registered job at
  its next scheduled time, until `ctx` is canceled or expires. Returns
  `ctx.Err()` on that exit. Fires each due job in its own goroutine and
  waits for every in-flight goroutine to finish before it returns. A
  nil `bus` means no event emits.

## Failure modes

Use `errors.Is` to test these.

- `ErrBlankID` ("scheduler: id must not be blank") — `Add` returns it
  when `id` is empty after `strings.TrimSpace`. Pinned by
  `scheduler/scheduler_test/scheduler_add_test.go`.
- `ErrNilSchedule` ("scheduler: schedule must not be nil") — `Add`
  returns it when `sched` is nil. Pinned by
  `scheduler/scheduler_test/scheduler_add_test.go`.
- `ErrNilJob` ("scheduler: job must not be nil") — `Add` returns it
  when `job` is nil. Pinned by
  `scheduler/scheduler_test/scheduler_add_test.go`.
- `ErrDuplicateID` ("scheduler: id already registered") — `Add` returns
  it on a second `Add` call with the same `id`. Pinned by
  `scheduler/scheduler_test/scheduler_add_test.go`.

## Invariants

- `Add` never calls `sched.Next` itself. It registers the entry
  pending; `Run`'s own loop goroutine computes the first
  `Next(time.Now())` the next time that loop wakes. Every `Next` call
  funnels through `Run`'s single loop goroutine, even when a caller
  shares one stateful `Schedule` value across multiple `Add` calls.
- `Add` called while `Run` is blocked in its sleep wakes the loop
  early through a non-blocking send on an internal wake channel.
- `Run` fires a due job in its own goroutine, so a slow `Job` never
  blocks the scheduling loop. `Run` waits for every in-flight goroutine
  before it returns, so it never leaks a goroutine past its own
  return.
- `Run` is safe to call once per `Scheduler` value at a time. A second
  concurrent `Run` call on the same `Scheduler` is caller error and is
  not defended against.
- A failing `Job` emits `JobFailedEvent` on the supplied `bus`, naming
  the job's id in `Event.Data`, and does not stop other jobs from
  firing on their own schedule. A nil `bus` skips the emit silently.

## Cross-references

- [events.md](events.md) — `Run` takes an optional `*events.Bus` and
  emits `JobFailedEvent` through it.

## Usage

```go
s := scheduler.New()
tick := func(ctx context.Context) error {
    fmt.Println("tick")
    return nil
}
if err := s.Add("tick", scheduler.Every(time.Second), tick); err != nil {
    // id blank, schedule nil, job nil, or id already registered
}

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
_ = s.Run(ctx, nil) // returns ctx.Err() once the timeout expires
```
