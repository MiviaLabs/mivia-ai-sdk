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
`flow.Monitor` each state their contract in their own plan before
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
See this plan's "Run timeout backstop" section, and the section of
the same name in docs/packages/tools.md, for the rules.

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
- `New() *Registry` — builds an empty registry. Takes no argument.
  Every run is bounded by `DefaultRunTimeout` unless the tool's
  profile declares its own `Timeout`.
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

Status: shipped. Commit 2b2d40b. `ExecutionClass.Validate` already
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

Status: shipped. Commit 16a7478. See `docs/plans/agentloop.md` for the
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

### Run timeout backstop

Status: shipped. Every `Run` and
`RunScoped` dispatch runs the tool under a deadline. This section
describes the surviving behavior after that removal.

`DefaultRunTimeout` is ten minutes. It is the built-in bound. No
caller opts in. It applies to every tool that declares no profile
`Timeout`.

`TimeoutNone` is minus one. It is the canonical "never cap" value.
The resolver treats any negative duration the same way.

`ErrRunTimeout` is the expiry sentinel. An expiry returns an error
wrapping it. The message carries the tool name and the effective
bound. Callers match with `errors.Is`, never against the naked
sentinel.

The bound resolves in one pass over the tool alone:

1. A positive `ExecutionProfile.Timeout` binds verbatim.
2. A negative `ExecutionProfile.Timeout` never caps that tool.
3. A zero or absent `ExecutionProfile.Timeout` resolves to
   `DefaultRunTimeout`.

One escape hatch survives. A tool exempts itself with a negative
profile `Timeout`. No registry-wide exemption exists. A tool that
declares no profile always runs under a deadline.

The budget starts when the tool's `Run` starts. It never covers
`Scope.Allowed` or `Approve`. A tool that finishes first keeps its
exact result, including a cancellation-shaped error it produced on
its own. A parent context already done yields the parent cause
instead of `ErrRunTimeout`. A panicking tool yields an error carrying
its name and panic value.

### Removal: the functional-option constructor

Status: shipped. `tools` was the one package in this
module that used the functional-option pattern.
`docs/plans/workspace.md` records the rule, in the bullet on the
per-call read override: this module uses no functional-option
pattern. Grep that file for `functional-option` to find it; a sibling
change in this batch moves its line number. This change restores the
rule. It deletes the registry-wide run-timeout knob with it. This
plan's API section already declares `New() *Registry`; the code
drifted from it.

The build is four edits.

1. `tools/registry.go` line 61. `func New(opts ...Option) *Registry`
   becomes `func New() *Registry`. Delete the option loop. Rewrite the
   doc comment. `New` creates an empty `Registry`. Every run is
   bounded by `DefaultRunTimeout` unless the tool's profile declares
   its own `Timeout`.
2. `tools/registry.go` lines 52 to 55. Delete the `defaultRunTimeout`
   field, its doc comment, and the sentence "Immutable after New
   applies its options left to right". The struct keeps `mu` and
   `tools`. Drop the then-unused `time` import from `registry.go`.
3. `tools/registry_timeout.go` lines 24 to 35. Delete `type Option
   func(*Registry)` and `func WithDefaultRunTimeout(d time.Duration)
   Option` with their doc comments. Amend
   `DefaultRunTimeout`'s doc comment at line 11 to drop the
   "no WithDefaultRunTimeout option applies" clause.
4. `tools/registry_timeout.go` lines 37 to 59 and line 82.
   `effectiveRunTimeout(t Tool, configured time.Duration)` becomes
   `effectiveRunTimeout(t Tool)`. Delete the `switch` over
   `configured` and return `DefaultRunTimeout` after the profile
   check. Amend its doc comment to drop the configured default.
   `runBounded` calls `effectiveRunTimeout(t)`.

The field goes, and `effectiveRunTimeout` loses its parameter. A
field that no code writes is dead state, and keeping it would leave
two branches inside `effectiveRunTimeout` that no caller can reach.

Observable behavior does not change. `configured` was already zero at
every reachable call site, because no caller outside `tools` ever
passed an option. A tool declaring a positive profile `Timeout` still
binds verbatim. A tool declaring a negative one still never caps. An
undeclared tool still resolves to `DefaultRunTimeout`.

