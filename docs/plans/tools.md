# Plan: tools

Status: shipped. `Registry.Remove` was added for symmetry with
`room.Room.Admit`/`Remove`, agreed in architecture review.
`ExecutionClass`, `ExecutionProfile`, `ProfiledTool`,
`ResultBudgetTool`, `PrivilegedTool`, `Scope`, `ScopeOptions`,
`NewScope`, `ExecutionProfileOf`, `ResultBudgetOf`, `IsPrivileged`,
`RunScoped`, and `ErrScopeDenied` extend the execution-risk surface,
shipped in phase 31: optional markers a `Tool` may implement, and a
`Scope` that narrows which tools a run may invoke. Phase 36 extended
`RunScoped` again, the same additive way phase 31 extended phase 14:
`ToolCall`, `ScopeOptions.Approve`, `ScopeOptions.ApprovalThreshold`,
and `ErrToolDeclined` add a synchronous approval gate `RunScoped` runs
after `Allowed` passes.

## Goal

Let an agent call a named action without knowing its concrete type. A
tool registers under a name. The registry resolves the name and runs
the tool. An unknown name fails the same way at lookup and at run.

## Scope

Inside: the `Tool` interface, the `Registry`, and tool execution.
Registration, lookup, removal, and run all live here. Inside: optional
execution-risk markers a `Tool` may implement (`ProfiledTool`,
`ResultBudgetTool`, `PrivilegedTool`), the `ExecutionClass` enum and
`ExecutionProfile` struct those markers publish, a `Scope` that narrows
which tools a run may invoke, and `Registry.RunScoped`, the scoped
counterpart to `Run`.

Outside: the agent binding. A future phase wires a `Registry` into an
agent. A tool never sees the agent. Outside: the memory store. Phase
15 owns memory. The `tools` package does not import `agent` or a
future `memory` package. Outside: any mivia-specific field on
`ExecutionProfile` or `Scope`. The shape stays generic so any caller in
this module, or a future one, can reuse it. Outside: any change to
`Tool`, `Registry.Add`, `Registry.Get`, `Registry.Remove`, or
`Registry.Run`. Execution-profile checks are opt-in through the new
`RunScoped` method, never a hidden check inside `Run`.

Inside, added in phase 36: a `ToolCall` type describing one call
eligible for approval, an `Approve` field and an `ApprovalThreshold`
field on `ScopeOptions`, an approval check inside `RunScoped`, and
`ErrToolDeclined`, a sentinel for a call `Approve` turns down.

Outside, in phase 36: any UI, prompt, or transport that delivers the
approval question to a human or a policy engine. That is a caller
concern, the same way a tool never sees the agent. Outside: any
persistence of an approval decision. Outside: any async or
event-driven approval flow. `Approve` is a plain, synchronous function
call; `RunScoped` calls it in place and blocks until it returns.
`tools` adds no goroutine or channel model of its own. A caller that
needs an out-of-band answer, for example a human replying hours later,
builds that flow itself outside `tools`, with an `Approve` function
that only decides once the out-of-band answer already exists. Outside:
any change to `Tool`, `Registry.Add`, `Registry.Get`,
`Registry.Remove`, `Registry.Run`, `ExecutionClass`,
`ExecutionProfile`, `ProfiledTool`, `ResultBudgetTool`, or
`PrivilegedTool` beyond the approval hook. `Scope.Allowed`'s denylist,
allowlist, and privileged rules stay unchanged; approval is an added
check `RunScoped` runs after `Allowed` passes, not a replacement for
it. Outside: any audit or event record of an approval decision.
`tools`'s row in `policy/layers.json` stays `[]`; a caller that wants
a typed event wraps its own `Approve` function to publish one on an
`events.Bus` it owns.

`ApprovalThreshold` compares against an unexported rank order over
`ExecutionClass`, used only inside the approval check:
`ExecutionClassUnclassified` ranks lowest, then `ExecutionClassRead`,
then `ExecutionClassWrite`, then `ExecutionClassExternal` highest. A
`ProfiledTool` that publishes a `Class` outside the four constants
ranks at the highest rank, the opposite default from `Scope.Allowed`,
which never reads `Class` at all. Approval gating treats an
unrecognized `Class` as the most cautious case on purpose: an
unrecognized value must not let a tool skip approval.

