# Plan: trigger

Status: shipped. One new package, `trigger`, with zero internal import
edges. It ships independently of phase 38 (flow loop). It composes
with phase 39 (scheduler) and `channel` only through caller-owned
closures.

An architecture review flagged a risk before this plan: one interface
spanning several invocable kinds, with no single caller shape, matches
this repo's overengineering signal in `AGENTS.md`. That review
recommended holding this package until a caller scoped it. The user's
direction overrides that recommendation for this plan: `trigger` ships
now, as a real package. This plan still keeps the package to the
smallest shape that answers "what fired" and "what runs."

This Go SDK has no "skill" concept today. No package in this module
exports a `Skill` type or a skill registry. `trigger.Action` is a
plain closure shape generic enough to wrap anything invocable: a
`tools.Tool.Run` call, an `agent.Run` call, a `flow.Run` call, or a
future concept once one exists as a concrete Go value. This plan adds
no `Skill` type and no skill-specific field.

## Goal

Give every part of this SDK, and every caller of this SDK, one shared
vocabulary for "a condition fired, so run this": `Condition`,
`Action`, and a `Registry` that maps a name to one of each. `trigger`
answers "what fired" and "what runs" once, so `scheduler.Job`,
`channel.Notifier`, and `events.Bus.Handler` can each drive a
`Registry` entry without `trigger` becoming a fourth, competing event
system.

## Scope

Inside:

- The `Condition` function type and the `Action` function type.
- The `Registry` type and its `Add`, `Remove`, and `Fire` methods.
- Field checks inside `Add` that stand in for a `Validate` method.
- A mutex-guarded map, matching `tools.Registry`'s concurrency shape.
  `Fire` releases the mutex before it calls the looked-up `Action`, so
  a slow `Action` never blocks a concurrent `Add` or `Remove`.

`trigger` is a leaf package: no I/O, no goroutine, no persistence, and
no polling loop of its own.

Outside:

- A `Skill` type or any skill-specific field.
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
  `tools`. This plan edits none of their files and none of their
  plans.

### Import edges: zero, not two

`trigger` declines an import edge to both `scheduler` and `channel`.
`Condition` and `Action` are the same decoupled-invocable shape
`scheduler.Job` and `channel.Notifier`'s `Notify` method already use:
a plain function value a caller builds and hands over, never a value
`trigger` constructs or holds itself. Composition, in every case
above, is one line in caller code: a `scheduler.Job` or an
`events.Handler` or a post-`Notify` closure that calls
`registry.Fire`. Importing `scheduler` or `channel` would buy
`trigger` nothing it cannot already do through that one caller-side
line, and it would cost `trigger` its place as a leaf package that
`agent`, `flow`, `tools`, `scheduler`, and `channel` can all depend on
later with no cycle risk. `trigger` stays at zero internal imports,
matching `tools`, `discovery`, and `envelope`.

### Condition and Action: the smallest workable shapes

- `Condition func(ctx context.Context) (bool, error)` matches
  `machine.Guard`'s exact signature. Reusing it, rather than inventing
  a new predicate type, keeps one predicate shape across the module.
- `Action func(ctx context.Context) error` matches `scheduler.Job`'s
  exact signature. A `trigger.Action` and a `scheduler.Job` are
  interchangeable by shape; a caller can pass the same closure to
  either, with no adapter.

## API

The surface below lands in `api/trigger.txt`.

- `type Condition func(ctx context.Context) (bool, error)` — reports
  whether a named trigger's `Action` should run. A nil `Condition`
  passed to `Add` means "always ready," matching `machine.Guard`'s own
  nil convention.
- `type Action func(ctx context.Context) error` — the invocable a
  named trigger runs once its `Condition` is satisfied. `Add` rejects
  a nil `Action`; a trigger with nothing to run has no purpose.
