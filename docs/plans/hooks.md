# Plan: hooks

Status: shipped. One new package, `hooks`, with zero internal import
edges. It depends on no unshipped phase and ships with no caller, the
same way `tools` shipped in phase 14. This plan folded in from
`docs/plans/agents/phase57_hooks.md` on shipping; no standalone phase
57 plan file remains.

## Goal

Give a caller a named, multi-handler registry for a lifecycle point.
A caller registers more than one handler at the same point. Each
handler observes or vetoes the action in progress. `Fire` runs every
handler for a point, in registration order, and stops at the first
veto.

## Scope

`hooks` is a new top-level leaf package. It imports no other package
in this module, matching `trigger`'s row in `policy/layers.json`.

The sibling repo `mivia-agent` runs a deterministic PreToolUse,
PostToolUse, and Stop lifecycle-hook system today, in its own
`internal/hooks` package. That system lets a caller register many
named handlers per lifecycle point and lets each handler veto or
observe an action. Its protocol denies a handler that returns a
rewritten input instead of applying it. This SDK ships two narrower
gates today: `tools.Scope.Approve`, one caller-supplied callback per
`RunScoped` call, and `flow.Confirm`, one caller-supplied callback
per step. Neither is a named registry a caller can add to and remove
from on its own. Neither package covers a point after the action
runs. This phase closes both gaps as one small, reusable leaf.

Inside:

- A `Point` type naming a lifecycle point, with named constants for
  the three points a real caller needs today: before a tool runs,
  after a tool runs, and at a run's stop.
- A `Handler` function type. It reads a context and an opaque
  payload and returns an allow decision plus an error.
- A `Registry` type holding named handlers, grouped by `Point`.
- `Add`, `Remove`, and `Fire` methods, matching the `Add`/`Remove`/
  `Fire` naming `trigger.Registry` already uses in this module.
- A mutex-guarded map, matching `tools.Registry` and
  `trigger.Registry`'s concurrency shape. `Fire` releases the lock
  before it calls a handler, so a slow handler never blocks a
  concurrent `Add` or `Remove` on an unrelated point.

Outside:

- Any wiring into `tools` or `flow`. This phase ships the registry
  alone, with no caller yet, the same way phase 14 shipped `tools`
  before any agent wired it in. A later phase can let
  `tools.RunScoped` and `flow.Confirm` each optionally consult a
  `hooks.Registry` before or after their own gate runs. That wiring
  needs its own plan review and its own `policy/layers.json` edge on
  `tools` or `flow`; this phase adds neither.
- Async or background handler execution. `Fire` is synchronous. It
  calls each handler in place and blocks until that handler returns,
  matching `tools.Scope.Approve`'s existing synchronous contract. A
  caller who needs an out-of-band answer builds that flow itself,
  the same way `tools`'s plan already states for `Approve`.
- A second, competing event system to `events.Bus`. `hooks` never
  imports `events` and holds no `Bus`. Both run handlers in order.
  `Fire` short-circuits on the first veto or handler error and
  reports it through `ErrVetoed` or the wrapped error. `Emit` runs
  every handler regardless and discards every handler error. `Fire`
  treats a point with no handler as a no-op nil; `Emit` rejects a
  name with no subscriber.
- In-place payload modification. See the API section for the
  reasoning.
- Persistence of a registered handler across a process restart. A
  `Registry` is an in-memory, caller-owned value, matching every
  other registry-shaped leaf package in this module.
- A point beyond the three constants this phase ships. A future
  phase adds a point only once a real caller needs it. Shipping a
  wider point set now is speculative generality.

A caller composes `hooks.Registry` with `machine.Guard` and
`flow.Confirm` today, without a new SDK edge. A domain-specific gate,
for example a git diff review before a step runs, is a closure a
caller writes once and hands to `Guard`, `Confirm`, or a `hooks.
Handler`. The SDK never names git, a diff, or a pull request; the
caller's closure does. This mirrors `flow.RetryPolicy.Retryable`,
which takes a caller-supplied `func(error) bool` instead of a
built-in error taxonomy. `hooks` adds the missing piece: a named,
multi-handler point a caller can add to and remove from at run time,
where `Guard` and `Confirm` each hold exactly one callback.

## API

The surface below lands in `api/hooks.txt`.