`RunScoped` calls `scope.Approve` only when `scope.Approve` is
non-nil and the resolved tool's rank meets or exceeds
`scope.ApprovalThreshold`'s rank, after `scope.Allowed` passes.
`Approve` returning `(true, nil)` runs the tool. `Approve` returning
`(false, nil)` makes `RunScoped` return `ErrToolDeclined`, guaranteed
by `RunScoped` itself so every caller can test
`errors.Is(err, ErrToolDeclined)` regardless of how `Approve` is
written. `Approve` returning a non-nil error makes `RunScoped` return
that error unchanged, distinct from `ErrToolDeclined`, so a caller can
tell an approval-mechanism failure apart from a decline.

Phase 16 runs the tool registry as a flow step. A panel step runs in
its own goroutine, so more than one goroutine can call `Add`, `Get`,
`Remove`, `Run`, and `RunScoped` on the same `Registry` at once, once
that wiring lands. This plan states the concurrency contract now,
ahead of that caller, matching how `room.Room`, `events.Bus`, and
`heartbeat.Monitor` each state their contract in their own plan before
every caller existed.

A tool that does not implement `ProfiledTool` is unclassified.
`ExecutionProfileOf` reports `ExecutionClassUnclassified`, the zero
value, for such a tool. Every tool that predates the execution-risk
surface stays valid with no change.

`Scope` narrows only. Built once from `ScopeOptions{Allowlist,
ExtraDenylist}` through `NewScope`. `ExtraDenylist` always removes a
name from the allowed set, even when `Allowlist` also names it.
`Allowlist`, when non-empty, keeps only the named tools; when empty,
every tool that is not denied and not privileged is allowed. A tool
that implements `PrivilegedTool` and reports true is denied unless its
name appears in `Allowlist`. No operation on a built `Scope` can re-add
a name `ExtraDenylist` removed.

`ExecutionProfileOf` and `RunScoped` never call `ExecutionClass.
Validate`. `Scope.Allowed` reads only `PrivilegedTool`, `Allowlist`,
and `ExtraDenylist`; it never reads `Class`. An out-of-enum `Class`
value passes through `ExecutionProfileOf` unchanged and never blocks
`RunScoped`. `Validate` exists for a caller that builds an
`ExecutionProfile` by hand and wants to check it before registering the
tool.

### ResourceKey and MaxResultBytes: published, not enforced

This phase publishes `ExecutionProfile.ResourceKey` and
`ResultBudgetTool.MaxResultBytes` as metadata only. No function reads
`ResourceKey` to dedup a call. Neither `Run` nor `RunScoped` reads
`ResultBudgetOf` to truncate or reject an oversized result.
`RunScoped` runs the tool the same way `Run` does; it checks only
`Scope.Allowed`. Enforcement is deferred to the future
agent-binding caller named in the roadmap's "Precedent for shipping
with no caller yet" section, the same caller that will wire a
`Registry` into `agent.Run`.

`ExecutionProfile.Timeout` left this trio. The registry now enforces
it: every `Run` and `RunScoped` dispatch carries a per-call deadline.
See docs/packages/tools.md's "Run timeout backstop" section, and
docs/plans/tools-run-backstop.md for the plan that added it.

## API

- `type InOut struct { Value any }` — the tool input payload. A tool
  reads its typed argument through `Value` and asserts the concrete
  type it expects.
- `type Out struct { Value any }` — the tool output payload. A tool
  writes its typed result through `Value`.
- `type Tool interface { Name() string; Run(ctx context.Context, in InOut) (Out, error) }`
  — a named action. `Name` returns the registration key. `Run`
  performs the action and returns its result or an error.
- `type Registry struct` — holds tools by name. Unexported fields.
  Built only through `New`. Registry is safe for concurrent Add, Get,
  Remove, and Run; a sync.RWMutex guards the map.
- `New() *Registry` — builds an empty registry.
- `(*Registry).Add(t Tool) error` — registers `t` under `t.Name()`.
  Rejects a nil `t` with a sentinel error, before it calls `t.Name()`.
  Rejects a blank name (empty after `strings.TrimSpace`) with a
  sentinel error, matching `room.Room.Admit`'s id check. Rejects a
  duplicate name with a sentinel error.
- `(*Registry).Get(name string) (Tool, bool)` — resolves a name.
  Returns `false` when the name is absent.
- `(*Registry).Remove(name string) bool` — removes a name. Returns
  whether the name was present. Removing an absent name is not a
  fault; it returns `false` and changes nothing. After `Remove`,
  `Get` returns `false` for that name, and `Run` fails with the same
  error as any unknown name.
