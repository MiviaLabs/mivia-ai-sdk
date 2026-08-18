# Phase 36: tool approval gating

Status: future. Depends on phase 31 (tools execution profile markers,
shipped; see docs/plans/tools.md), extending the `tools` package again
the same additive way phase 31 extended phase 14. It adds no new
package and no new import edge.

## Why a new phase, not a phase 31 revision

Phase 31 passed three plan-review rounds as written and has since
shipped. Reopening that design to add approval gating risks unrelated
churn on a design that already cleared review. `PHASES.md` already has
this pattern: phase 33 builds on phase 34, phase 30 builds on phases
22 and 23, each as its own phase file layered on a landed or reviewed
dependency. This plan follows the same shape: a phase that builds on
phase 31's shipped `Scope` and `RunScoped`, instead of amending
phase 31 itself.

## Goal

Let a caller require a yes-or-no decision before `RunScoped` executes
a specific tool call, for any tool whose execution risk meets or
exceeds a threshold the caller sets. The decision is synchronous: a
caller-supplied function returns approved-or-not before `RunScoped`
calls the tool. `tools` adds no I/O, no goroutine, and no transport of
its own to collect that decision.

## Scope

Inside: a `ToolCall` type describing one call eligible for approval,
an `Approve` field and an `ApprovalThreshold` field on
`ScopeOptions`, the resolved approval state on `Scope`, an approval
check inside `RunScoped`, and a sentinel error, `ErrToolDeclined`, for
a call an `Approve` function turns down.

Outside: any UI, prompt, or transport that delivers the approval
question to a human or a policy engine. That is a caller concern, the
same way phase 14's plan states a tool never sees the agent. Outside:
any persistence of an approval decision. Outside: any async or
event-driven approval flow inside `tools`; see "Disclosed scope limit:
synchronous approval only" below. Outside: any change to `Tool`,
`Registry.Add`, `Registry.Get`, `Registry.Remove`, `Registry.Run`,
`ExecutionClass`, `ExecutionProfile`, `ProfiledTool`,
`ResultBudgetTool`, or `PrivilegedTool` beyond what this phase strictly
needs to add the approval hook. `Scope.Allowed`'s existing narrowing
rules (denylist, allowlist, privileged) stay unchanged; approval is an
added check, not a replacement for them.

This phase touches only `tools/`. `policy/layers.json` keeps the
`tools` row at `[]`. `ToolCall`, `Approve`, and the approval check use
only `context`, the same standard-library-only footprint phase 14 and
phase 31 already use. No internal import is added.

### Where the hook lives: `ScopeOptions`, not a new `RunScoped` parameter

`RunScoped`'s shipped signature is
`(ctx context.Context, name string, in InOut, scope *Scope) (Out, error)`.
Adding a fifth parameter for an approval function would break that
signature the moment it locks. `Scope` already narrows which tools a
run may invoke; approval narrows *when* a narrowly-allowed call still
needs external sign-off before it runs. Both concerns belong to the
same value a caller builds once per run and passes to every
`RunScoped` call. This phase adds `Approve` and `ApprovalThreshold` to
`ScopeOptions`, so a caller builds both the allow rules and the
approval rule through one `NewScope` call, and `RunScoped` keeps its
phase 31 signature unchanged.

### The threshold: a `Scope` field, not a `RunScoped` argument or a hardcoded rule

Two designs were considered for deciding which calls need approval.

- Hardcode "write and external classes always need approval" inside
  `RunScoped`. Rejected: it removes the caller's ability to tune the
  bar per run, and it silently changes behavior for every `RunScoped`
  caller the moment `Approve` is set, with no place to opt out for one
  tool.
- A `RunScoped` argument carrying the threshold. Rejected for the same
  reason a `Scope` argument, not a `Run` argument, carries the
  allowlist in phase 31: the threshold is a property of the run's
  policy, decided once, not a per-call argument a caller must repeat
  correctly at every call site.

