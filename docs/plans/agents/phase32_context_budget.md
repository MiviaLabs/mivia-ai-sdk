# Phase 32: context budget limits

Status: future. Depends on phase 12 (agent definition, shipped). This
phase adds a new leaf package, `contextbudget`, and wires it into
`agent.Run` as a real, optional caller in the same change. The future
`provider` package (a sibling planning track; see the empty `provider`
row already reserved in `policy/layers.json`) is a second, later
caller: a provider call site checks a budget before it builds a model
request. This phase adds no import of `provider` and does not block on
it.

## Goal

Give a caller a pure, storage-agnostic way to state and check a budget
for one model call's context: a byte cap, an event or message count
cap, and a check that says whether more content still fits. The type
holds no data of its own beyond the limits; it does no I/O.

## Scope

Inside: the `Limits` type, its zero-means-uncapped semantics, a
`Validate` method that enforces the invariant, and a method that
checks whether a given size and count still fit inside the limits.
Inside: one optional parameter on `agent.Run` that, when set, calls
`Validate` once and `Fits` before every gated step's `wait` call, so
`Limits` ships with a real caller in this change, not a future one.

Outside: summarization, trimming policy, and any decision about which
event or blob to drop. Outside: I/O, storage, and any coupling to a
specific memory store or model provider. The caller (a context manager
in the composing application, `agent.Run`, or a later `provider` call
site) owns the trim or summarize decision; `Limits` only tells the
caller whether the content still fits, and `agent.Run` only tells the
caller the run stopped because it did not. Also outside: any budget
check for a panel member's message; see "Disclosed scope limit: panel
steps get no budget check" below.

### Disclosed scope limit: panel steps get no budget check

The budget check reaches only a gated, singleton step: the kind
`confirmStep` gates behind a `wait` call. `flow.Run`'s panel wave runs
every panel member concurrently, in a goroutine per member, with no
`Confirm` or `wait` call at all; see `flow/runner.go`'s `runWave`.
`Run` checks `Fits` only inside `confirmStep`, so a panel of two or
more members' messages never add to `runningBytes` and never trip
`MaxEvents` or `MaxBytes`. This is the same gap phase 26 already
discloses for the heartbeat beat, which also reaches only
`confirmStep`'s `wait` call and never a panel member's goroutine. This
is a known, disclosed scope limit, not a bug this phase fixes. A later
phase that wants panel-member budget coverage needs its own `Fits`
call inside `flow`'s panel wave, plus its own accounting for a
concurrent, per-member byte and event count; this phase adds neither.

This is distinct from phase 15's `memory.Store`. `memory.Store` holds
content-addressed blobs and evicts by insertion order once a byte
budget is spent; it does I/O and owns state. `Limits` holds no blobs
and does no eviction; it is an accounting check a caller runs before
it decides to call `memory.Store`, trim a message list, or summarize.
The two types may compose in a caller (a `provider` call site checks
`Limits` against the messages it is about to send, then reaches for
`memory.Store` if it needs to fetch a summarized blob), but neither
imports the other.

### Placement: a new leaf package, not a field inside `agent`

`Limits` lands in its own package, `contextbudget`, not inside
`agent`. `AGENTS.md`'s Building blocks rule requires leaf blocks
first, composition last, and states an agent imports blocks, a block
never imports agent. `provider` is planned as a leaf (its
`policy/layers.json` row is reserved with no allowed imports). If
`Limits` lived inside `agent`, a future `provider` call site that
needs `Limits.Fits` would have to import `agent`, the composition
layer, to reach it. That inverts the direction phase 13 already
rejected for `agent`'s own dependencies. A dependency-free type with
one exported struct and two methods costs one small package: a
`policy/layers.json` row with an empty `allowed_imports` list, a plan
file, and an `api/contextbudget.txt` lock. Both `agent` and the future
`provider` import `contextbudget` directly; neither imports the other
to reach it.