- `(*Registry).Run(ctx context.Context, name string, in InOut) (Out, error)`
  — resolves `name` through `Get` and calls the tool's `Run`. Returns
  the unknown-name error when `Get` reports `false`.

### InOut and Out: a new type, not a reused one

`machine.InOut` bundles one input and one output field in a single
struct, shaped for a transition that mutates a record in place.
`Tool.Run` takes one input value and returns a separate output value:
`Run(ctx, in InOut) (Out, error)`. Reusing `machine.InOut` as the
input type would leave its `Output` field unused and would still need
a distinct `Out` return type. It would also add a `tools` to `machine`
import edge that no requirement in this plan asks for. The `tools`
package defines its own `InOut` and
`Out` types instead. Each wraps one `any` payload, matching the shape
`machine.InOut` uses for a single field, without pulling in
`machine`'s transition-specific `Output` field or its import edge.

`InOut` and `Out` are structs, not named aliases over `any`. A struct
field lets a later phase add a second field, such as a metadata map or
a typed error code, without changing the field name callers already
use or breaking every existing `Tool` implementation's call site. An
alias over `any` would force that same future change onto every
caller's type assertion instead.

### Errors

- `var ErrNilTool` — `Add` returns this for a nil `t`. `Add` checks
  `t == nil` before it calls any method on `t`, so a nil `Tool` never
  panics.
- `var ErrBlankName` — `Add` returns this when `t.Name()` is empty
  after `strings.TrimSpace`. A tool needs a real name to register
  under and to look up later.
- `var ErrDuplicateName` — `Add` returns this for a name already in
  the registry.
- `var ErrUnknownName` — `Get` reports `false` for an unknown name;
  `Run` returns this error when `Get` reports `false`.
- `var ErrScopeDenied` — `RunScoped` returns this when `scope.Allowed`
  returns false for the resolved tool.
- `var ErrToolDeclined` — `RunScoped` returns this when `scope.Approve`
  returns `(false, nil)`. Test with `errors.Is`. Phase 36 addition.

### Execution profile and scope

- `type ExecutionClass string` — the enum. `Validate` enforces the
  set.
- `const ExecutionClassUnclassified ExecutionClass = ""` — the zero
  value; the default for a tool with no `ExecutionProfile`.
- `const ExecutionClassRead ExecutionClass = "read"`
- `const ExecutionClassWrite ExecutionClass = "write"`
- `const ExecutionClassExternal ExecutionClass = "external"`
- `(ExecutionClass) Validate() error` — rejects any value outside the
  four constants above.
- `var ErrInvalidExecutionClass` — `Validate` returns this for a value
  outside the four constants above. Test with `errors.Is`. Gap-fix
  addition, see below.
- `type ExecutionProfile struct { Class ExecutionClass; ResourceKey string; Timeout time.Duration }`
  — execution-risk metadata for one tool: its class, its per-turn
  dedup key, and its timeout.
- `type ProfiledTool interface { ExecutionProfile() ExecutionProfile }`
  — optional; a `Tool` implements it to publish an `ExecutionProfile`.
- `type ResultBudgetTool interface { MaxResultBytes() int }` —
  optional; a `Tool` implements it to bound its output size.
- `type PrivilegedTool interface { Privileged() bool }` — optional; a
  `Tool` implements it to mark itself as needing explicit
  allowlisting.
- `func ExecutionProfileOf(t Tool) ExecutionProfile` — returns
  `t.ExecutionProfile()` when `t` implements `ProfiledTool`; else
  returns the zero `ExecutionProfile`. Never calls `Validate`.
- `func ResultBudgetOf(t Tool) (int, bool)` — returns
  `t.MaxResultBytes()` and true when `t` implements `ResultBudgetTool`;
  else returns `0, false`.
- `func IsPrivileged(t Tool) bool` — returns `t.Privileged()` when `t`
  implements `PrivilegedTool`; else returns false.
- `type ScopeOptions struct { Allowlist []string; ExtraDenylist []string; Approve func(ctx context.Context, call ToolCall) (bool, error); ApprovalThreshold ExecutionClass }`
  — `Approve` and `ApprovalThreshold` are phase 36 additions. Both are
  optional; a `ScopeOptions` with neither set behaves exactly as phase
  31 shipped it, with no approval check.
