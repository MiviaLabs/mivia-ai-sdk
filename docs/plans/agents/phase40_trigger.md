# Phase 40: trigger

Status: future. Plan-only; it has not yet gone through plan review. It
adds one new package, `trigger`, with no dependency on a landed
phase. It ships independently of phase 38 (flow loop). It composes
with phase 39 (scheduler) and phase 37 (channel) only through
caller-owned closures; see Import edges below.

## Why this phase ships, and why it still stays small

An architecture review flagged a real risk before this plan: one
interface spanning several different invocable kinds, flow, agent,
tools, and an undefined "skills" concept, with no single caller shape,
matches this repo's own overengineering signal in `AGENTS.md`'s
Building blocks section, "no caller means speculative generality."
That review recommended holding this package until a caller scoped
it. The user's direction overrides that recommendation for this
plan: `trigger` ships now, as a real package. This plan still applies
the review's warning to the design, not to the decision of whether to
build: it keeps the package to the smallest shape that answers "what
fired" and "what runs," and it names, rather than special-cases, the
"skills" ambiguity below.

### Resolving "skills"

This Go SDK has no "skill" concept today. No package in this module
exports a `Skill` type or a skill registry. `trigger.Action` is
`func(ctx context.Context) error`, a plain closure shape generic
enough to wrap anything invocable: a `tools.Tool.Run` call, an
`agent.Run` call, a `flow.Run` call, or a future concept once one
exists as a concrete Go value. This plan adds no `Skill` type and no
skill-specific field. A future "skill," whatever shape it takes,
becomes an `Action` the same way `agent.Run` does today: a caller
wraps it in a closure matching the one func type this package ships.

## Goal

Give every part of this SDK, and every caller of this SDK, one shared
vocabulary for "a condition fired, so run this": `Condition`,
`Action`, and a `Registry` that maps a name to one of each. `trigger`
answers "what fired" and "what runs" once, so `scheduler.Job`,
`channel.Notifier`, and `events.Bus.Handler` can each drive a
`Registry` entry without `trigger` becoming a fourth, competing event
system.

## Scope

Inside: the `Condition` function type, the `Action` function type, the
`Registry` type, and its `Add`, `Remove`, and `Fire` methods, plus
their `Validate`-equivalent field checks inside `Add`. `trigger` is a
leaf package: no I/O, no goroutine, no persistence, no polling loop of
its own.

Outside:

- A `Skill` type or any skill-specific field. See "Resolving skills"
  above.
- A poller. `Registry.Fire` evaluates one named entry's `Condition`
  once, on the caller's own call. A caller who wants to poll a
  `Condition` on an interval wraps `registry.Fire` in a
  `scheduler.Job` and adds it to a `scheduler.Scheduler`, composed
  entirely in caller code. `trigger` never imports `scheduler` and
  never starts a timer itself.
- A second, competing dispatch path to `events.Bus`. A caller who
  wants an `events.Bus` subscription to drive a `Registry` entry
  writes an `events.Handler` closure whose body calls
  `registry.Fire(ctx, name)`. `trigger` never imports `events` and
  never holds a `Bus`.
- A second, competing question-and-answer path to `channel.Notifier`.
  A caller who wants a human or agent answer to gate a `Registry`
  entry writes the `Notifier` call and, on an approved `Answer`, calls
  `registry.Fire`. `trigger` never imports `channel`.
- Any change to `scheduler`, `channel`, `events`, `agent`, `flow`, or
  `tools`. This phase edits none of their files and none of their
  plans.
- Concurrent registration during a `Fire` call blocking on a long
  `Action`. `Registry` guards its map with a mutex the same way
  `tools.Registry` does; `Fire` releases the mutex before it calls the
  looked-up `Action`, so a slow `Action` never blocks a concurrent
  `Add` or `Remove`. This matches `tools.Registry.Run`'s own shape,
  which resolves through `Get` before it calls the tool.

### Import edges: why zero, not two

