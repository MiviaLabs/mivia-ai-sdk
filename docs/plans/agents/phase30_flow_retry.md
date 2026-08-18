# Phase 30: flow retry

Status: shipped. `RetryPolicy`, `RetryPolicy.Validate`,
`RetryPolicy.NextDelay`, and `Step.Retry` all exist in `flow`'s code
and locked API; see `docs/plans/flow.md`'s Phase 30 subsection.
Phases 21, 22, and 23 shipped first: `Outcome`, `Report`, `Admission`,
`Route`, `Failure`, and `FailureFrom` all exist in `flow`'s code and
locked API. This phase kept every one of `Admission`, `Route`, and
`FailureFrom`'s behaviors intact.

## Goal

Let a step retry its own `Fire` call a bounded number of times, with
exponential backoff, before the run treats it as failed. Keep the
retry loop deterministic and testable. Keep phase 23's fallback the
outer safety net for a step that exhausts its retries.

## Scope

Inside: the `RetryPolicy` type, its `Validate` method, its
`NextDelay` method, the `Step.Retry` field, the retry loop, and the
new `New` validations. `RetryPolicy`, `Validate`, `NextDelay`, and
`fireWithRetry` land in a new file, `flow/retry.go`. `flow/runner.go`
is already at 500 lines, the structure gate's cap, so this phase adds
no retry code to it. `runSingleton`'s only change is a one-line
call-site swap: its existing `fireStep(fireCtx, m, cur, rec, step,
row)` call becomes `fireWithRetry(fireCtx, m, cur, rec, step, row)`,
the same six arguments, so the swap adds zero net lines to
`runner.go`. `fireWithRetry` decides inside `retry.go` whether
`step.Retry` is nil; a nil `Retry` calls `fireStep` once and returns
its result unchanged, matching today's behavior exactly, so
`runSingleton` calls `fireWithRetry` unconditionally and needs no
branch of its own. If a later edit to `runner.go` in this phase needs
even one more line than that swap, offset it by moving
`fireFromChild` (`runner.go`, the chained-step fire helper) into
`flow/failure.go`, next to the `fireStep` and `pickTransitionFor`
helpers it already calls; that keeps `runner.go` at or under 500
lines in the same change. This phase extends the existing `flow`
package. It adds no new top-level package, so `policy/layers.json`
gains no new row. The `flow` row already lists `events` and
`machine`; this phase imports only the standard library `time`
package inside `flow`, which needs no policy entry.

Outside:

- Cross-run persistence of retry state. A retry count lives only in
  the `runSingleton` call stack for the run in progress. No ledger, no
  checkpoint field. Durable execution is a separate, later concern.
- Compensation and rollback. Phase 23 already declined that scope. A
  step that exhausts its retries is a plain `Fire` failure; the
  fallback path in phase 23 is the only recovery mechanism.
- Retry on `Confirm` rejection. Phase 23 states a `Confirm` rejection
  aborts the run and no fallback catches it. Retry follows the same
  rule: it wraps only the `Fire` call, never `Confirm`.
- Retry on a `Route` error. `Route` is scheduling, not step work, by
  phase 22's own rule. A `Route` error stays a single-shot failure.
- Retry on a panel member or a chained (`Sub`) step. A panel wave
  fires every member's `Fire` inside its own goroutine on a single
  shared clock tick; a per-member retry loop would desynchronize the
  wave's shared transition row. A chained step's `Fire` call sits
  behind a whole child workflow; retrying it would silently re-run
  that child workflow, which is a materially bigger operation than
  retrying one transition. `New` rejects both shapes at build time,
  mirroring the panel and chained-step rejections phases 22 and 23
  already use for `Route` and `AdmissionOnFailed`.
- A default jitter source. `RetryPolicy.Jitter` is an optional hook,
  not a built-in random generator. See Determinism below.

### Determinism and testability