`DefaultRunTimeout`, `TimeoutNone`, `ErrRunTimeout`, and
`ExecutionProfile.Timeout` all stay. The backstop stays. Only the
registry-wide knob goes.

`New` stays source-compatible. Every call site in the module already
calls `tools.New()` with no argument.

#### Invariant 4 is removed, not changed

Invariant 4 said `WithDefaultRunTimeout(TimeoutNone)` restores
unbounded runs for undeclared tools. That capability is deleted. A
tool that declares no profile `Timeout` can no longer be exempted at
all. Only a tool that declares a negative profile `Timeout` runs
unbounded. The plan states this, and
`docs/packages/tools.md` states it too. No test may claim invariant 4
survives.

#### Test moves

Ten call sites build a registry through the deleted option. Nine live
in `tools/tools_test/registry_run_timeout_test.go`. One lives in
`tools/run_timeout_internal_test.go` at line 103. Every surviving test
moves its bound onto the tool's `ExecutionProfile.Timeout`.

Two probe tool types are new in
`tools/tools_test/registry_run_timeout_test.go`. Both report their
context deadline through `Out.Value`, using one report struct:

```go
// deadlineReport is a probe tool's Out.Value. bounded records whether
// the context carried a deadline. remaining records
// time.Until(deadline); it is zero when bounded is false.
type deadlineReport struct {
	bounded   bool
	remaining time.Duration
}
```

One probe publishes a `ProfiledTool` timeout. The other implements
`Tool` only, so it can never declare a bound:

```go
// deadlineProbe reports its context deadline and publishes one
// declared Timeout.
type deadlineProbe struct {
	name    string
	timeout time.Duration
}

// bareDeadlineProbe reports its context deadline and declares
// nothing. It implements Tool only.
type bareDeadlineProbe struct {
	name string
}
```

Both probes carry a per-instance `name` field, the way `gateTool` and
`napTool` already do. `Registry.Add` returns `ErrDuplicateName` on a
name collision, and one test registers two profiled probes in one
registry. Each `Run` returns `Out{Value: deadlineReport{...}}` built
from `ctx.Deadline()`. `deadlineProbe.ExecutionProfile` returns
`ExecutionProfile{Timeout: p.timeout}`. `bareDeadlineProbe` must not
gain an `ExecutionProfile` method, or it stops testing the undeclared
path.

A context deadline is observable through the `Tool` contract. A bound
is therefore provable with no wall-clock wait.

`tools/tools_test/registry_run_timeout_test.go`:

- Line 181, `TestRunConfiguredDefaultExpires`. It proves that a run
  with no profile declaration is still bounded. It also proves the
  expiry carries the tool name and the bound. The first half dies with
  the knob. Rename to `TestRunScopedDeclaredTimeoutExpires`. Wrap the
  gate tool in `profiledGateTool` with
  `timeout: 20 * time.Millisecond`. Nothing is configured any more, so
  rename the tool-name literal from `configured-slow` to `scoped-slow`
  at all three sites: the `gateTool` literal at line 183, the run
  argument at line 189, and the message assertion at line 194. Call
  `RunScoped` under an allowing scope from
  `tools.NewScope(tools.ScopeOptions{})`. Keep both assertions
  otherwise unchanged. The renamed test pins expiry through
  `RunScoped`, which no test covers today.
- Line 222, `TestProfileLongerThanConfiguredFiresLonger`. Delete it.
  It proves a declared 120 ms bound governs a 30 ms configured
  registry, so a silent minimum is ruled out. The configured half of
  that pair dies with the knob. The built-in ten-minute fallback is
  larger than any bound a unit test can wait out. The invariant
  therefore cannot survive at this level. The new
  `declared-longer-than-default` row in `TestEffectiveRunTimeout`
  carries it instead, at zero wall-clock cost. Renaming this test
  would only duplicate `TestRunDeclaredTimeoutExpires` at line 157,
  which already kills the mutation that ignores the declared value.