The task that scoped this phase allowed `trigger` to import
`scheduler` and `channel` if the design genuinely composed them. This
plan declines both edges. `Condition` and `Action` are the same
decoupled-invocable shape `scheduler.Job` and `channel.Notifier`'s
`Notify` method already use: a plain function value a caller builds
and hands over, never a value `trigger` constructs or holds itself.
Composition, in every case above, is one line in caller code: a
`scheduler.Job` or an `events.Handler` or a post-`Notify` closure that
calls `registry.Fire`. Importing `scheduler` or `channel` would buy
`trigger` nothing it cannot already do through that one caller-side
line, and it would cost `trigger` its place as a leaf package that
`agent`, `flow`, `tools`, `scheduler`, and `channel` can all depend on
later with no cycle risk. `docs/plans/channel.md` reasons the same
way about `envelope`: `channel` declines an import that would buy
convenience at the cost of forcing every implementation to carry a
dependency it may not need. `trigger` follows that precedent and
stays at zero internal imports, matching `tools`, `discovery`, and
`envelope`.

### `Condition` and `Action`: the smallest workable shapes

- `Condition func(ctx context.Context) (bool, error)` matches
  `machine.Guard`'s exact signature. Reusing it, rather than inventing
  a new predicate type, follows the same reuse this repo's phase 38
  plan applies to its own loop guard: one predicate shape across the
  module, not one per package.
- `Action func(ctx context.Context) error` matches `scheduler.Job`'s
  exact signature. A `trigger.Action` and a `scheduler.Job` are
  interchangeable by shape; a caller can pass the same closure to
  either, with no adapter, which is the shared vocabulary this phase's
  Goal names.

## API

The surface below lands in `api/trigger.txt` once this phase builds.

- `type Condition func(ctx context.Context) (bool, error)` — reports
  whether a named trigger's `Action` should run. A nil `Condition`
  passed to `Add` means "always ready," matching `machine.Guard`'s own
  nil convention.
- `type Action func(ctx context.Context) error` — the invocable a
  named trigger runs once its `Condition` is satisfied. `Add` rejects
  a nil `Action`; unlike `Condition`, a trigger with nothing to run
  has no purpose.
- `type Registry struct` — holds named triggers. Built only through
  `New`. Safe for concurrent `Add`, `Remove`, and `Fire`, matching
  `tools.Registry`'s concurrency shape.
- `func New() *Registry` — creates an empty `Registry`.
- `func (r *Registry) Add(name string, c Condition, a Action) error`
  — registers `c` and `a` under `name`. Rejects a blank `name` (empty
  after `strings.TrimSpace`) with `ErrBlankName`, a nil `a` with
  `ErrNilAction`, and a duplicate `name` with `ErrDuplicateName`,
  mirroring `tools.Registry.Add`'s per-field rejection order and
  sentinel-error shape. A nil `c` is accepted; see `Condition` above.
- `func (r *Registry) Remove(name string) bool` — removes `name`.
  Returns whether `name` was present, matching
  `tools.Registry.Remove`'s exact contract.
- `func (r *Registry) Fire(ctx context.Context, name string) error` —
  resolves `name`, evaluates its `Condition` (a nil `Condition` reads
  as true), and, when true, calls its `Action` and returns that
  call's error. Returns `ErrUnknownName` when `name` is not
  registered. Returns `ErrConditionNotMet` when the `Condition`
  evaluates false, without calling `Action`. Returns a `Condition`
  evaluation error wrapped `trigger: %q: %w`, without calling
  `Action`, the same way `machine.Fire` stops before `OnExit` on a
  `Guard` error.
- Sentinel errors: `ErrBlankName`, `ErrNilAction`, `ErrDuplicateName`,
  `ErrUnknownName`, `ErrConditionNotMet` — returned by `Add` and
  `Fire`, tested with `errors.Is`.

No interface beyond the two func types. No `Kind` or `Urgency`
classification: no caller in this module branches on a trigger's kind
today, matching phase 37's own reasoning for declining a `Question`
`Kind` field. A future phase adds a classification only once a real
caller needs to route on it.

## Composition patterns, in prose

These are caller-code patterns this plan documents but does not build.
None of them adds an import edge to `trigger`.