`RetryPolicy` never calls `time.Sleep` directly to compute a duration;
`NextDelay` is a pure function of the attempt number and the policy's
own fields. `Run`'s retry loop calls the policy's `Sleep` field to
wait between attempts. `Sleep` defaults to a context-aware sleep when
the field is nil, described under Context cancellation below. A test
supplies a recording or no-op `Sleep` function, so a retry test never
blocks on real wall-clock time and stays deterministic.

A retried step's `Guard`, `OnExit`, and `OnEntry` closures must
tolerate repeated invocation: `fireWithRetry` calls `fireStep`
(`flow/failure.go`), which calls `m.Fire`, and so all three closures,
once per attempt, with no de-duplication. A closure with a side
effect that is not safe to repeat (for example, an un-idempotent
external call) must guard its own idempotency; `flow` provides no
dedup layer, matching the existing panel-aliasing caveat that pushes
fan-out safety onto the caller's closures.

### Context cancellation

`Sleep`'s signature is `func(context.Context, time.Duration)`, matching
every other ctx-threaded hook in the loop (`Guard`, `OnExit`,
`OnEntry`, `Confirm` all take `ctx` today). `fireWithRetry` passes the
run's own `ctx` to `Sleep` on every call. The default `Sleep`, used
when the field is nil, selects on `ctx.Done()` and a `time.Timer` for
the computed duration; a canceled `ctx` returns at once with the
context's error instead of waiting out the full backoff. A canceled
`ctx` during a pending sleep aborts the retry loop the same way a
canceled `ctx` aborts `fireStep`, and the `m.Fire` call inside it,
today: `fireWithRetry` returns the context error without waiting for
`MaxAttempts` to run out.

`Jitter` follows the same shape: an optional
`func(time.Duration) time.Duration` field. A nil `Jitter` means
`NextDelay` returns the pure exponential backoff with no randomness, so
the default behavior is fully deterministic. A caller who wants jitter
supplies a closure over their own random source; `flow` adds no
`math/rand` dependency and picks no seeding strategy on the caller's
behalf.

## API

The surface below lands in `api/flow.txt` via `make api-update`.

- `type RetryPolicy struct { MaxAttempts int; BaseDelay time.Duration; MaxDelay time.Duration; Retryable func(error) bool; Jitter func(time.Duration) time.Duration; Sleep func(context.Context, time.Duration) }`
  — a step's retry rule. `MaxAttempts` counts every attempt, including
  the first; a value of 1 disables retry. `BaseDelay` is the first
  retry's backoff. `MaxDelay` clamps every computed backoff, so the
  exponential term cannot overflow `time.Duration`'s range.
  `Retryable`, when non-nil, gates each failure before the next
  attempt; a nil `Retryable` retries every error. `Jitter` and `Sleep`
  are the determinism hooks described above; `Sleep` takes the run's
  `ctx` so a caller can cancel a pending backoff, described under
  Context cancellation below.
- `func (p RetryPolicy) Validate() error` — enforces `MaxAttempts >= 1`
  and `MaxDelay > 0`, with the pinned messages below. `New` calls this
  for every step whose `Retry` is non-nil.
- `func (p RetryPolicy) NextDelay(attempt int) time.Duration` — the
  backoff before the given retry attempt, one-indexed from the first
  retry. Doubles `delay` from `BaseDelay` one step per attempt above 1,
  but checks the bound before each doubling instead of after: when
  `delay > MaxDelay>>1`, one more doubling would reach or overflow
  `MaxDelay`, so `NextDelay` sets `delay` to `MaxDelay` and stops
  doubling, without ever performing the overflow-prone multiply. A
  final clamp to `MaxDelay` then covers the case where `BaseDelay`
  itself already exceeds `MaxDelay`. This clamp-before-double order
  makes overflow structurally impossible for the pre-`Jitter` value:
  `delay` never exceeds `MaxDelay` at the start of a doubling step, so
  the doubled value never exceeds `2*MaxDelay`, well inside
  `time.Duration`'s range for any `MaxDelay` the `Validate` bound
  accepts. Pure; no field mutation, no sleep, no randomness of its
  own. `Jitter`, when non-nil, applies to the clamped result last, and
  `NextDelay` does not re-clamp its output: a `Jitter` closure that
  returns a value above `MaxDelay` passes through unclamped, by
  design. Re-clamping after `Jitter` is the caller's responsibility if
  the caller's `Jitter` closure needs that bound; `flow` states this
  as a documented limit, not a gap to close in this phase.