- `type Scope struct` — built only through `NewScope`; holds the
  resolved allow and deny sets and the approval configuration.
  Unexported fields.
- `func NewScope(opts ScopeOptions) *Scope`
- `(*Scope).Allowed(name string, t Tool) bool` — true when `name`
  passes the denylist, the privileged check, and the allowlist. Phase
  36 leaves this signature and behavior unchanged; approval is a
  separate check `RunScoped` runs after `Allowed` returns true.
- `(*Registry).RunScoped(ctx context.Context, name string, in InOut, scope *Scope) (Out, error)`
  — resolves `name` through `Get`, checks `scope.Allowed` when `scope`
  is non-nil, then calls the tool the same way `Run` does. Returns
  `ErrUnknownName` for an unresolved name and `ErrScopeDenied` for a
  name the scope excludes. A nil `scope` allows every resolved tool,
  matching `Run`'s behavior. Phase 36 keeps this signature unchanged
  and adds one branch: after `scope.Allowed` passes, when
  `scope.Approve` is non-nil and the resolved tool's rank meets or
  exceeds `scope.ApprovalThreshold`'s rank, `RunScoped` calls
  `scope.Approve(ctx, ToolCall{Name: name, In: in, Profile: ExecutionProfileOf(t)})`
  before it calls `Run`. `Approve` returning `(true, nil)` proceeds to
  `Run`. `Approve` returning `(false, nil)` returns `ErrToolDeclined`.
  `Approve` returning a non-nil error returns that error unchanged. A
  nil `scope`, matching phase 31, skips both `Allowed` and this check.
- `type ToolCall struct { Name string; In InOut; Profile ExecutionProfile }`
  — describes one call `RunScoped` is about to make, passed to
  `Approve`. `Name` is the resolved tool's registration name. `In` is
  the caller's input payload, unchanged from the `RunScoped` call.
  `Profile` is `ExecutionProfileOf(t)` for the resolved tool.

`Registry` is safe for concurrent `Add`, `Get`, `Remove`, `Run`, and
`RunScoped`. `RunScoped`'s map lookup is guarded by the same
`sync.RWMutex` as `Run`; the phase 36 approval branch runs
`scope.Approve` and `t.Run` with no lock held, so a caller's `Approve`
may block indefinitely without blocking other registry callers.

## Tests

Test files live in `tools/tools_test/`, an external test package.

- `registry_test.go` — unit, red-green cases for `Add`, `Get`, `Run`,
  and `Remove`.
  - `Add(nil)` returns `ErrNilTool` and does not panic.
  - `Add` rejects a tool whose `Name()` is empty and a tool whose
    `Name()` is whitespace-only, both with `ErrBlankName`.
  - `Add` accepts a new name and rejects a duplicate name with
    `ErrDuplicateName`.
  - `Get` returns the tool and `true` for a registered name; returns
    `nil` and `false` for an unknown name.
  - `Run` calls the tool and returns its result for a registered
    name; returns `ErrUnknownName` for an unknown name.
  - `Remove` on a present name returns `true`; a following `Get`
    returns `false`, and a following `Run` fails with the same error
    as any unknown name.
  - `Remove` on an absent name returns `false` and leaves the
    registry unchanged; a follow-up `Get` for an unrelated registered
    name still succeeds.
- `registry_integration_test.go` — register two tools, resolve each by
  name, and run one. Prove a duplicate `Add` fails. Prove an unknown
  name fails `Run`. Extend with a remove-then-run case: register a
  tool, run it once to prove it works, remove it, then prove `Run`
  fails for that name the same way it fails for a name that was never
  registered.
- `registry_concurrent_test.go` — modeled on
  `heartbeat`'s `monitor_concurrent_test.go` pattern: N goroutines,
  a concrete outcome asserted, run under `go test -race`.
  1. N goroutines each call `Add` with a distinct name concurrently,
     then join. A following loop of `Get` calls must find every one
     of the N names, proving concurrent `Add` calls all land.
  2. One tool is registered up front. N goroutines call `Run` for its
     name concurrently while N other goroutines call `Add` for N
     distinct other names concurrently, then join. Every `Run` call
     must return the registered tool's result with no error, and a
     following `Get` loop must find all N added names, proving reads
     and writes on the map do not corrupt each other under `-race`.
  3. A tool is registered, then N goroutines race one `Remove` call
     for its name against N `Run` calls for the same name. Exactly one
     outcome is valid per `Run` call: either the tool's result (it ran
     before removal) or `ErrUnknownName` (it ran after removal). No
     call may panic or return any other error, proving `Remove` and
     `Run` serialize correctly against each other.