- Scheduled polling: a caller writes
  `scheduler.Job(func(ctx context.Context) error { return
  registry.Fire(ctx, name) })` and adds it to a `scheduler.Scheduler`
  with an `Every` or `At` schedule. `ErrConditionNotMet` is a normal,
  expected `Fire` result on a tick where the condition has not
  cleared yet; a caller who wants that case to not count as a
  `scheduler.JobFailedEvent` checks for `ErrConditionNotMet` with
  `errors.Is` inside the wrapping closure and returns nil for that
  case.
- Event-driven firing: a caller subscribes an `events.Handler` to a
  `events.Bus` whose body calls `registry.Fire(ctx, name)` once the
  subscribed event arrives, so an `events.Bus` emit becomes the
  "what fired" signal instead of a poll.
- Answer-gated firing: a caller's `channel.Notifier` implementation
  calls `registry.Fire(ctx, name)` after it receives an `Answer` with
  `Approved` true, so a human or agent approval becomes the "what
  fired" signal.

## Tests

Test files live in `trigger/trigger_test/`, following `PHASES.md`'s
flat test layout.

- `registry_add_test.go` — red-green cases for `Add`: a blank `name`
  returns `ErrBlankName`; a nil `Action` returns `ErrNilAction`; a
  duplicate `name` returns `ErrDuplicateName`; a nil `Condition` with
  a non-nil `Action` succeeds; a fully populated call succeeds.
  `Remove` returns true for a present `name` and false for an absent
  one.
- `registry_fire_test.go` — red-green cases for `Fire`: an unknown
  `name` returns `ErrUnknownName` and never calls any `Action`; a nil
  `Condition` entry always calls its `Action` and returns that call's
  error; a `Condition` returning false returns `ErrConditionNotMet`
  and never calls `Action`; a `Condition` returning a non-nil error
  returns that error wrapped `trigger: %q: %w` and never calls
  `Action`; a `Condition` returning true calls `Action` exactly once
  and returns its error, nil or not.
- `composition_test.go` — proves the three composition patterns
  compile and behave as documented, using only `context`, a local
  `Registry`, and locally declared stand-ins for
  `scheduler.Job`, `events.Handler`, and `channel.Notifier`'s
  signatures, so the test proves shape compatibility without giving
  `trigger` an import edge to any of the three packages. Cases:
  - A closure matching `scheduler.Job`'s signature that calls
    `registry.Fire` and swallows `ErrConditionNotMet` returns nil on a
    not-yet-ready condition and the `Action`'s own error once ready.
  - A closure matching `events.Handler`'s signature that calls
    `registry.Fire` on receipt of a stand-in event runs the correct
    `Action` for the event's payload-derived name.
  - A closure standing in for a `channel.Notifier`-driven approval
    that calls `registry.Fire` only when a stand-in `Answer.Approved`
    is true never calls `Fire` when it is false.
- `registry_concurrency_test.go` — runs `Add`, `Remove`, and `Fire`
  from several goroutines against one `Registry` under `go test
  -race`, proving the mutex-guarded map has no data race and that a
  slow `Action` does not block a concurrent `Add` or `Remove`.

## Verification

`make verify` passes, once this phase's code lands. The coverage floor
for `trigger` holds at 85 or above. `api/trigger.txt` is created by
`make api-update` in this phase's own change, locking `Condition`,
`Action`, `Registry`, `New`, `Add`, `Remove`, `Fire`, and the five
sentinel errors. `policy/layers.json` gains a `trigger` row set to
`[]`, added by this plan ahead of the code, matching the gate's own
rule that a new package needs a row before it has code.
`scripts/check_deps.py` passes with no edge from `trigger` to
`scheduler`, `channel`, `events`, `agent`, `flow`, or `tools`, and no
edge from any of those to `trigger` (composition happens in caller
code). `scripts/check_plan.py` passes once `docs/plans/trigger.md`
exists, written from `docs/plans/TEMPLATE.md`.

`go test -race ./trigger/...` passes for the concurrent `Add`,
`Remove`, and `Fire` paths. `AGENTS.md`'s package layout list gains a
`trigger/` bullet, matching the existing bullets' level of detail:
package name, one-sentence purpose, and its import edges (none).
