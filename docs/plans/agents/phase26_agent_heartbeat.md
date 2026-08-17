# Phase 26: agent step-liveness heartbeat

Status: ready to build. Builds on phase 13's `Run` (docs/plans/agents/
phase13_agent_run.md) and the shipped `heartbeat` package
(docs/plans/heartbeat.md). Independent of phases 14 through 25.

## Goal

Close the one real liveness gap in `Run`: the caller-supplied `wait`
call inside `confirmStep` can block forever, and today nothing signals
that a step's wait has stalled. `Run` gains an optional
`*heartbeat.Monitor` parameter. `Run` beats the monitor once per step,
right before it calls `wait`. An external caller, holding the same
`*heartbeat.Monitor`, polls `Dead` on its own schedule and reacts, for
example by canceling `ctx` to unblock a stalled `wait`.

`Run` does not call `Dead` itself and does not abort a stalled step on
its own. `heartbeat`'s own plan states `Dead` is normally consumed by
an external sweep, not the beating code. `Monitor` has no callback or
subscription hook; `Run` adds none. This phase wires the smallest
correct piece: the beat.

### Disclosed scope limit: panel steps get no beat

Heartbeat coverage reaches only a gated, singleton step: the kind
`confirmStep` gates behind a `wait` call. `flow.Run`'s panel wave runs
every panel member concurrently, in a goroutine per member, with no
`Confirm` or `wait` call at all; see `flow/runner.go`'s `runWave`.
`Run` beats the monitor only inside `confirmStep`, so a panel of two
or more members never reaches a beat call. `hb.Dead` can never report
a stalled panel member, because no id for that member's wave is ever
beaten in the first place.

This is a known, disclosed scope limit, not a bug this phase fixes.
It matches how this plan already treats "a beat right before `wait`
ages a slow-but-healthy wait out of `Alive`" as a disclosed tradeoff:
the beat only covers the sequential, gated path `Run` already
serializes through `wait`. A later phase that wants panel-member
liveness needs its own beat call inside `flow`'s panel wave, plus its
own id scheme for a concurrent, per-member beat; this phase adds
neither.

## Scope

Inside: one new trailing parameter on `Run`, `hb *heartbeat.Monitor`.
One beat call per gated step, right before `wait`. One `Forget` call,
deferred, that runs once per `Run` invocation regardless of outcome.

Outside: a `Dead` check inside `Run`. A retry, backoff, or
cancellation policy. A `MissedEvent` emission from inside `agent`; a
caller that wants that event still reads `Dead` and emits it through
its own `events.Bus`, exactly as `heartbeat`'s own plan describes.
`Run` adds no such wiring, because no phase before this one scopes an
automatic-reaction requirement, and adding one now would be
speculative generality under the Building blocks rule in AGENTS.md.
Also outside: any beat call for a panel member. Coverage stops at the
gated, singleton steps `confirmStep` already serializes through
`wait`; see "Disclosed scope limit: panel steps get no beat" above.

### Why `hb` is optional, not required

`wait` and `bus` are required parameters; a nil value there is a
caller bug and `Run` returns a sentinel. Heartbeat tracking is
different: it is a supplementary telemetry signal, not a correctness
requirement for step confirmation. A caller running a fast, supervised
loop with no external stall-watcher gains nothing from a `Monitor` and
should not need to build one to call `Run`. `hb == nil` skips every
heartbeat call; `Run`'s existing behavior is otherwise unchanged. This
matches flow's `onCheckpoint` parameter (docs/plans/agents/
phase25_flow_checkpoint.md), which is nil-safe for the same reason: an
optional hook a caller opts into, not a required input.

### The beat id: identity plus thread, not just one or the other

The beat id is `a.id.Signer() + ":" + threadID`, built once per `Run`
call and reused for every step's beat inside that call.