- `registry_bench_test.go` — benchmark `Run` on a registry of one
  hundred tools. Target under one microsecond per call. State the
  allocation budget for one `Run` call.

### Execution profile and scope tests

- `execution_profile_test.go` — red-green cases for
  `ExecutionProfileOf`, `ResultBudgetOf`, and `IsPrivileged`. A tool
  implementing `ProfiledTool` returns its published `ExecutionProfile`
  unchanged. A tool that does not implement `ProfiledTool` returns the
  zero `ExecutionProfile` with `Class == ExecutionClassUnclassified`. A
  tool implementing `ResultBudgetTool` returns its bound and true; a
  tool that does not returns zero and false. `ExecutionClass.Validate`
  rejects a value outside the four constants. One case registers a
  `ProfiledTool` that publishes an out-of-enum `Class` and proves
  `ExecutionProfileOf` returns it unchanged and `RunScoped` still runs
  the tool when the scope otherwise allows it. `ExecutionClass.
  Validate` accepts all four declared constants, including the zero
  value `ExecutionClassUnclassified`.
- `scope_test.go` — red-green cases for `NewScope` and `Scope.Allowed`.
  An empty `ScopeOptions` allows any non-privileged tool. A name in
  `ExtraDenylist` is denied even when `Allowlist` also names it,
  proving denylist wins. A name absent from a non-empty `Allowlist` is
  denied. A privileged tool is denied when its name is absent from
  `Allowlist`, and allowed when present. A combined case: a name in
  both `ExtraDenylist` and `Allowlist`, on a tool that also reports
  `Privileged() == true`, is denied. This proves the denylist,
  privileged, and allowlist rules combine and do not depend on
  evaluation order.
- `registry_run_scoped_test.go` — red-green cases for `RunScoped`. An
  unknown name returns `ErrUnknownName`. A denied name returns
  `ErrScopeDenied` and never calls the tool's `Run`. An allowed name
  runs and returns the tool's result. A nil `Scope` behaves like `Run`.
- `execution_profile_integration_test.go` — register a read-class tool
  and a write-class tool implementing `ProfiledTool` in one `Registry`.
  Build a `Scope` that allowlists only the read tool. Prove `RunScoped`
  runs the read tool and denies the write tool with `ErrScopeDenied`.
  Prove `Registry.Run`, unscoped, still runs both, showing the phase 14
  path is unchanged.
- `registry_run_scoped_concurrent_test.go` — modeled on
  `registry_concurrent_test.go`'s pattern, required for every method
  that touches the tools map. A tool is registered. N goroutines call
  `RunScoped` for its name under an allowing `Scope`, racing against N
  goroutines calling `Remove` for the same name, all under
  `go test -race`. Sub-case one uses an allowing `Scope`. Every
  `RunScoped` call returns either the tool's result or
  `ErrUnknownName` (removed before `Get` resolved it), never
  `ErrScopeDenied`. A second sub-case adds a denying `Scope` racing the
  same `Remove` goroutines and asserts every call returns either
  `ErrScopeDenied` or `ErrUnknownName`. No call may panic. A third
  sub-case races N goroutines calling `RunScoped` for a registered
  name under an allowing `Scope` against N other goroutines calling
  `Add` for N distinct other names, mirroring
  `registry_concurrent_test.go`'s `Run`-versus-`Add` case. Every
  `RunScoped` call must return the tool's result with no error, and a
  following `Get` loop must find all N added names.
- `registry_run_scoped_bench_test.go` — benchmark `RunScoped` on a
  registry of one hundred tools behind a `Scope` with a fifty-name
  allowlist. State the allocation budget next to `registry_bench_test.go`.

### Approval gating tests

- `tool_call_test.go` — red-green cases proving `RunScoped` builds
  `ToolCall` with the resolved tool's name, the caller's `In` value
  unchanged, and `ExecutionProfileOf(t)` as `Profile`, for a tool that
  implements `ProfiledTool` and for one that does not (zero
  `ExecutionProfile`, `Class == ExecutionClassUnclassified`).