- `Step` gains `Retry *RetryPolicy`. Nil means no retry, matching
  today's single-attempt behavior exactly.

`New` gains four validations, with pinned messages:

- `flow: step %q retry: max attempts must be at least 1` — a
  `RetryPolicy` with `MaxAttempts < 1`.
- `flow: step %q retry: max delay must be positive` — a `RetryPolicy`
  with `MaxDelay <= 0`. The clamp must always bound the exponential
  term.
- `flow: step %q has a retry policy but a sub-workflow` — `Retry` set
  on a step whose `Sub` is non-nil.
- `flow: panel %d names retried step %q` — `Retry` set on a panel
  member.

### The retry loop

`fireWithRetry` wraps `fireStep` (`flow/failure.go`), the same seam
`runSingleton` already calls for a step with no `Retry`. Using
`fireStep` instead of `m.Fire` directly matters: `fireStep` tags a
`Fire` error `failureKindFire`, and `resolveCatchable`
(`flow/failure.go`) requires that tag to route an exhausted retry
into a declared `AdmissionOnFailed` fallback. A helper that called
`m.Fire` directly would strip that tag and silently break phase 23's
fallback catch path for every retried step. On a `fireStep` error,
when `step.Retry` is non-nil, `fireWithRetry`:

1. Compares the completed attempt count against `MaxAttempts`. A count
   already at `MaxAttempts` stops the loop and returns the last
   tagged error, unchanged from today.
2. Checks `Retryable(err)`, when non-nil. A false result stops the
   loop at once and returns the tagged error unchanged, so the
   existing failure path (including a declared fallback) sees the
   same error shape it sees today.
3. Otherwise calls `Retry.Sleep(ctx, Retry.NextDelay(attempt))`. A
   `ctx` canceled during that call stops the loop at once and returns
   the context error, without calling `fireStep` again. Otherwise
   `fireWithRetry` retries `fireStep` from the same pre-step `cur`,
   `rec`, and `row` the first attempt used, and repeats from step 1.

A retry that succeeds continues exactly like today's single-attempt
success: `Confirm` runs, the step emits its event, and its outcome is
`OutcomeSucceeded`. The caller sees no difference between a step that
succeeded on its first attempt and one that succeeded on a later
retry; `Report` records no attempt count. A step that exhausts its
retries reports `OutcomeFailed` and carries the last attempt's error,
exactly like today's single-attempt failure. Phase 23's fallback
admission applies unchanged: a declared `AdmissionOnFailed` dependent
still catches it.

The builder places the loop in a small helper in `flow/retry.go`, so
`runSingleton` stays inside the 80-line cap and `runner.go` stays
inside the 500-line cap:

- `fireWithRetry(ctx context.Context, m *machine.Definition, cur machine.Status, rec machine.InOut, step Step, row machine.Transition) (machine.Status, machine.InOut, error)`
  — the same six-argument shape `fireStep` takes, so `runSingleton`
  swaps one call for the other with no other signature change. It
  runs the loop described above and returns the same three values
  `fireStep` returns. A nil `step.Retry` calls `fireStep` once and
  returns its result unchanged; `runSingleton` therefore calls
  `fireWithRetry` unconditionally in place of its previous direct
  `fireStep` call, and needs no branch of its own for the nil case.
  `fireWithRetry` lives in `flow/retry.go`, next to `RetryPolicy`,
  `Validate`, and `NextDelay`; it imports `flow/failure.go`'s
  unexported `fireStep`, both already in the same package.

