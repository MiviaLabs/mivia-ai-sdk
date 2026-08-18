# Package reference: tools

The tools package resolves named actions. A `Registry` holds `Tool`
values by name and runs one by name. A step names a tool by string; an
unknown name fails. The exported surface below mirrors
`api/tools.txt`.

## Types

- `Tool` — a named action a `Registry` can resolve and run. `Name`
  returns the registration key. `Run` performs the action.
- `Registry` — tools keyed by name. Safe for concurrent use. The zero
  value is not usable; create one with `New`.
- `InOut` — a tool's input payload. `Value` holds the caller's typed
  argument.
- `Out` — a tool's output payload. `Value` holds the tool's typed
  result.

- `ExecutionClass` — a string enum for a tool's execution risk:
  `ExecutionClassUnclassified` (the zero value), `ExecutionClassRead`,
  `ExecutionClassWrite`, `ExecutionClassExternal`. `Validate` rejects
  any other value.
- `ExecutionProfile` — execution-risk metadata for one tool call:
  `Class`, `ResourceKey` (a per-turn dedup key), and `Timeout`. This
  package publishes these fields; it does not enforce them. See
  "Published, not enforced" below.
- `ProfiledTool` — optional interface. A `Tool` implements
  `ExecutionProfile() ExecutionProfile` to publish its profile.
- `ResultBudgetTool` — optional interface. A `Tool` implements
  `MaxResultBytes() int` to publish its output-size bound. This
  package does not read the bound to truncate or reject a result.
- `PrivilegedTool` — optional interface. A `Tool` implements
  `Privileged() bool` to mark itself as needing explicit allowlisting.
- `ScopeOptions` — `Allowlist` and `ExtraDenylist`, the inputs to
  `NewScope`.
- `Scope` — a narrowing filter over tool names. The zero value is not
  usable; create one with `NewScope`.

## Functions and methods

- `New()` — creates an empty `Registry`.
- `Registry.Add(t)` — registers `t` under `t.Name()`.
- `Registry.Get(name)` — resolves `name` to a `Tool`. Returns false
  for an unknown name.
- `Registry.Remove(name)` — drops `name` from the registry. Returns
  whether it was present.
- `Registry.Run(ctx, name, in)` — resolves `name` through `Get` and
  runs the tool.
- `Registry.RunScoped(ctx, name, in, scope)` — resolves `name` through
  `Get`, checks `scope.Allowed` when `scope` is non-nil, then runs the
  tool. A nil `scope` behaves like `Run`.
- `ExecutionProfileOf(t)` — returns `t`'s published `ExecutionProfile`,
  or the zero value when `t` does not implement `ProfiledTool`.
- `ResultBudgetOf(t)` — returns `t`'s `MaxResultBytes()` and true, or
  `0, false` when `t` does not implement `ResultBudgetTool`.
- `IsPrivileged(t)` — returns `t.Privileged()`, or false when `t` does
  not implement `PrivilegedTool`.