`policy/layers.json` already carries both rows this phase needs:
`"contextbudget": []` and `"contextbudget"` inside `agent`'s row,
alongside its existing entries (`envelope`, `events`, `machine`,
`identity`, `discovery`, `flow`, `heartbeat`). Commit 07cddc7 ("add
phases 29-34 for mivia-agent capability parity") landed both rows
ahead of this phase's code, per the layer policy's own rule: a new
package needs a row before it has code. This phase makes no further
edit to `policy/layers.json`; it only adds the `contextbudget` package
and the `agent.Run` change the existing rows already allow.

### Why `agent.Run` is the caller now, not a deferred `provider` hook

`AGENTS.md` rejects planner output that adds abstraction without a
caller. `provider` does not exist yet and has no committed phase
number, so deferring `Limits`'s only caller to it would ship an
unused type today. `agent.Run` already threads several optional,
nil-skips-the-check parameters (`hb *heartbeat.Monitor`, `room
string`'s empty-string case); a `budget *contextbudget.Limits`
parameter follows that established shape and is the smaller change:
one new parameter, one new nil check, one new call to `Fits` inside
`confirmStep`, versus standing up `provider` early to give `Limits` a
caller. `Run` gains a real behavior, gated behind a nil check that
reproduces today's unbounded behavior exactly when the caller passes
`nil`.

### Why `Run` tracks a running byte total, not a per-step size

`contextbudget.Fits` "takes the candidate totals the caller already
has; it keeps no running total of its own" (see the `contextbudget`
API section). `Run` already accumulates a running step count across
iterations to gate `MaxEvents`; `MaxBytes` needs the same treatment,
not a per-message value. `Run` (inside `confirmStep`) keeps a single
`runningBytes int` that starts at zero and, after each step's message
is signed into `built`, adds that message's `len(payload)` to the
running sum. Before the `wait` call for the next step, `Run` calls
`budget.Fits(runningBytes+len(payload for the step about to run),
stepCount)`, so `MaxBytes` is checked against the cumulative size of
every message the run has built or is about to build, exactly the way
`stepCount` already reflects every step built or about to run. A run
of many small messages that together cross `MaxBytes` trips the cap on
the step that pushes the running sum over it, even though no single
message is large.

## API

Two surfaces land in this phase: `api/contextbudget.txt` (new) and an
addition to `api/agent.txt` (existing), both via `make api-update` in
the same change as the code.

### `contextbudget` (new package, leaf, no internal imports)

- `type Limits struct` holds `MaxBytes int` and `MaxEvents int`. Both
  fields are zero-value by default. A zero `MaxBytes` means no byte
  cap. A zero `MaxEvents` means no event-count cap. Both zero means
  the budget is uncapped.
- `(Limits) Validate() error` reports an error when `MaxBytes` is
  negative or when `MaxEvents` is negative. A zero or positive value
  in either field passes. `Validate` enforces the zero-means-uncapped
  invariant this plan states in prose: a negative cap has no defined
  meaning, so it is rejected, not silently treated as uncapped or as
  a cap of zero items. `Validate` names the offending field in the
  error text: `"contextbudget: MaxBytes must not be negative"` when
  `MaxBytes` is negative, `"contextbudget: MaxEvents must not be
  negative"` when `MaxEvents` is negative and `MaxBytes` is not. When
  both are negative, `Validate` checks `MaxBytes` first and returns
  only the `MaxBytes` message; it does not join both.
- `(Limits) Fits(bytes, events int) bool` reports whether `bytes` and
  `events` both stay at or under their respective caps. A zero cap
  always reports fit for its own dimension. `Fits` takes the candidate
  totals the caller already has; it keeps no running total of its
  own, since `Limits` holds no state beyond the two caps. `Fits` does
  not call `Validate`; a caller that skips `Validate` and passes a
  negative cap gets whatever comparison `bytes <= cap` produces for
  that cap, which is caller error, not a `Fits` contract.

`Limits` needs no constructor. A caller builds it with a struct
literal; both fields default to the uncapped zero value, matching
`envelope.Provenance`, an existing zero-value struct type in this SDK
built with a literal and no `New`. `Fits` takes both totals as
parameters rather than mutating internal counters, because `Limits`
is a pure check, not an accumulator: the caller (`agent.Run`, a
context manager, or a future `provider` call site) already tracks the
running byte and event count as it builds a request, and only needs a
yes/no answer against the cap.

### `agent` (existing package, gains one parameter and one sentinel)

- `func (a *Agent) Run(ctx context.Context, threadID string, m
  *machine.Definition, in machine.InOut, wait AckWait, bus
  *events.Bus, hb *heartbeat.Monitor, room string, budget
  *contextbudget.Limits) (machine.Status, machine.InOut, error)` gains
  a trailing `budget` parameter. A `nil` budget skips every budget
  check; `Run`'s behavior is unchanged from today. A non-nil `budget`
  runs `budget.Validate()` once, at the same point `Run` checks `wait`,
  `bus`, and `threadID`, before it touches `m` or `a`'s plan; an
  invalid budget returns `machine.Status("")`, `in` unchanged, and the
  wrapped `Validate` error. A non-nil, valid `budget` makes `Run` keep
  a running byte total, `runningBytes`, across the whole call: after
  each step's message is signed into `built`, `Run` adds that
  message's `len(payload)` to `runningBytes`. Right before the `wait`
  call for the step about to run, `confirmStep` calls `budget.Fits`
  with `runningBytes` plus the about-to-run step's own payload byte
  length, and the 1-indexed count of steps built so far (including the
  step about to run); both totals are cumulative over the whole run,
  matching how `Fits` documents itself as taking "the candidate totals
  the caller already has" rather than keeping its own running total.
  `confirmStep` checks `Fits` before it calls `hb.Beat`: today's code
  positions `hb.Beat` immediately before the `wait` call (see
  `agent/run.go`'s `confirmStep`, around the lines right after
  `EmitMessageDelivered`), and this phase inserts the `Fits` check
  ahead of that `hb.Beat` call, not after it. A step that fails `Fits`
  returns `ErrOverBudget` before `confirmStep` ever calls `hb.Beat`,
  so an over-budget step records no heartbeat beat for a step that
  will never confirm. A `Fits` failure returns `ErrOverBudget`,
  wrapping the step ID, without calling `hb.Beat`, `wait`, or
  `EmitMessageAcked` for that step; `built` keeps every message signed
  for steps that already fit, and `runningBytes` is not incremented
  for the failing step's message. A panel step
  never reaches `confirmStep`'s `wait` call at all (see `flow/runner.go`'s
  `runWave`), so a panel member's payload is never added to
  `runningBytes` and never checked against `Fits`; this mirrors the
  disclosed panel-step gap phase 26 already documents for the
  heartbeat beat, and this plan discloses it the same way below.