## Tests

Test files live in `flow/flow_test/`:

- `retry_test.go` — the red-green cases. Red step: the file does not
  compile on the empty phase, because `RetryPolicy` and `Step.Retry`
  do not exist. Record the compiler error as the red. Cases:
  - `RetryPolicy.Validate` rejects `MaxAttempts` of 0, pinned message.
  - `RetryPolicy.Validate` rejects a zero `MaxDelay`, pinned message.
  - `RetryPolicy.NextDelay` returns `BaseDelay` for attempt 1 and
    doubles for each later attempt, clamped at `MaxDelay`.
  - `RetryPolicy.NextDelay` applies a non-nil `Jitter` to the clamped
    result; a nil `Jitter` leaves the clamped result unchanged.
  - `RetryPolicy.NextDelay` with a `Jitter` closure that returns a
    value above `MaxDelay` returns that above-`MaxDelay` value
    unchanged. This pins that `NextDelay` never re-clamps `Jitter`'s
    output, matching the documented limit in the API section: bounding
    `Jitter`'s result is the caller's responsibility.
  - `RetryPolicy.NextDelay` called with `attempt` 50, `BaseDelay` of one
    millisecond (1,000,000 nanoseconds), and a `MaxDelay` well below
    `time.Duration`'s maximum (for example one hour) returns exactly
    `MaxDelay`, never a wrapped or negative duration. This is the
    red-green proof that the clamp-before-double order in the API
    section prevents overflow. The naive `BaseDelay * 2^(attempt-1)`
    computation for this input is 1,000,000 * 2^49 nanoseconds, and
    2^49 is 562,949,953,421,312, so the naive product is
    562,949,953,421,312,000,000 nanoseconds. `int64`'s maximum is
    9,223,372,036,854,775,807 nanoseconds (about 9.223e18); the naive
    product is about 5.629e20, roughly 61 times over that maximum, so
    the naive multiply overflows `int64` and wraps to an unpredictable,
    often negative, value long before attempt 50. A naive
    multiply-then-clamp implementation therefore fails this case; the
    clamp-before-double implementation in the API section never
    performs that multiply and returns `MaxDelay` exactly.
  - `RetryPolicy.NextDelay` called with `MaxAttempts` at the
    `Validate`-accepted maximum and `attempt` equal to `MaxAttempts`
    still returns a value at or below `MaxDelay` for every `BaseDelay`
    from one nanosecond up to `MaxDelay` itself, run as a table over
    several `BaseDelay`/`MaxDelay` pairs.
  - `New` rejects a retried step with a sub-workflow, pinned message.
  - `New` rejects a retried panel member, pinned message.
  - `New` accepts a retried step with no `Sub` and no panel.
  - A step whose guard fails twice then succeeds, under a
    `RetryPolicy` with `MaxAttempts` of 3, ends `OutcomeSucceeded`.
    Assert the guard ran exactly three times and `Retry.Sleep` ran
    exactly twice, using a recording `Sleep` field instead of real
    time. Assert `OnExit` and `OnEntry` also ran exactly three times
    each, once per attempt, proving the retry loop re-invokes every
    part of `Fire`, not only the guard.
  - A step whose guard always fails, under a `RetryPolicy` with
    `MaxAttempts` of 3, ends `OutcomeFailed` after exactly three guard
    calls. The run aborts with the last attempt's wrapped error, when
    no fallback is declared.
  - A `Retryable` predicate that returns false stops the loop after
    the first failure, even when `MaxAttempts` allows more. Assert the
    guard ran exactly once.
  - A step with `Retry` nil keeps today's single-attempt behavior:
    one guard call, one failure, one abort.
  - A step with a non-nil `Retry` whose `MaxAttempts` is 1, and a
    guard that always fails: one guard call, one failure, one abort,
    the same outcome as the `Retry`-nil case above, but assert
    `Retry.Sleep` and `Retry.Retryable`, both set to recording
    closures, are never called. This isolates the "loop runs zero
    extra iterations" path (`Retry` non-nil, `MaxAttempts` 1) from the
    "no retry policy at all" path (`Retry` nil) the previous case
    covers; the two paths produce the same outcome for a different
    reason.
  - A retried step (`RetryPolicy` `MaxAttempts` 3) whose `Confirm`
    always rejects: assert the guard (`Fire`) ran exactly once, not
    three times. `fireWithRetry` wraps only `fireStep`; `runSingleton`
    calls `Confirm` after `fireWithRetry` returns, outside the retry
    loop, so a `Confirm` rejection never triggers a retry.
  - A retried branch step (`RetryPolicy` `MaxAttempts` 3, guard
    succeeds on the first attempt) whose `Route` always returns an
    error: assert `Route` ran exactly once. `Route` runs in the
    runner, after the wave, never inside `fireWithRetry`; this test
    proves the retry loop never wraps `Route`.
  - A canceled `ctx`, passed to `Run` before the second attempt's
    `Sleep` call resolves, aborts the retry loop with the context
    error. Use a `RetryPolicy` with `MaxAttempts` of 5 and a guard that
    always fails, and a recording `Sleep` field that cancels the `ctx`
    on its first call before returning. Assert `fireWithRetry` returns
    before the guard's third call, so the loop stops short of
    `MaxAttempts`.
  - The default `Sleep` (nil field) returns before its full duration
    when its `ctx` is canceled mid-wait. Use a short real duration and
    a `ctx` canceled from a second goroutine partway through, and
    assert `Sleep` returns near the cancellation time, not the full
    duration.
  - A step that exhausts its retries and has a phase 23 fallback
    declared: the run continues down the fallback path, and
    `FailureFrom` inside the fallback returns the last attempt's
    error.