- `type Registry struct` — holds named triggers. The zero value is
  ready to use, the same as `New`'s result. Safe for concurrent `Add`,
  `Remove`, and `Fire`, matching `tools.Registry`'s concurrency shape.
- `func New() *Registry` — creates an empty `Registry`.
- `func (r *Registry) Add(name string, c Condition, a Action) error` —
  registers `c` and `a` under `name`. Rejects a blank `name` (empty
  after `strings.TrimSpace`) with `ErrBlankName`, a nil `a` with
  `ErrNilAction`, and a duplicate `name` with `ErrDuplicateName`,
  mirroring `tools.Registry.Add`'s rejection order. A nil `c` is
  accepted; see `Condition` above.
- `func (r *Registry) Remove(name string) bool` — removes `name`.
  Returns whether `name` was present, matching
  `tools.Registry.Remove`'s exact contract.
- `func (r *Registry) Fire(ctx context.Context, name string) error` —
  resolves `name`, evaluates its `Condition` (a nil `Condition` reads
  as true), and, when true, calls its `Action` and returns that call's
  error. Returns `ErrUnknownName` when `name` is not registered.
  Returns `ErrConditionNotMet` when the `Condition` evaluates false,
  without calling `Action`. Returns a `Condition` evaluation error
  wrapped `trigger: %q: %w`, without calling `Action`.
- Sentinel errors: `ErrBlankName`, `ErrNilAction`, `ErrDuplicateName`,
  `ErrUnknownName`, `ErrConditionNotMet` — returned by `Add` and
  `Fire`, tested with `errors.Is`.

No interface beyond the two func types. No `Kind` or `Urgency`
classification: no caller in this module branches on a trigger's kind
today. A future phase adds a classification only once a real caller
needs to route on it.

### Composition patterns, in prose

These are caller-code patterns this plan documents but does not build.
None of them adds an import edge to `trigger`.

- Scheduled polling: a caller wraps `registry.Fire(ctx, name)` in a
  `scheduler.Job` and adds it to a `scheduler.Scheduler` with an
  `Every` or `At` schedule. A caller who wants `ErrConditionNotMet` to
  not count as a `scheduler.JobFailedEvent` checks for it with
  `errors.Is` inside the wrapping closure and returns nil for that
  case.
- Event-driven firing: a caller subscribes an `events.Handler` to an
  `events.Bus` whose body calls `registry.Fire(ctx, name)` once the
  subscribed event arrives.
- Answer-gated firing: a caller's `channel.Notifier` implementation
  calls `registry.Fire(ctx, name)` after it receives an `Answer` with
  `Approved` true.

## Tests

Test files live in `trigger/trigger_test/`, an external test package.

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
  `Registry`, and locally declared stand-ins for `scheduler.Job`,
  `events.Handler`, and `channel.Notifier`'s signatures. This proves
  shape compatibility with no import edge from `trigger` to any of the
  three packages. Cases:
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
  -race`. Proves the mutex-guarded map has no data race and that a
  slow `Action` does not block a concurrent `Add` or `Remove`.

## Verification

`make verify` passes. The coverage floor for `trigger` holds at 85
percent or above. `api/trigger.txt` locks `Condition`, `Action`,
`Registry`, `New`, `Add`, `Remove`, `Fire`, and the five sentinel
errors.

`policy/layers.json` carries a `trigger` row set to `[]`, added by
this plan ahead of the code, matching the gate's rule that a new
package needs a row before it has code. `scripts/check_deps.py` passes
with no edge from `trigger` to `scheduler`, `channel`, `events`,
`agent`, `flow`, or `tools`, and no edge from any of those to
`trigger`. `scripts/check_plan.py` passes with this file in place.

`go test -race ./trigger/...` passes for the concurrent `Add`,
`Remove`, and `Fire` paths. `AGENTS.md`'s package layout list gains a
`trigger/` bullet, matching the existing bullets' level of detail:
package name, one-sentence purpose, and its import edges (none).
