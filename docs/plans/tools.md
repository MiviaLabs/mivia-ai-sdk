# Plan: tools

Status: shipped. `Registry.Remove` was added for symmetry with
`room.Room.Admit`/`Remove`, agreed in architecture review.
`ExecutionClass`, `ExecutionProfile`, `ProfiledTool`, `ResultBudgetTool`,
`PrivilegedTool`, `Scope`, `ScopeOptions`, `NewScope`,
`ExecutionProfileOf`, `ResultBudgetOf`, `IsPrivileged`, `RunScoped`, and
`ErrScopeDenied` were added by phase 31, folded into this plan the way
phase 14 was folded in when it shipped. See
`docs/plans/agents/phase31_tools_capabilities.md` for the design
history.

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

Phase 16 runs the tool registry as a flow step. A panel step runs in
its own goroutine, so more than one goroutine can call `Add`, `Get`,
`Remove`, `Run`, and `RunScoped` on the same `Registry` at once, once
that wiring lands. This plan states the concurrency contract now,
ahead of that caller, matching how `room.Room`, `events.Bus`, and
`heartbeat.Monitor` each state their contract in their own plan before
every caller existed.

A tool that does not implement `ProfiledTool` is unclassified.
`ExecutionProfileOf` reports `ExecutionClassUnclassified`, the zero
value, for such a tool. Every tool shipped before phase 31 stays valid
with no change.

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

### Timeout, ResourceKey, and MaxResultBytes: published, not enforced

This phase publishes `ExecutionProfile.Timeout`, `ExecutionProfile.
ResourceKey`, and `ResultBudgetTool.MaxResultBytes` as metadata only.
No function in this phase reads `Timeout` to set a context deadline.
No function reads `ResourceKey` to dedup a call. Neither `Run` nor
`RunScoped` reads `ResultBudgetOf` to truncate or reject an oversized
result. `RunScoped` runs the tool the same way `Run` does; it checks
only `Scope.Allowed`. Enforcement is deferred to the future
agent-binding caller named in the roadmap's "Precedent for shipping
with no caller yet" section, the same caller that will wire a
`Registry` into `agent.Run`.

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

### Execution profile and scope (phase 31)

- `type ExecutionClass string` — the enum. `Validate` enforces the
  set.
- `const ExecutionClassUnclassified ExecutionClass = ""` — the zero
  value; the default for a tool with no `ExecutionProfile`.
- `const ExecutionClassRead ExecutionClass = "read"`
- `const ExecutionClassWrite ExecutionClass = "write"`
- `const ExecutionClassExternal ExecutionClass = "external"`
- `(ExecutionClass) Validate() error` — rejects any value outside the
  four constants above.
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
- `type ScopeOptions struct { Allowlist []string; ExtraDenylist []string }`
- `type Scope struct` — built only through `NewScope`; holds the
  resolved allow and deny sets. Unexported fields.
- `func NewScope(opts ScopeOptions) *Scope`
- `(*Scope).Allowed(name string, t Tool) bool` — true when `name`
  passes the denylist, the privileged check, and the allowlist.
- `(*Registry).RunScoped(ctx context.Context, name string, in InOut, scope *Scope) (Out, error)`
  — resolves `name` through `Get`, checks `scope.Allowed` when `scope`
  is non-nil, then calls the tool the same way `Run` does. Returns
  `ErrUnknownName` for an unresolved name and `ErrScopeDenied` for a
  name the scope excludes. A nil `scope` allows every resolved tool,
  matching `Run`'s behavior.

`Registry` is safe for concurrent `Add`, `Get`, `Remove`, `Run`, and
`RunScoped`; the same `sync.RWMutex` that guards `Run` guards
`RunScoped`.

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

### Execution profile and scope tests (phase 31)

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
`IsPrivileged`, `ScopeOptions`, `Scope`, `NewScope`, `RunScoped`, and
`ErrScopeDenied`. `go test -race ./tools/...` passes, covering
`registry_concurrent_test.go` and
`registry_run_scoped_concurrent_test.go`.

`semgrep/sdk-standards.yml`'s `sdk.go.no-enum-string-literals` rule
gains `ExecutionClass` in its regex alternation, in the same change as
the code. `python3 scripts/check_semgrep_probes.py` passes with the
extended `viol_enum.go`/`clean_enum.go` probe pair, proving the rule
fires on an `ExecutionClass("x")` violation and stays silent on the
declared constants, alongside the existing `Intent` case.

`docs/packages/tools.md` is amended in the same change to add the
phase 31 symbols, the concurrency contract for `RunScoped`, and a usage
note on `Scope`.