This phase picks `ApprovalThreshold ExecutionClass` on `ScopeOptions`.
`RunScoped` calls `Approve` only when `scope.Approve` is non-nil and
the resolved tool's `ExecutionProfile.Class` ranks at or above
`ApprovalThreshold`. A `Scope` built with `Approve` set and
`ApprovalThreshold` left at its zero value, `ExecutionClassUnclassified`,
gates every call, including an unclassified tool. A caller that wants
approval gating only for `write` and `external` calls sets
`ApprovalThreshold: ExecutionClassWrite`. This mirrors phase 31's
choice to make `ExtraDenylist` and `Allowlist` explicit caller-set
fields instead of a fixed rule baked into `Scope.Allowed`.

### Ranking `ExecutionClass`: cautious by default, the opposite of `Scope.Allowed`

`ExecutionClass` is a set of string constants with no declared order.
Comparing "meets or exceeds a threshold" needs a rank. This phase adds
one unexported rank, used only inside the approval check:
`ExecutionClassUnclassified` ranks lowest, then `ExecutionClassRead`,
then `ExecutionClassWrite`, then `ExecutionClassExternal` highest. A
`ProfiledTool` that publishes a `Class` outside the four constants
ranks at the same level as `ExecutionClassExternal`, the highest rank.

Phase 31 documents the opposite default for `Scope.Allowed`: an
out-of-enum `Class` never blocks a call, because `Allowed` does not
read `Class` at all. This phase's approval rank does read `Class`, and
it treats an unrecognized value as the most cautious case on purpose.
Approval gating exists to catch a call a caller did not plan for; an
unrecognized `Class` is exactly that case. Defaulting it to the lowest
rank would let a tool skip approval by publishing a `Class` string the
four constants do not name. Defaulting it to the highest rank asks for
a decision instead, the safer failure mode for a gate whose entire job
is asking before a risky action runs.

### What happens while approval is pending: synchronous only

`tools` has no I/O and no goroutine model of its own; `Registry.Run`
and `RunScoped` are direct, blocking calls. This phase keeps that
shape. `Approve` is `func(ctx context.Context, call ToolCall) (bool, error)`.
`RunScoped` calls it in place, on its own goroutine, and blocks until
it returns, the same way `agent.AckWait` already blocks `agent.Run`
until a caller-supplied function resolves one step's ack. A caller
that needs the answer to come from a human on a different timescale
gives `Approve` a function that blocks on a channel, a `context.Context`
timeout, a polling loop, or a call into `flow`'s phase 25 checkpoint
mechanism to pause a whole run while a human answers out of band.
`tools` composes none of that. It only calls the function once and
reads its two return values.

### Disclosed scope limit: synchronous approval only

`tools` never pauses a `flow.Run` graph walk, never checkpoints a run,
and never emits an event a separate process later resolves. A caller
that wants an out-of-band, asynchronous approval flow, for example a
`flow` run that checkpoints out, waits for a human to answer through a
web form hours later, then resumes, builds that flow itself out of
phase 25's `Checkpoint`, `onCheckpoint` hook, and `Resume` function,
with an `Approve` function on this phase's `Scope` that only decides
once the out-of-band answer already exists. This phase adds no
mechanism for that composition. This is a known, disclosed scope
limit, not a gap this phase leaves accidentally: `RunScoped`'s
`Approve` call is a plain, synchronous function call, and nothing in
`tools` may become asynchronous without giving the package a goroutine
or channel model it does not have today.

### `ErrToolDeclined`: guaranteed by `RunScoped`, not left to the caller's `Approve`

`RunScoped` distinguishes three outcomes from an approval check.
`Approve` returns `(true, nil)`: the call proceeds to `Run`. `Approve`
returns `(false, nil)`: `RunScoped` returns `ErrToolDeclined`, a
sentinel this package defines and guarantees, so every caller can test
`errors.Is(err, ErrToolDeclined)` without depending on how a specific
`Approve` implementation signals a decline. `Approve` returns a
non-nil error: `RunScoped` returns that error unchanged, distinct from
`ErrToolDeclined`, so a caller can tell "the approval mechanism itself
failed" (a timeout asking Slack, a policy-engine outage) apart from
"a human or policy said no". This three-way split follows the
sentinel-error convention this repo already uses:
`identity.ErrKeyFormat` versus `identity.ErrKeyInvalid` separates two
distinct failure shapes the same way, and `room.ErrNoMonitor` is a
guaranteed sentinel a caller checks with `errors.Is`, not a value the
caller must construct correctly itself.