- `ErrOverBudget = errors.New("agent: context budget exceeded")` is a
  new sentinel next to `ErrEscalated`, `ErrNoWait`, and `ErrNoThread`;
  test it with `errors.Is`.

This is an additive, backward-incompatible signature change to an
already-shipped exported function (Go has no optional parameters), so
it is a deliberate break: every existing caller of `agent.Run` in this
module's tests gains one trailing argument, `nil`, to keep today's
unbounded behavior. `scripts/check_api.py` records the new signature
as the locked surface; there is no wire-format change, so
`docs/protocol-design.md` is untouched.

## Tests

### `contextbudget/contextbudget_test.go`

Red-green cases for `Fits` and `Validate`. Start with the assertions;
confirm they fail on the empty package; implement and watch them pass.
Table cases for `Fits`:

- both caps zero: `Fits` returns true for any byte and event count,
  including a large value of each (for example `math.MaxInt`), noted
  in the test as documenting that `Fits` does no overflow protection:
  a caller that sums byte counts into an `int` that wraps gets an
  undefined answer, the same as any other unchecked `int` arithmetic
  in this SDK.
- `MaxBytes` set, `MaxEvents` zero: `Fits` returns true at and under
  the byte cap, false one byte over it, regardless of event count.
- `MaxEvents` set, `MaxBytes` zero: `Fits` returns true at and under
  the event cap, false one event over it, regardless of byte count.
- both caps set: `Fits` returns true only when both totals stay at
  or under their caps; false when either alone goes over.
- a negative `bytes` or `events` argument: `Fits` treats it as
  already under any positive cap (the caller never builds a negative
  total in practice, but `Fits` does not panic or misreport on one).