- `type Point int` — a lifecycle point a `Registry` groups handlers
  under. An int-based enum, matching `flow.Outcome` and
  `flow.Admission`, not a string-based one; the no-string-literal
  enum rule in `semgrep/sdk-standards.yml` covers the string-typed
  enums (`Intent`, `Epistemic`, `AckStatus`, `Role`,
  `tools.ExecutionClass`) and does not need a new entry for `Point`.
- `const pointUnset Point = iota` — the unexported zero value. A
  `Point` a caller never set stays invalid, the same way a caller
  must name a real `machine.Status` rather than rely on a usable
  zero value.
- `const PointPreTool` — before a tool call runs.
- `const PointPostTool` — after a tool call runs, success or
  failure. This is the point the SDK has no equivalent for today.
- `const PointStop` — at a run's stop.
- `func (p Point) Validate() error` — rejects `pointUnset` and any
  value outside the three named constants.
- `func (p Point) String() string` — a short label for each named
  constant, used in `Fire`'s wrapped error messages. Returns
  `"unknown"` for an invalid value, never a panic, matching
  `a2aclient.State.String`. `Fire`'s wraps already carry the
  `hooks:` prefix, so a prefixed label would double it.
- `type Handler func(ctx context.Context, payload any) (bool, error)`
  — one registered hook. `payload` is opaque to `hooks`: the caller
  that fires a point supplies whatever value that point's real
  action carries, for example a tool name and its input, or a run's
  final record. `Handler` returns `true, nil` to allow the action to
  continue, `false, nil` to veto it, or a non-nil error when the
  handler itself failed to decide.
- `type Registry struct` — holds handlers by `Point`, in registration
  order. Unexported fields. Built only through `New`.
- `func New() *Registry` — creates an empty `Registry`.
- `func (r *Registry) Add(point Point, name string, h Handler) error`
  — registers `h` under `name`, at `point`. Rejects an invalid
  `point` (`point.Validate()`), a blank `name` (empty after
  `strings.TrimSpace`) with `ErrBlankName`, a nil `h` with
  `ErrNilHandler`, and a `name` already registered at that same
  `point` with `ErrDuplicateName`. The same `name` may register at
  two different points; `name` scopes to one `Point`, not to the
  whole `Registry`, so `PointPreTool` and `PointPostTool` handlers
  can share a label such as `"audit-log"`.
- `func (r *Registry) Remove(point Point, name string) bool` —
  removes `name` from `point`. Returns whether it was present,
  matching `tools.Registry.Remove` and `trigger.Registry.Remove`'s
  exact contract.
- `func (r *Registry) Fire(ctx context.Context, point Point, payload any) error`
  — runs every handler registered at `point`, in registration order.
  An invalid `point` returns its `Validate` error at once, with no
  handler call. A `point` with no registered handlers returns nil at
  once: a no-op success. A handler returning `true, nil` lets `Fire`
  continue to the next handler. A handler returning `false, nil`
  stops `Fire` at once and returns `ErrVetoed` wrapped
  `hooks: %s: handler %q: %w`. A handler returning a non-nil error
  stops `Fire` at once and returns that error wrapped
  `hooks: %s: handler %q: %w`. The veto wrap embeds `ErrVetoed`;
  the failure wrap embeds the handler's error, so `errors.Is` tells
  the two apart.
  `Fire` returns nil once every registered handler at `point` has
  returned `true, nil`.
- Sentinel errors, tested with `errors.Is`: `ErrBlankName`,
  `ErrNilHandler`, `ErrDuplicateName`, `ErrVetoed`.

### Why `(bool, error)`, not a `Decision` enum or a modify-in-place return

`Handler` returns `(bool, error)`, the same two-value shape
`tools.ScopeOptions.Approve` already uses. A three-state
`Decision` enum (allow, veto, modify) buys nothing over a bool for
the two outcomes this phase's only stated need covers: continue, or
stop the chain. Modify-in-place needs a third return value, a
changed payload, which forces every existing and future `Handler` to
decide what an unchanged payload means and forces `Fire` to thread a
mutated value back through every remaining handler in the chain.
No caller in this module names a required payload rewrite. The
sibling's own protocol denies a rewritten input, so no reference
contract exists to build against. Adding modify now, ahead of a
real caller, is the speculative generality AGENTS.md's Building
blocks section forbids. A future phase that needs modification faces
two routes. A third return value breaks `Handler`: the type changes
and every registered literal stops compiling. The additive route is
a mutable payload holder the caller's handlers agree on. No caller
needs either today.