This phase considered `agent.AckWait`'s shape, `func(ctx, msg) (envelope.Ack, error)`,
where the implementation wraps `agent.ErrEscalated` with `%w` to
signal escalation, instead of a `(bool, error)` return. That shape
was rejected here because it puts the guarantee in the wrong place: a
caller's `Approve` function would have to remember to wrap
`ErrToolDeclined` correctly every time, and a caller that forgets
produces a decline `RunScoped` cannot distinguish from any other
error. `(bool, error)` lets `RunScoped` itself produce the guaranteed
sentinel, so the guarantee holds regardless of how `Approve` is
written.

### Audit and events: stays a caller concern, `tools` stays a leaf

A related question is whether `RunScoped` should emit an auditable
record of every approval decision. `tools`'s row in
`policy/layers.json` is `[]`; it is the module's leaf package, with no
internal import, so any future caller in this module can depend on it
with no risk of a cycle. Importing `events` would break that
property for a concern this phase does not need: `RunScoped` already
returns enough for a caller to build its own audit record. `Out`,
`error`, and the `ToolCall` value passed to `Approve` give a caller
every fact an audit log needs, at the exact call site `RunScoped`
runs from. A caller that wants a typed event wraps its own `Approve`
function to publish one on an `events.Bus` it owns before it returns
its decision, the same way a caller wires any other side effect into
a function value this package calls. This phase adds no `events`
import to `tools` and no audit type of its own. Leaf-package purity is
the strong precedent set by phase 14 and kept by phase 31; this phase
finds no reason strong enough to break it for an audit concern a
caller can already cover through the function value it supplies.

## API

The additions below extend phase 31's shipped, locked shape. Every
entry lands in `api/tools.txt` via `make api-update` in this phase's
own change.

- `type ToolCall struct { Name string; In InOut; Profile ExecutionProfile }`
  — describes one call `RunScoped` is about to make, passed to
  `Approve`. `Name` is the resolved tool's registration name. `In` is
  the caller's input payload, unchanged from the `RunScoped` call.
  `Profile` is `ExecutionProfileOf(t)` for the resolved tool.
- `ScopeOptions` gains two fields: `Approve func(ctx context.Context, call ToolCall) (bool, error)`
  and `ApprovalThreshold ExecutionClass`. Both are optional; a
  `ScopeOptions` with neither set behaves exactly as phase 31 shipped
  it, with no approval check.
- `(*Scope).Allowed` keeps its phase 31 signature and behavior
  unchanged; approval is a separate check `RunScoped` runs after
  `Allowed` returns true, not a change to `Allowed` itself.
- `(*Registry).RunScoped` keeps its phase 31 signature,
  `(ctx context.Context, name string, in InOut, scope *Scope) (Out, error)`.
  Its body gains one new branch: after `scope.Allowed` passes, when
  `scope.Approve` is non-nil and the resolved tool's rank meets or
  exceeds `scope.ApprovalThreshold`'s rank, `RunScoped` calls
  `scope.Approve(ctx, ToolCall{...})` before it calls `Run`. A nil
  `scope`, matching phase 31, skips both `Allowed` and this check and
  behaves like `Run`.
- `var ErrToolDeclined = errors.New(...)` — `RunScoped`'s error when
  `Approve` returns `(false, nil)`. Test with `errors.Is`.

No change lands on `Tool`, `Registry.Add`, `Registry.Get`,
`Registry.Remove`, `Registry.Run`, `ExecutionClass`,
`ExecutionProfile`, `ProfiledTool`, `ResultBudgetTool`, or
`PrivilegedTool`.