- Line 243, `TestNegativeProfileExemptsUnderAggressiveConfigured`. It
  proves a negative profile `Timeout` exempts a tool under a tight
  registry default. The exemption survives; the tight default does
  not. A 100 ms nap under the ten-minute fallback would pass either
  way. Rename to `TestNegativeProfileExemptsFromBackstop`. Register
  the profiled probe with `timeout: tools.TimeoutNone` and assert
  `bounded` is false. Register a second profiled probe with
  `timeout: 50 * time.Millisecond` and assert `bounded` is true. The
  second case is the positive control. The renamed test pins the
  surviving per-tool exemption.
- Line 264, `TestWithDefaultRunTimeoutNoneExemptsUndeclared`. Its
  whole subject is the deleted symbol and the deleted invariant 4.
  Delete it. Add `TestUndeclaredToolAlwaysBounded` in its place. That
  test registers the no-profile probe. It asserts `bounded` is true
  and the run returns no error. It also asserts `remaining` is greater
  than `tools.DefaultRunTimeout - time.Minute` and at most
  `tools.DefaultRunTimeout`. The range pins the resolved bound to
  `DefaultRunTimeout` itself, so any other fallback value fails the
  test. Both sides of the range are symbolic, so raising the constant
  moves both and the test still passes; the test pins the resolver
  against the constant, never the constant's value. Removing the bound
  fails the test, and so does a resolver returning a hard-coded
  minute or `DefaultRunTimeout * 2`. A bare "a deadline exists"
  assertion would pass under any bound and pin nothing.
- Line 281, `TestParentCancelMidRunBeatsDeadline`. Keep the name and
  every assertion. Wrap the gate tool in `profiledGateTool` with
  `timeout: 1 * time.Hour`.
- Line 316, `TestToolOwnDeadlineErrorStaysUntouched`. Keep the name
  and every assertion. Give `selfDeadlineTool` an `ExecutionProfile`
  method returning `Timeout: 200 * time.Millisecond`.
- Line 333, `TestSlowApproveDoesNotConsumeBudget`. Keep the name and
  every assertion. Set `timeout: 20 * time.Millisecond` on the
  existing `profiledNapTool` literal, which declares no timeout
  today.
- Line 371, `TestUnknownNameUnchangedUnderBackstop`. Keep the name and
  every assertion. Build the registry with `tools.New()`. The test
  never runs a tool, so no bound is needed.
- Line 387, `TestConcurrentBlockingCallsEachExpire`. Keep the name and
  every assertion. Register each blocker as a `profiledGateTool` with
  `timeout: 25 * time.Millisecond`.

`tools/run_timeout_internal_test.go`:

- Line 103, `TestRunBoundedLateProducerBufferedSend`. Keep the name
  and every assertion. Build the registry with `New()`. Give
  `lateProducerTool` an `ExecutionProfile` method returning
  `Timeout: 15 * time.Millisecond`.
- Line 32, `TestEffectiveRunTimeout`. Keep the name. Drop the
  `configured` column from the table and from the call. Two rows name
  the deleted parameter and go with it. Two more rows differ only in
  `configured` and collapse into one. Five rows remain, in the field
  order name, declared, hasProfile, want:
  - `{"undeclared-defaults", 0, false, DefaultRunTimeout}`
  - `{"declared-zero-falls-through", 0, true, DefaultRunTimeout}`
  - `{"declared-positive-verbatim", 80 * time.Millisecond, true, 80 * time.Millisecond}`
  - `{"declared-longer-than-default", 20 * time.Minute, true, 20 * time.Minute}`
  - `{"declared-negative-none", TimeoutNone, true, 0}`

  The `declared-zero-falls-through` row is new. It exercises the
  `declared != 0` guard through a `ProfiledTool`, which today's table
  never reaches. The `declared-longer-than-default` row is new and
  load-bearing. It is the only surviving proof that a declared bound
  binds verbatim with no silent clamp. Without it a
  `min(declared, DefaultRunTimeout)` mutation survives the whole
  suite.

#### Test doc comment rewrites

Five test doc comments describe the deleted knob. Each is a
behavioral claim, so each must be rewritten in the same change.