Table cases for `Validate`:

- zero-value `Limits`: `Validate` returns nil.
- positive `MaxBytes` and `MaxEvents`: `Validate` returns nil.
- negative `MaxBytes`, zero `MaxEvents`: `Validate` returns an error
  and `strings.Contains(err.Error(), "MaxBytes")` is true.
- zero `MaxBytes`, negative `MaxEvents`: `Validate` returns an error
  and `strings.Contains(err.Error(), "MaxEvents")` is true.
- both fields negative: `Validate` returns an error and
  `strings.Contains(err.Error(), "MaxBytes")` is true, proving
  `MaxBytes` is checked first.

No benchmark file. `Fits` and `Validate` are integer comparisons with
no allocation; a benchmark would measure noise, not signal. This phase
skips `contextbudget_bench_test.go` for that reason.

### `agent/agent_test/run_budget_test.go`

- `Run` with a `nil` budget behaves exactly as today's shipped
  behavior: reuse an existing `Run` success-path test unchanged except
  for the added trailing `nil` argument, asserting an identical
  result.
- `Run` with a valid, generous budget (caps well above the test plan's
  payload sizes and step count) succeeds identically to the `nil` case.
- `Run` with an invalid budget (`Limits{MaxBytes: -1}`) returns
  `machine.Status("")`, `in` unchanged, and an error that is non-nil
  and satisfies `strings.Contains(err.Error(), "MaxBytes")`, proving
  the wrapped `Validate` error surfaces through `Run`'s own error
  rather than being replaced or swallowed; asserted before `wait` is
  ever called (a spy `AckWait` records zero calls).
- `Run` with a budget whose `MaxEvents` is smaller than the plan's
  gated step count returns `ErrOverBudget` on the step that exceeds
  it, `bus`'s recorded `MessageAckedEvent` count equal to the number
  of steps built before the failing one, and no `ThreadVerifiedEvent`,
  proving `Run` stops mid-plan rather than completing over budget. The
  failing step still fires `MessageDeliveredEvent` (the `Fits` check
  runs after `EmitMessageDelivered`, per the ordering in API above)
  but never reaches `EmitMessageAcked`, so `MessageAckedEvent` count,
  not `MessageDeliveredEvent` count, is the proof that the step never
  committed, matching the wait-error precedent in
  `TestRunOneStepWaitErrorWithValidAck`
  (`agent/agent_test/run_test.go`).
- `Run` with a generous `MaxEvents` (above the plan's step count) and
  a `MaxBytes` cap that no single step's payload exceeds alone, but
  that the sum of the first two steps' payloads does exceed: `Run`
  succeeds through the first step, then returns `ErrOverBudget` on the
  second step, proving `Fits` is checked against `runningBytes`, the
  cumulative sum of every payload built so far plus the step about to
  run, not against the current step's payload alone. The test plan
  fixes each step's payload size (for example by controlling `in`'s
  content) so the first step alone stays under `MaxBytes`, the second
  step alone also stays under `MaxBytes`, and only their sum exceeds
  it.
- `Run` with a `MaxBytes` cap set below the very first step's own
  payload size (not a cumulative-over-two-steps case, a single
  oversize step): `Run` returns `ErrOverBudget` on step one, `bus`'s
  recorded `MessageAckedEvent` count is zero, and `built` is empty,
  proving `Fits` catches a single step that alone exceeds the cap, not
  only a cumulative sum across steps.
- `Run` with a budget whose `Fits` check fails on a gated step, run
  with a non-nil `hb`: `hb.Alive` for the run's identity-plus-thread
  beat id reads false after `Run` returns `ErrOverBudget`, proving
  `confirmStep` checks `Fits` before `hb.Beat` and never beats an
  id for a step that will never confirm. Add this case to
  `run_budget_test.go`, mirroring the two-argument fixture pattern in
  `TestLivenessFullRunLeavesDeadEmpty`
  (`agent/agent_test/liveness_integration_test.go`), but passing a
  budget whose `Fits` fails on the run's single gated step instead of
  a confirming wait.

### `agent/agent_test/run_panel_integration_test.go`