## Tests

Test files live in `tools/tools_test/`, beside the phase 14 and phase
31 suites:

- `tool_call_test.go` — red-green cases proving `RunScoped` builds
  `ToolCall` with the resolved tool's name, the caller's `In` value
  unchanged, and `ExecutionProfileOf(t)` as `Profile`, for a tool that
  implements `ProfiledTool` and for one that does not (zero
  `ExecutionProfile`, `Class == ExecutionClassUnclassified`).
- `approval_threshold_test.go` — red-green cases for the rank order.
  A tool ranked below `ApprovalThreshold` runs with no `Approve` call,
  proven by a counter the test's `Approve` increments. A tool ranked
  at or above `ApprovalThreshold` triggers exactly one `Approve` call.
  A tool with an out-of-enum `Class` triggers `Approve` at any
  threshold at or below `ExecutionClassExternal`, proving the
  cautious-default rank.
- `run_scoped_approval_test.go` — red-green cases for `RunScoped`'s
  three-way outcome. `Approve` returning `(true, nil)` runs the tool
  and returns its result. `Approve` returning `(false, nil)` returns
  `ErrToolDeclined` and never calls the tool's `Run`, proven by a
  counter on a stub `Tool`. `Approve` returning a non-nil error
  returns that exact error, unwrapped, and never calls `Run`. A `nil`
  `Approve` field skips the check entirely, matching a `Scope` with no
  approval configured. A `nil` `scope` argument skips the check,
  matching phase 31's existing nil-scope behavior.
- `run_scoped_approval_order_test.go` — proves ordering: a name denied
  by `Allowed` (denylist, absent allowlist, or unapproved privileged
  tool) returns `ErrScopeDenied` and never calls `Approve`, even when
  `Approve` is set and would return true. This proves `Allowed` gates
  before `Approve` runs.
- `run_scoped_approval_integration_test.go` — register a read-class
  tool and a write-class tool implementing `ProfiledTool` in one
  `Registry`. Build a `Scope` with `ApprovalThreshold: ExecutionClassWrite`
  and an `Approve` function that denies every call. Prove `RunScoped`
  runs the read tool with no `Approve` call and denies the write tool
  with `ErrToolDeclined`. Prove `Registry.Run`, unscoped, still runs
  both tools with no approval check, showing the phase 14 and phase 31
  paths stay unchanged.
- `run_scoped_approval_concurrent_test.go` — modeled on
  `registry_run_scoped_concurrent_test.go`'s pattern, required by
  `docs/plans/tools.md` for every method that touches the tools map. A
  tool requiring approval is registered. N goroutines call `RunScoped`
  under a `Scope` whose `Approve` always approves, racing against N
  goroutines calling `Remove` for the same name, under `go test -race`.
  Every call returns either the tool's result, `ErrUnknownName`, or
  `ErrToolDeclined` only if a second sub-case uses a denying `Approve`;
  no call panics.

## Verification

`make verify` passes. The coverage floor for `tools` holds, including
the new files. `api/tools.txt` gains `ToolCall`, the two new
`ScopeOptions` fields, and `ErrToolDeclined`, via `make api-update`;
every symbol phase 14 and phase 31 already locked stays unchanged.
`policy/layers.json`'s `tools` row stays `[]`. `go test -race
./tools/...` passes, covering
`run_scoped_approval_concurrent_test.go`.

`docs/plans/tools.md` and `docs/packages/tools.md` are amended in the
same change to add `ToolCall`, the `ScopeOptions.Approve` and
`ScopeOptions.ApprovalThreshold` fields, and `ErrToolDeclined`,
matching how phase 31's design was folded into `docs/plans/tools.md`
once phase 31 lands. This phase's own plan file stays as the design
history; the package plan is the current contract.

`AGENTS.md`'s `tools/` layout bullet gains `RunScoped`'s approval
outcome, `ErrToolDeclined`, if not already updated by phase 31's own
landing, matching the existing bullet's level of detail for `Run`.