- `NewScope(opts)` — builds a `Scope` from `ScopeOptions`.
- `Scope.Allowed(name, t)` — true when `name` passes the denylist, the
  privileged check, and the allowlist.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrNilTool` — `Add` got a nil `Tool`.
- `ErrBlankName` — `Add` got a tool whose `Name()` is blank, after
  `strings.TrimSpace`.
- `ErrDuplicateName` — `Add` got a name already registered.
- `ErrUnknownName` — `Run` or `RunScoped` got a name `Get` reports as
  absent.
- `ErrScopeDenied` — `RunScoped` got a name `scope.Allowed` rejects.

## Invariants

- `Add` rejects a nil `t` with `ErrNilTool`, before it calls any
  method on `t`.
- `Add` rejects a blank name, after trim, with `ErrBlankName`.
- `Add` rejects a duplicate name with `ErrDuplicateName`. It never
  overwrites an existing registration.
- `Get` returns false for an unknown name. It never panics.
- `Remove` returns whether `name` was present. Removing an absent
  name is not a fault; it changes nothing and returns false.
- `Run` returns `ErrUnknownName` when `Get` reports the name absent.
  A name removed after registration fails `Run` the same way an
  unknown name always did.
- `Registry` is safe for concurrent `Add`, `Get`, `Remove`, `Run`, and
  `RunScoped`; a `sync.RWMutex` guards the map.
- A tool that does not implement `ProfiledTool` is unclassified.
  `ExecutionProfileOf` reports `ExecutionClassUnclassified`, the zero
  value.
- `ExecutionProfileOf` and `RunScoped` never call `ExecutionClass.
  Validate`. `Scope.Allowed` never reads `Class`; an out-of-enum
  `Class` never blocks `RunScoped`.
- `Scope` narrows only. `ExtraDenylist` always wins over `Allowlist`.
  A privileged tool is denied unless `Allowlist` names it. No
  operation on a built `Scope` re-adds a name `ExtraDenylist` removed.
- `RunScoped` returns `ErrUnknownName` for an unresolved name and
  `ErrScopeDenied` for a name the scope excludes. A nil `scope` allows
  every resolved tool, matching `Run`.
- `RunScoped` never reads `Timeout`, `ResourceKey`, or
  `MaxResultBytes`. It checks only `scope.Allowed`, then runs the tool
  the way `Run` does. See "Published, not enforced" below.

## Why this shape

`Tool` is an interface because tools are many and replaceable: an
agent's step names a tool by string, and the registry resolves it at
run time without the caller knowing the concrete type. `Registry`
mirrors `room.Room`'s membership shape: `Add` and `Remove` pair the
same way `Room.Admit` and `Room.Remove` do, so a tool can be
withdrawn, not only added.

`ExecutionProfile` uses its own name, not `discovery.Card`'s
"capability" word: a discovery card lists what an agent can do, while
an `ExecutionProfile` states one tool call's execution risk. `Scope`
narrows only, never widens, so a caller cannot accidentally grant a
tool back once denied. `RunScoped` sits beside `Run`, not inside it, so
the phase 14 `Run` signature and behavior stay locked while scoped
calls opt in explicitly.

### Published, not enforced

`Timeout`, `ResourceKey`, and `MaxResultBytes` are metadata this
package publishes and never enforces. No function sets a context
deadline from `Timeout`. No function dedups a call by `ResourceKey`.
Neither `Run` nor `RunScoped` truncates or rejects a result using
`ResultBudgetOf`. `RunScoped` runs the tool the same way `Run` does; it
checks only `Scope.Allowed`. A future agent-binding caller enforces
these fields when it wires a `Registry` into `agent.Run`.

## Cross-references

- [room.md](room.md) — `Room.Admit`/`Room.Remove` is the precedent
  for `Registry.Add`/`Registry.Remove`'s add-and-remove symmetry.
- `tools` imports no other package in this module.
- `docs/plans/agents/phase32_context_budget.md` — the future
  `contextbudget.Limits` type bounds a whole model call's context.
  `ResultBudgetTool.MaxResultBytes` bounds one tool call's output. The
  two types do not import each other.

## Usage

```go
type echoTool struct{}

func (echoTool) Name() string { return "echo" }

func (echoTool) Run(_ context.Context, in tools.InOut) (tools.Out, error) {
    return tools.Out{Value: in.Value}, nil
}

reg := tools.New()
_ = reg.Add(echoTool{})
out, err := reg.Run(context.Background(), "echo", tools.InOut{Value: "hi"})
// out.Value == "hi", err == nil
_ = reg.Remove("echo")
_, err = reg.Run(context.Background(), "echo", tools.InOut{Value: "hi"})
// err is tools.ErrUnknownName
```

`RunScoped` narrows which registered tools a run may invoke:

```go
type writeTool struct{}

func (writeTool) Name() string { return "delete" }
func (writeTool) Run(_ context.Context, in tools.InOut) (tools.Out, error) {
    return tools.Out{}, nil
}
func (writeTool) Privileged() bool { return true }

reg := tools.New()
_ = reg.Add(echoTool{})
_ = reg.Add(writeTool{})

scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"echo"}})
_, err := reg.RunScoped(context.Background(), "echo", tools.InOut{Value: "hi"}, scope)
// err == nil
_, err = reg.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
// err is tools.ErrScopeDenied: "delete" is privileged and not allowlisted
```