- `approval_rank_test.go` — red-green cases for the rank order. A
  tool ranked below `ApprovalThreshold` runs with no `Approve` call,
  proven by a counter the test's `Approve` increments. A tool ranked
  at or above `ApprovalThreshold` triggers exactly one `Approve` call.
  A tool with an out-of-enum `Class` triggers `Approve` at any
  threshold at or below `ExecutionClassExternal`, proving the
  cautious-default rank. A `Scope` built with `Approve` set and
  `ApprovalThreshold` left unset (zero value) triggers `Approve` even
  for an unclassified tool.
- `run_scoped_approval_test.go` — red-green cases for `RunScoped`'s
  three-way outcome. `Approve` returning `(true, nil)` runs the tool
  and returns its result. `Approve` returning `(false, nil)` returns
  `ErrToolDeclined` and never calls the tool's `Run`, proven by a
  counter on a stub `Tool`. `Approve` returning a non-nil error
  returns that exact error, unwrapped, and never calls `Run`. A nil
  `Approve` field skips the check entirely, matching a `Scope` with no
  approval configured. A nil `scope` argument skips the check,
  matching phase 31's existing nil-scope behavior.
- `run_scoped_approval_order_test.go` — proves ordering: a name denied
  by `Allowed` (denylist, absent allowlist, or unapproved privileged
  tool) returns `ErrScopeDenied` and never calls `Approve`, even when
  `Approve` is set and would return true. This proves `Allowed` gates
  before `Approve` runs.
- `run_scoped_approval_integration_test.go` — register a read-class
  tool and a write-class tool implementing `ProfiledTool` in one
  `Registry`. Build a `Scope` with
  `ApprovalThreshold: ExecutionClassWrite` and an `Approve` function
  that denies every call. Prove `RunScoped` runs the read tool with no
  `Approve` call and denies the write tool with `ErrToolDeclined`.
  Prove `Registry.Run`, unscoped, still runs both tools with no
  approval check, showing the phase 14 and phase 31 paths stay
  unchanged.
- `run_scoped_approval_concurrent_test.go` — modeled on
  `registry_run_scoped_concurrent_test.go`'s pattern, required by this
  plan for every method that touches the tools map. A tool requiring
  approval is registered. Sub-case one uses a `Scope` whose `Approve`
  always approves. N goroutines call `RunScoped` under that `Scope`,
  racing against N goroutines calling `Remove` for the same name,
  under `go test -race`. Every call returns either the tool's result
  or `ErrUnknownName`; no call panics. Sub-case two uses a `Scope`
  whose `Approve` always denies. N goroutines call `RunScoped` under
  that `Scope`, racing against N goroutines calling `Remove` for the
  same name, under `go test -race`. Every call returns either
  `ErrToolDeclined` or `ErrUnknownName`; no call panics.

## Verification

`make verify` passes. The coverage floor for `tools` holds at or above
85 percent. The `tools` row in `policy/layers.json` lists its allowed
imports and stays `[]`. `api/tools.txt` lands via `make api-update` and
locks `Tool`, `Registry`, `InOut`, `Out`, `New`, `Add`, `Get`, `Remove`,
`Run`, `ErrNilTool`, `ErrBlankName`, `ErrDuplicateName`,
`ErrUnknownName`, `ExecutionClass`, `ExecutionClassUnclassified`,
`ExecutionClassRead`, `ExecutionClassWrite`, `ExecutionClassExternal`,
`ExecutionProfile`, `ProfiledTool`, `ResultBudgetTool`,
`PrivilegedTool`, `ExecutionProfileOf`, `ResultBudgetOf`,
`IsPrivileged`, `ScopeOptions`, `Scope`, `NewScope`, `RunScoped`,
`ErrScopeDenied`, `ToolCall`, and `ErrToolDeclined`. `ScopeOptions`
gains its `Approve` and `ApprovalThreshold` fields in the same lock.
`go test -race ./tools/...` passes, covering
`registry_concurrent_test.go`, `registry_run_scoped_concurrent_test.go`,
and `run_scoped_approval_concurrent_test.go`.

`semgrep/sdk-standards.yml`'s `sdk.go.no-enum-string-literals` rule
gains `ExecutionClass` in its regex alternation, in the same change as
the code. `python3 scripts/check_semgrep_probes.py` passes with the
extended `viol_enum.go`/`clean_enum.go` probe pair, proving the rule
fires on an `ExecutionClass("x")` violation and stays silent on the
declared constants, alongside the existing `Intent` case.