- `retry_integration_test.go` — run a three-step linear graph where
  the middle step's guard fails on its first two calls and succeeds on
  its third, under a `RetryPolicy` with `MaxAttempts` of 3 and a
  recording `Sleep`. Assert every step's outcome, the final status,
  and the final record. Assert the recorded sleep durations match
  `NextDelay(1)` and `NextDelay(2)` in order. Run a second case where
  the same guard never succeeds and a fallback step catches the
  failure; assert the fallback's `Failure.Err` wraps the last guard
  error.
- `retry_bench_test.go` — benchmark a single-step run whose guard
  always succeeds on the first attempt, with a `RetryPolicy` present
  but never triggered, against the same step with `Retry` nil. Measure
  the `Retry`-nil baseline on the phase 23 code before this phase
  lands. Record both in the file's leading comment. Report the
  ns/op and allocs/op ratio; the always-succeeds case measures the
  loop's guard overhead, not backoff timing, so a fixed allocation
  budget applies as in phase 21's benchmarks.

## Verification

`make verify` passes. The coverage floor for `flow` holds.
`api/flow.txt` gains `RetryPolicy`, its two methods, and `Step.Retry`
via `make api-update`. Commit the `api/` diff in the same change.
`policy/layers.json` is unchanged; `flow`'s allowed imports
(`events`, `machine`) already cover every edge this phase needs, since
`time` is a standard-library import inside `flow` itself, not a new
internal package edge. `api/machine.txt` is unchanged; the machine
package is untouched.

`docs/architecture.md` and `docs/packages/flow.md` update the flow
sections in the same change as the code. Describe `RetryPolicy`,
`NextDelay`, and the retry loop's place between a `Fire` failure and
phase 23's fallback admission. `AGENTS.md` updates its `flow/` layout
bullet in the same change; the bullet names the retry vocabulary next
to outcome, admission, route, and fallback.