A bare `threadID` is not enough. Two different `Agent` values, sharing
one caller-supplied `Monitor`, could pick the same `threadID` by
coincidence; the envelope protocol names `thread_id` as a caller
convention, not a namespaced identifier. A bare agent identity is not
enough either. One `Agent` value can run more than one thread
concurrently, each call sharing the same `Monitor`; keying only on
identity would let two concurrent `Run` calls beat the same id from
two goroutines, and `Beat` requires each beat's `at` to be at or after
the previous one for that id. Two independent goroutines racing
`time.Now()` reads can produce an earlier read after a later one lands
first, and the id-sharing goroutine would then see `ErrStaleBeat` for
a wait that is not stalled at all. Combining identity and thread makes
each `Run` invocation's beat id unique across a shared `Monitor`,
regardless of how many agents or how many concurrent threads use it.

### The beat timing: right before `wait`, every step, same id

`Run`'s doc comment already states `confirmStep` runs sequentially per
gated step; `flow.Run` never calls `Confirm` twice at once. So one id
per `Run` call, beaten once per step, carries no race inside one
`Run` call.

A beat lands right before the `wait` call, not right after `Run`
builds the signed message and not on `wait`'s return. "Alive" means
"about to wait, or still waiting," not "started a step a while ago."
A beat placed earlier and never refreshed would let a step that stalls
inside `wait` keep reading as alive long after the real stall began.
Placing the beat immediately before `wait` means a `Monitor` sized
with a timeout close to the expected wait latency ages the id out of
`Alive` at close to the real stall point.

`Run` beats the same id before every step's `wait` call, not a
per-step id. A per-step id would leave one dangling, never-forgotten
entry in the shared `Monitor` for every completed step, growing the
tracked set for the life of the `Monitor` with no cleanup path. One
id per `Run` call, reused across steps and forgotten once, keeps the
`Monitor`'s tracked set bounded by the number of runs in flight, not
the number of steps ever executed.

### The clock: `time.Now()`, not an injected clock

`heartbeat.Monitor` never reads a clock; every method takes the
caller's `time.Time`. `agent` is the composition layer (AGENTS.md,
Building blocks); it is the layer allowed to touch real wall-clock
time, the same way a real sender in `heartbeat`'s own design "beats on
its own schedule." `Run` calls `time.Now()` directly for the beat's
`at` argument. No package in this repository calls `time.Now()` in
non-test code today; `Run` is the first, and it is the right layer for
it, not `heartbeat`, `flow`, or `machine`, which all stay clock-free
by design.

A test that needs a deterministic "this beat is now stale" outcome,
with no `time.Sleep`, builds the `Monitor` with a very small timeout,
for example one nanosecond. Any real, unavoidable wall-clock
progress between the beat and the next `Alive` or `Dead` check exceeds
that timeout, so the outcome is deterministic without a sleep and
without an injected clock. `heartbeat`'s own `TestAlive` already
proves the boundary math this trick relies on.

### `Forget`: one deferred call per `Run` invocation

`Run` defers `hb.Forget(hbID)` right after it builds `hbID`, so it
runs exactly once, on every return path: a successful run, a
`flow.Run` error, an escalated step, or a plain `wait` error. This
matches `heartbeat.Forget`'s own doc: "for a clean departure." A
`Run` invocation that has ended has nothing further to beat; leaving
its id in a shared `Monitor` after that point offers no caller any
signal and only grows the tracked set.

`Run` discards `Beat`'s error return. `ErrNoID` cannot happen: `hbID`
is built from `a.id.Signer()`, which is never blank once `New`
accepted the identity, and `threadID`, already checked non-empty by
the sentinel guard ahead of it. `ErrStaleBeat` cannot happen either,
given the beat-id design above: one id, one `Run` call, one goroutine,
beaten with `time.Now()`, whose monotonic reads inside one goroutine
never regress. A beat error here would be unreachable in practice; the
liveness signal is supplementary and must not fail the step it only
observes.

## API

The surface below lands in `api/agent.txt` via `make api-update`.