## Tests

Test files live in `hooks/hooks_test/`, an external test package.
The invalid-point cases use `Point(0)` for the zero value because an
external test package cannot name `pointUnset`.

- `registry_add_test.go` — red-green cases for `Add`: an invalid
  `Point` returns its `Validate` error, through both `Point(0)` and
  the out-of-range `Point(99)`; a blank `name` returns
  `ErrBlankName`; a nil `Handler` returns `ErrNilHandler`; a
  duplicate `name` at the same `Point` returns `ErrDuplicateName`;
  the same `name` at two different points both succeed; `Remove`
  returns true for a present `(point, name)` pair and false for an
  absent one, and a following `Remove` on the same pair returns
  false.
- `point_test.go` — `Point.String` cases: `String` returns each
  named constant's label for `PointPreTool`, `PointPostTool`, and
  `PointStop`; `Point(99).String()` returns `"unknown"`; no value,
  valid or invalid, panics.
- `registry_fire_test.go` — red-green cases for `Fire`: an invalid
  `Point` returns its `Validate` error and calls no handler, through
  both `Point(0)` and the out-of-range `Point(99)`; a `Point` with
  no registered handlers returns nil; one allowing handler returns
  nil; a veto from a middle handler in a three-handler chain stops
  the chain, returns `ErrVetoed` under `errors.Is`, and the third
  handler never runs, proven by a counter; multiple handlers at the
  same point all run, in registration order, when none veto, proven
  by an ordered-append slice; every handler run receives the exact
  payload value passed to `Fire`, proven by handlers that compare
  against it; a handler returning a non-nil error stops the chain
  and returns that error wrapped, distinct from `ErrVetoed` under
  `errors.Is`; a handler registered at `PointPreTool` never runs on
  a `Fire` call for `PointPostTool`.
- `hooks_integration_test.go` — two handlers register at
  `PointPreTool`: the first is observational, appends to a shared
  slice, and returns `true, nil`; the second vetoes. `Fire` returns
  `ErrVetoed`, the observational handler's append landed, and a
  third, never-registered check confirms no handler ran twice.
- `registry_concurrent_test.go` — modeled on
  `trigger`'s `registry_concurrency_test.go` pattern: N goroutines
  call `Add` for distinct names at the same point concurrently, then
  join; a following `Fire` call runs all N handlers, under
  `go test -race`. A second case races `Fire` calls against `Remove`
  calls for one handler's name and asserts no panic and no data
  race, matching `tools`'s `registry_run_scoped_concurrent_test.go`
  shape.
- `registry_fire_bench_test.go` — a benchmark of `Fire` over a point
  with ten registered, always-allowing handlers, against a point
  with one handler. States the allocation budget for one `Fire`
  call.

## Verification

`make verify` passes. The coverage floor for `hooks` holds at or
above 85 percent. `api/hooks.txt` lands via `make api-update` and
locks `Point`, `pointUnset`'s absence (unexported), `PointPreTool`,
`PointPostTool`, `PointStop`, `Point.Validate`, `Point.String`,
`Handler`, `Registry`, `New`, `Add`, `Remove`, `Fire`,
`ErrBlankName`, `ErrNilHandler`, `ErrDuplicateName`, and `ErrVetoed`.

The `hooks` row in `policy/layers.json` already exists, set to `[]`,
added ahead of the code alongside the `trace` and `usage` rows. It
carries this plan's claim: no internal imports.
`scripts/check_deps.py` passes with no edge from `hooks` to any
other package, and no edge from any other package to `hooks`, since
no caller wires it in this phase.

`go test -race ./hooks/...` passes for the concurrent `Add`,
`Remove`, and `Fire` paths. `AGENTS.md`'s package layout list gains
a `hooks/` bullet, at the same level of detail as the `trigger/`
bullet: package name, one-sentence purpose, and its import edges
(none). `docs/plans/agents/PHASES.md` records phase 57's dependency
on no unshipped phase, matching how it records phase 51 and phase
52.