- `tools/run_timeout_internal_test.go` lines 27 to 31,
  `TestEffectiveRunTimeout`. Three clauses name the configured value.
  Replace the whole comment. The table drives the resolution
  precedence from the tool alone. A positive declared `Timeout` binds
  verbatim, longer or shorter than `DefaultRunTimeout`. A negative one
  never caps. An undeclared or zero `Timeout` falls through to
  `DefaultRunTimeout`.
- `tools/tools_test/registry_run_timeout_test.go` lines 178 to 180.
  The comment names `WithDefaultRunTimeout` and the configured-default
  path. Replace it. The renamed test pins the expiry identity through
  `RunScoped`: a declared bound expires as `ErrRunTimeout` wrapped
  with the tool name and the bound.
- `tools/tools_test/registry_run_timeout_test.go` lines 219 to 221.
  The comment describes a 30 ms configured registry. It goes with the
  deleted test.
- `tools/tools_test/registry_run_timeout_test.go` lines 240 to 242.
  The comment says "under a tight registry default". Replace it. The
  renamed test pins the surviving exemption: a negative profile
  `Timeout` leaves the tool's context with no deadline, while a
  positive one sets one.
- `tools/tools_test/registry_run_timeout_test.go` lines 312 to 315.
  The comment says "under a much longer bound". The bound is now
  declared on the tool, so change that phrase to "under a much longer
  declared bound". The rest of the comment stays true.

#### Gate expectations for the test moves

`scripts/check_test_tampering.py` fires TT01 four times.
The change makes two renames and two deletions.
`check_moved_or_dropped` matches a removed test to an added one by
normalized body hash. Both renames change their bodies, so neither
finds a match. Neither deletion has a body-identical addition. The
four findings name
`TestRunConfiguredDefaultExpires`,
`TestProfileLongerThanConfiguredFiresLonger`,
`TestNegativeProfileExemptsUnderAggressiveConfigured`, and
`TestWithDefaultRunTimeoutNoneExemptsUndeclared`.

TT04 may flag the assertion count. The change adds
`TestUndeclaredToolAlwaysBounded`, a positive control, and two table
rows, so a net increase is expected. TT07 should stay silent, because
no numeric bound increases.

The justification the builder must present:

`TestWithDefaultRunTimeoutNoneExemptsUndeclared` tests
`WithDefaultRunTimeout`, the symbol this change deletes. The
registry-wide exemption it pins no longer exists.
`TestUndeclaredToolAlwaysBounded` replaces it and pins the opposite
rule: the resolved fallback bound is `DefaultRunTimeout`.
`TestProfileLongerThanConfiguredFiresLonger` pins a comparison against
the configured default, which is deleted; the new
`declared-longer-than-default` table row carries that invariant
instead. This change makes two renames and two deletions. Both
renames change their bodies and their assertions, because the bound
moves from the registry to the tool's profile.
`TestRunScopedDeclaredTimeoutExpires` now pins expiry identity through
`RunScoped`. `TestNegativeProfileExemptsFromBackstop` now pins the
per-tool exemption through the context deadline. No invariant is
dropped without a named replacement.

This plan does not authorize an override trailer. The builder stops,
presents the four TT01 findings and this justification, and waits. The
orchestrator decides. One `Allow-Test-Change: TT01 <reason>` trailer
waives all four, because `resolve_overrides` keys by finding ID. The
reason needs at least six significant words.

#### Documentation edits

`docs/packages/tools.md` changes at six sites. Grep with
`WithDefaultRunTimeout` and with `Option` to confirm the set.

- Lines 28 to 30. Delete the `Option` bullet.
- Lines 58 to 61. `New(opts ...Option)` becomes `New()`. It creates an
  empty `Registry`. Every run is bounded by `DefaultRunTimeout` unless
  the tool declares its own profile `Timeout`.
- Lines 62 to 64. Delete the `WithDefaultRunTimeout(d)` bullet. Line
  65 starts the unrelated `Registry.Add(t)` bullet and stays.
- Lines 203 to 207. The bound resolves from the tool alone. A
  positive profile `Timeout` binds verbatim. A negative one never
  caps. Otherwise `DefaultRunTimeout` applies.
- Line 212. Delete the `WithDefaultRunTimeout(d)` table row. The table
  keeps its header and the `ExecutionProfile.Timeout` row.