- `TestBudgetPanelWaveReachesNoCheck` mirrors
  `TestLivenessPanelWaveReachesNoBeat`
  (`agent/agent_test/liveness_integration_test.go`), the test that
  pins phase 26's disclosed heartbeat panel gap. Build a two-member
  panel plan (`flow.New` with two steps in one `flow.Panel`, no gated
  step), the same shape `TestLivenessPanelWaveReachesNoBeat` uses. Set
  a budget whose `MaxBytes` or `MaxEvents` cap the panel members'
  combined payload would exceed if `Run` checked it (for example a cap
  smaller than the sum of both panel members' payload sizes, or a
  `MaxEvents` of one). Run with that budget, a non-nil `hb`, and a
  confirming `wait`. Assert `Run` returns `nil`, not `ErrOverBudget`:
  a panel step reaches no `confirmStep` `wait` call, so `Fits` never
  sees the panel members' payloads and the run completes as if the
  budget were absent. This test pins the same disclosed scope limit
  the plan states in "Disclosed scope limit: panel steps get no budget
  check" above, the way `TestLivenessPanelWaveReachesNoBeat` pins
  phase 26's precedent for the heartbeat beat.

## Verification

- `make verify` passes: gofmt, vet, tests, the python gates, the
  Semgrep scan and probes, and the coverage block.
- The coverage floor of 85 holds for `contextbudget` and for `agent`,
  and for the total, with every new line counted in.
- `python3 scripts/check_deps.py` passes against the `"contextbudget":
  []` row and the `"contextbudget"` entry already present in `agent`'s
  row in `policy/layers.json` (landed by commit 07cddc7, ahead of this
  phase). This phase makes no edit to `policy/layers.json`; it only
  needs `check_deps.py` to keep passing against the existing rows once
  `contextbudget`'s code and `agent`'s import of it land.
- `api/contextbudget.txt` is created and `api/agent.txt` is updated
  via `make api-update`, in the same change as the code.
- `contextbudget/doc.go`'s file map and `agent/doc.go`'s file map each
  gain the new file names this phase adds, for example
  `contextbudget.go` and `run.go`'s updated signature.
- `docs/architecture.md` gains a `contextbudget/` bullet next to the
  other leaf packages, and the `agent/` bullet gains the `budget`
  parameter and `ErrOverBudget`, in the same change as the code.
- `docs/packages/agent.md` gains the new 9-argument `Run` signature in
  the same change as the code. The `Run` bullet (currently `Agent.Run(ctx,
  threadID, m, in, wait, bus, hb, room)`) and the runnable example
  (currently `a.Run(context.Background(), "task-42", m,
  machine.InOut{}, wait, bus, nil, "")`) both gain a trailing `budget`
  argument, `nil` in the example, to keep the doc's call sites
  compiling against the locked `api/agent.txt` signature.
- `docs/examples/agent-dispatch.md` gains the new 9-argument `Run`
  signature in the same change as the code, in two places: the
  runnable Go code block's call (currently `a.Run(context.Background(),
  "thread-dispatch-1", m, machine.InOut{Input: "incoming task"}, wait,
  bus, mon, rm.ID())`) and the mermaid sequence diagram's call
  (currently `Run(ctx, threadID, m, in, wait, bus, hb, room)`). Both
  gain a trailing `budget` argument (`nil` in the code block, `budget`
  spelled out in the diagram to match), so the example still compiles
  and the diagram still matches the locked signature.
- This phase adds no conformance vector. `Limits` defines no wire
  schema; it is an in-process accounting check with no `Encode` or
  `Decode` counterpart, and `agent.Run`'s budget check changes no byte
  on the wire.
- `docs/plans/agents/phase15_memory.md` stays unchanged: this phase
  does not alter `memory.Store` and does not depend on it. The two
  types may compose in a future caller, but neither imports the other.
- The builder creates `docs/plans/contextbudget.md` from
  `docs/plans/TEMPLATE.md`, restating this plan's Goal/Scope/API/
  Tests/Verification for `scripts/check_plan.py`, in the same change
  as the code.
- `docs/plans/agent.md` gains the `budget` parameter and
  `ErrOverBudget` alongside the API section it already documents, in
  the same change as the code.