- `func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor) (machine.Status, machine.InOut, error)`
  — the existing phase 13 signature, with one trailing parameter. Every
  existing behavior described in docs/plans/agent.md's phase 13
  section is unchanged. `hb == nil` skips every heartbeat call and
  `Run` behaves exactly as it does today. `hb != nil` adds one
  `hb.Beat(a.id.Signer()+":"+threadID, time.Now())` call right before
  each step's `wait` call, and one deferred
  `hb.Forget(a.id.Signer()+":"+threadID)` call that runs once per
  `Run` invocation.

No new exported type, sentinel, or constant. `Run`'s existing sentinel
checks (`ErrNoWait`, `ErrNoBus`, `ErrNoThread`) are unchanged; `hb`
gets no check of its own, by design (see Scope).

The expected `api/agent.txt` diff, against the phase 13 block already
in docs/plans/agent.md:

```text
- func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus) (machine.Status, machine.InOut, error)
+ func (a *Agent) Run(ctx context.Context, threadID string, m *machine.Definition, in machine.InOut, wait AckWait, bus *events.Bus, hb *heartbeat.Monitor) (machine.Status, machine.InOut, error)
```

### Import change

`agent` gains `heartbeat`. The `policy/layers.json` row becomes
`"agent": ["identity", "discovery", "flow", "envelope", "events",
"machine", "heartbeat"]`. `heartbeat`'s own row stays `["events"]`; it
gains no new import, and it does not import `agent`.

### Every existing call site changes

This is a breaking signature change to an exported method, matching
how phase 25 changed `flow.Run`. Every existing call site gains a
trailing `nil` argument in this change, since no existing test wires a
`Monitor` yet:

- `agent/agent_test/lifecycle_integration_test.go:94`
- `agent/agent_test/lifecycle_integration_test.go:135`
- `agent/agent_test/run_bench_test.go:49`
- `agent/agent_test/run_bench_test.go:70`
- `agent/agent_test/run_integration_test.go:89`
- `agent/agent_test/run_integration_test.go:150`
- `agent/agent_test/run_test.go:116`
- `agent/agent_test/run_test.go:135`
- `agent/agent_test/run_test.go:149`
- `agent/agent_test/run_test.go:164`
- `agent/agent_test/run_test.go:183`
- `agent/agent_test/run_test.go:215`
- `agent/agent_test/run_test.go:250`
- `agent/agent_test/run_test.go:269`
- `agent/agent_test/run_test.go:288`
- `agent/agent_test/run_test.go:314`
- `agent/agent_test/run_test.go:337`
- `agent/agent_test/run_test.go:360`
- `agent/agent_test/run_panel_integration_test.go:104`
- `agent/agent_test/run_panel_integration_test.go:156`

The builder confirms `go build ./...` catches any missed site before
the rest of `make verify` runs.

## Tests

New test files live in `agent/agent_test/`, alongside the phase 13
files:

- `liveness_test.go` — the red-green cases. Assertions come first; the
  builder confirms they fail against the unchanged `Run` signature,
  then implements the new parameter to green. Cases:
  - `hb == nil`, a confirmed one-step run: same status, same record,
    same nil error as the equivalent phase 13 case, proving the
    heartbeat path is fully inert when `hb` is nil.
  - `hb != nil`, a confirmed one-step run: the `AckWait` itself calls
    `hb.Alive(id, time.Now())` before it returns and asserts true,
    proving the beat lands before `wait` runs. `id` is computed in the
    test as `identity.Signer() + ":" + threadID`, using the same
    `*identity.Identity` value the test passed to `agent.New`.
  - `hb != nil`, a confirmed one-step run: after `Run` returns, the
    test asserts `hb.Alive(id, time.Now())` is false, proving the
    deferred `Forget` ran.
  - `hb != nil`, an escalated step: after `Run` returns with
    `errors.Is(err, agent.ErrEscalated)`, the test asserts
    `hb.Alive(id, time.Now())` is false, proving `Forget` runs on the
    error path too.
  - `hb != nil`, a plain `wait` error wrapping nothing: same `Forget`
    assertion as the escalated case, proving `Forget` is unconditional
    on the error's shape.
  - `hb != nil`, a two-step sequential run: the test's `AckWait`
    counts calls and asserts `hb.Alive(id, time.Now())` is true on
    both calls, with the same `id` both times, proving one id serves
    the whole run, not one id per step.
  - `hb != nil`, a `Monitor` built with a one-nanosecond timeout: the
    test's `AckWait` calls `hb.Alive(id, time.Now())` a second time,
    a few instructions after its first check, and asserts it now
    reads false. No `time.Sleep` call anywhere in this case; real,
    unavoidable wall-clock progress between the two `time.Now()` reads
    exceeds the one-nanosecond timeout deterministically.