- Lines 214 to 218. Replace the whole paragraph. Line 215 says the
  escape hatches point both ways, and lines 217 to 218 claim
  `WithDefaultRunTimeout(tools.TimeoutNone)` restores unbounded runs
  for one registry. Both claims become false. The replacement says
  this. Any negative means never cap, and `TimeoutNone` names the
  canonical constant. One escape hatch exists: a negative profile
  `Timeout` exempts one tool. No registry-wide exemption exists. An
  undeclared tool always runs under `DefaultRunTimeout`. Line 219 is
  blank and stays.

#### Verification for the removal

`make api-update` rewrites `api/tools.txt` with three line changes.
Line 18 becomes `func New() (*Registry)`. Line 22,
`func WithDefaultRunTimeout(d time.Duration) (Option)`, is deleted.
Line 32, `type Option func(*Registry)`, is deleted. Lines 2, 7, and
73 are untouched: `DefaultRunTimeout`, `TimeoutNone`, and
`ErrRunTimeout` all stay locked.

`policy/layers.json` does not change. The `tools` row stays `[]`.

`make verify` does not pass on the working tree. `Makefile` line 24
runs `scripts/check_test_tampering.py` with no `--message-file`, so no
trailer resolves and the four TT01 findings exit non-zero. Only
`.githooks/commit-msg` passes the message. The real sequence is:

1. The builder makes the change and runs `make verify`.
2. TT01 fires four times. The builder stops.
3. The builder presents the four findings and the justification above.
4. The orchestrator decides, and authorizes one trailer or rejects it.
5. `make verify` is green only once the commit carries that trailer.

`python3 scripts/check_plan.py`, `python3 scripts/check_api.py`,
`python3 scripts/check_docs.py`, and `python3 scripts/check_prose.py`
each pass on their own, with no trailer.
`go test -race ./tools/...` passes.

The `tools` package sits at 100 percent coverage today. The change
deletes covered production code and two tests, and adds one test, one
positive control, and two table rows. The 85 percent floor is not at
risk. The builder confirms the number in the `make verify` coverage
block.

No conformance vector changes. The `tools` package owns none.

### Correction: SchemaOf fails closed on a nil schema

Status: shipped. One commit together with `docs/plans/spool.md`'s
"Change: collapse the wrapper variants to one". See that section's
"Landing order" for the two-commit split. The fail-closed rule is a
precondition for the `spool` collapse in the same commit.

`SchemaOf` (`tools/schema.go:18-23`) returns `st.ParameterSchema()`
and true whenever the type assertion succeeds. A `SchemaTool` whose
`ParameterSchema()` returns nil reports `nil, true`. That reads as a
published schema. It is not one.

Change `SchemaOf` to fail closed. When the assertion succeeds but
`ParameterSchema()` returns nil, return `nil, false`. State the rule
in the function body, not in the comment alone. Update the doc
comment: a true second result promises non-nil schema bytes.

Call-site audit, grepped over `SchemaOf`, `SchemaTool`, and
`ParameterSchema` across `*.go`:

- `agentloop/definitions.go:22-25` skips a tool when the bool is
  false and records the name. A nil-schema `SchemaTool` now lands in
  `skipped` instead of reaching `compileSchemas` as a nil document.
  This is the intended fix.
- `agentrun/wire.go:118-126` gates an unguarded `t.(tools.SchemaTool)`
  assertion on the bool. A true bool still implies the interface. The
  assertion stays safe. A nil-schema tool keeps its plain payload.
- `spool/tool.go` and `runconfig/steptool.go:47` discard the bool and
  forward the bytes. Nil forwards as nil either way. Unchanged.
- `agentloop/toolcall.go:476` asserts the interface directly and
  never calls `SchemaOf`. Unchanged.
- Tests at `mcp/schema_tool_test.go:26`, `mcp/client_test.go:94`, and
  `spool/spool_test/readtool_test.go:165` use tools with real
  schemas. Unchanged.
- `runconfig/steptool_internal_test.go:394` compares wrapper bytes
  against `tools.SchemaOf(inner)` bytes. Byte parity holds under the
  fail-closed rule. Unchanged.

Tests:

- Flip the "typed nil" subtest in `tools/tools_test/schema_test.go`.
  It pins the old fail-open result `nil, true`. It must expect
  `nil, false`, and its comment states the fail-closed rationale.
  This is a mandated expectation change, not a weakened test. The
  commit body names it. `scripts/check_test_tampering.py` reports one
  `TT01` finding for the rewritten function body. The orchestrator
  authorizes the trailer, following the "Removal" section's sequence.
- Extend `TestSchemaOf` with one subtest: a non-nil receiver whose
  `ParameterSchema()` returns nil. `SchemaOf` returns `nil, false`.
  Keep the existing `(schema, true)` case and the plain-Tool
  `(nil, false)` case.
- Add one case in `agentloop/agentloop_test/definitions_test.go`, the
  package that consumes the bool. Name it
  `TestDefinitionsSkipsNilSchemaSchemaTool`. Its tool implements
  `tools.SchemaTool` and returns nil from `ParameterSchema()`. The
  test asserts the tool lands in `Definitions`' skip list and appears
  in neither the offered definitions nor the error. Put the case in
  `agentloop`, not in `tools`.

API and lock: no exported symbol changes. `SchemaOf`'s signature is
unchanged, so `make api-update` produces no `api/tools.txt` diff.
`policy/layers.json` needs no edit. The `tools` row stays `[]`.

Docs: rewrite the `SchemaOf` bullet at `docs/packages/tools.md:81`
with the fail-closed rule. Extend its neighbor at lines 164-166: a
tool that publishes no schema bytes is unschema'd, whether or not it
implements `SchemaTool`. Extend the two skip clauses at
`docs/packages/tools.md:50-51` and `docs/architecture.md:404`. Each
says `Definitions` skips a tool that does not implement
`SchemaTool`. Each gains "or whose `ParameterSchema()` returns nil".
Check the `ErrArgumentDecode` entry at
`docs/packages/agentrun.md:106-110`; add one clause for the
nil-schema pass-through.

## Addendum: Error sentinel sweep

Status: shipped.

This addendum classifies every `tools` sentinel error as CONFIG or
RUNTIME. CONFIG means every return site checks caller-supplied
construction input. RUNTIME means at least one return site reacts to
live registry or scope state.

Four sentinels were CONFIG. They merged into one new sentinel,
`ErrInvalidOptions`, defined in `registry.go`. Each old return site now
wraps it with a field name and a one-line rule, in the form
`fmt.Errorf("%w: %s", ErrInvalidOptions, "<Field>: <rule>")`.

| Sentinel | Classification | Disposition |
| --- | --- | --- |
| `ErrNilTool` | CONFIG | Deleted. Merged into `ErrInvalidOptions`, field `Tool`. |
| `ErrBlankName` | CONFIG | Deleted. Merged into `ErrInvalidOptions`, field `Tool.Name`. |
| `ErrInvalidExecutionClass` | CONFIG | Deleted. Merged into `ErrInvalidOptions`, field `ExecutionClass`. |
| `ErrUnknownApprovalThreshold` | CONFIG | Deleted. Merged into `ErrInvalidOptions`, field `ApprovalThreshold`. |
| `ErrDuplicateName` | RUNTIME | Unchanged. `Add` reacts to a name already in the live map. |
| `ErrUnknownName` | RUNTIME | Unchanged. `Run` and `RunScoped` react to a name absent from the live map. |
| `ErrScopeDenied` | RUNTIME | Unchanged. `RunScoped` reacts to a live `Scope.Allowed` result. |
| `ErrToolDeclined` | RUNTIME | Unchanged. `RunScoped` reacts to a live approval callback result. |
| `ErrRunTimeout` | RUNTIME | Unchanged. `runBounded` reacts to a live deadline expiry. |

Tests in `tools/tools_test/` that asserted the four deleted sentinels
now assert `errors.Is(err, tools.ErrInvalidOptions)` plus a
`strings.Contains` check on the field name. No test function was
deleted.

`tools/doc.go` still names `ErrNilTool` and `ErrBlankName` in its
package map comment. That file is outside this sweep's owned file
list and needs a follow-up edit to name `ErrInvalidOptions` instead.