`docs/packages/tools.md` documents the execution-risk symbols, the
concurrency contract for `RunScoped`, a usage note on `Scope`, and the
phase 36 approval-gating additions (`ToolCall`, `ScopeOptions.Approve`,
`ScopeOptions.ApprovalThreshold`, `ErrToolDeclined`), amended in the
same change as this phase's code.

`policy/layers.json`'s `tools` row stays `[]`; phase 36 adds no
internal import. `ToolCall`, `Approve`, and the approval check use
only `context` and `errors`, the same standard-library-only footprint
phase 14 and phase 31 already use.

### Gap fix: export the invalid-execution-class sentinel

Status: planned, not yet built. `ExecutionClass.Validate` already
returns a sentinel, `errInvalidExecutionClass`
(`tools/execution_profile.go`), but it stays unexported. No caller
outside this package can match it with `errors.Is`, and the existing
test only checks nil-versus-non-nil.

The build: rename `errInvalidExecutionClass` to
`ErrInvalidExecutionClass`, keep the message text
(`"tools: invalid execution class"`) and update the doc comment's
first line and its "not exported" sentence, since the sentinel is now
exported. Update the one call site,
`tools/execution_profile.go:34` (`return ErrInvalidExecutionClass`).
No other line changes.

`make api-update` locks `ErrInvalidExecutionClass` into `api/tools.txt`
in the same change, joining the list above. No `policy/layers.json`
edit.

Test: `tools/tools_test/execution_profile_test.go`'s
`TestExecutionClassValidate` currently checks only `err == nil` versus
`err != nil` per case (see its `wantErr` field). Strengthen the
`true`-want cases to assert
`errors.Is(err, tools.ErrInvalidExecutionClass)` instead of a plain
non-nil check.

### Addition, planned with `agentloop`: `SchemaTool` and `SchemaOf`

Status: planned, not yet built. See `docs/plans/agentloop.md` for the
full contract; this section is the `tools`-side record of the same
change, since `agentloop` is the first caller.

`Tool` publishes no parameter schema and decodes no argument bytes. A
model-driven caller needs both. Add one optional interface, following
the `ProfiledTool`/`ResultBudgetTool`/`PrivilegedTool` precedent:

```go
type SchemaTool interface {
    ParameterSchema() []byte
    DecodeArguments(raw []byte) (InOut, error)
}

func SchemaOf(t Tool) ([]byte, bool)

func (r *Registry) Tools() []Tool
```

`SchemaOf` returns `t.ParameterSchema()` and true when `t` implements
`SchemaTool`; else `nil, false`, matching `ExecutionProfileOf`'s
shape. `DecodeArguments` sits on the tool because only the tool
knows its own input type. `Registry.Tools()` is a new enumeration
method — a name-sorted snapshot slice — added because `agentloop`'s
`Definitions` needs to walk every registered tool and call `SchemaOf`
on each; `Add`, `Get`, `Remove`, `Run`, and `RunScoped` are
unchanged, and `Tool`, `InOut`, and `Out` are unchanged.

`mcp/tools.go` already defines and locks an exported `SchemaTool`
interface (`InputSchema() any`), a different shape than this
`tools.SchemaTool`; phase 70 must rename or remove `mcp.SchemaTool`
when `mcp` adopts `tools.SchemaTool`, so the collision is a tracked,
deliberate decision.

Test: `tools/tools_test/schema_test.go` covers `SchemaOf` on a tool
that implements `SchemaTool`, one that does not, and a typed nil.
`tools/tools_test/registry_test.go` gains a case for `Tools()`
returning a name-sorted, non-nil snapshot, including the empty case.
`Tools()` reads the same map as `Add`, `Get`, `Remove`, `Run`, and
`RunScoped` under the same mutex, so this plan's own "required for
every method that touches the tools map" policy
(`registry_run_scoped_concurrent_test.go`, above) applies to it too:
`registry_run_scoped_concurrent_test.go` gains one race sub-case
racing N goroutines calling `Tools()` against N goroutines calling
`Add`, under `go test -race`, asserting every `Tools()` call returns a
consistent, non-corrupt snapshot and no call panics.

`make api-update` locks `SchemaTool`, `SchemaOf`, and
`Registry.Tools()` into `api/tools.txt` in the same change as
`agentloop`'s own code. No
`policy/layers.json` edit; `tools`'s row stays `[]`.