- `liveness_integration_test.go` — build a real `identity.Identity`, a
  real `discovery.Card`, a real two-step `flow.Definition`, a real
  `machine.Definition`, a real `events.Bus`, and a real
  `heartbeat.Monitor`. Cases:
  - A full successful run: after `Run` returns, `hb.Dead(time.Now())`
    is empty, proving `Forget` removed the run's id regardless of the
    `Monitor`'s timeout.
  - Two goroutines call `Run` on the same `*Agent`, with two different
    `threadID` values, sharing one `*heartbeat.Monitor`. Both calls
    succeed with no `ErrStaleBeat`-derived failure, proving the
    identity-plus-thread beat id avoids the same-id race the Scope
    section describes. Runs under `go test -race`.
  - An external-sweep scenario: one goroutine calls `Run` with an
    `AckWait` that blocks on `ctx.Done()` and returns `ctx.Err()`. A
    second goroutine polls `hb.Dead(time.Now())` in a bounded loop,
    yielding with `runtime.Gosched()` between polls, no `time.Sleep`,
    until it observes the run's id, then calls the test's `cancel`
    function. The test asserts `Run` returns a non-nil error, and that
    `hb.Alive(id, time.Now())` is false after `Run` returns. This
    proves the intended pattern: an external caller reacts to `Dead`
    by canceling `ctx`, and `Run` unwinds cleanly through the deferred
    `Forget`.
  - A panel-coverage-gap case, pinning the disclosed scope limit: a
    two-member panel plan with no gated step, run with `hb != nil`. The
    test asserts `hb.Dead(time.Now())` is empty after `Run` returns,
    and that `hb.Alive(id, time.Now())` is false for the same
    identity-plus-thread id a gated run would have used, proving no
    beat ever landed for the panel wave. This pins the gap described in
    "Disclosed scope limit: panel steps get no beat" above against
    silent regression if a later phase adds panel-aware beating without
    updating this contract.
- `run_bench_test.go` gains `BenchmarkRunWithHeartbeat`, alongside the
  existing nil-`hb` benchmark, on the same two-step plan, with a
  `*heartbeat.Monitor` built with a one-second timeout. The builder
  records the measured baseline and reports the allocs/op delta
  against the existing nil-`hb` benchmark in the file's leading
  comment, matching phase 25's before/after benchmark pattern.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `agent` and for the total, with
  the new heartbeat lines counted in.
- The `agent` row in `policy/layers.json` gains `heartbeat`. The row
  change lands with this plan update, before the code.
- `heartbeat`'s row in `policy/layers.json` stays `["events"]`;
  `heartbeat` gains no new import and does not import `agent`.
- `api/agent.txt` gains the changed `Run` line, through
  `make api-update` in the same change as the code. No other line in
  `api/agent.txt` changes. `api/heartbeat.txt` stays unchanged.
- `go test -race ./agent/...` passes, covering the two-goroutine and
  the external-sweep integration cases.
- `agent/doc.go`'s file map gains `liveness.go` if the builder adds
  one, or notes the beat and forget logic living inside `run.go` if
  the builder keeps it there. Either placement stays under the
  500-line file cap and the 80-line function cap in
  `scripts/check_structure.py`.
- `docs/architecture.md`'s `agent/` bullet gains one sentence
  describing the optional `*heartbeat.Monitor` parameter and the
  `heartbeat` import edge, in the same change as the code.
- This phase adds no conformance vector. `Run` still composes
  `envelope.Message` and `envelope.Ack`; the heartbeat addition
  carries no wire form of its own.
