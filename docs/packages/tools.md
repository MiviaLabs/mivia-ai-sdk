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

## Functions and methods

- `New()` — creates an empty `Registry`.
- `Registry.Add(t)` — registers `t` under `t.Name()`.
- `Registry.Get(name)` — resolves `name` to a `Tool`. Returns false
  for an unknown name.
- `Registry.Remove(name)` — drops `name` from the registry. Returns
  whether it was present.
- `Registry.Run(ctx, name, in)` — resolves `name` through `Get` and
  runs the tool.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrNilTool` — `Add` got a nil `Tool`.
- `ErrBlankName` — `Add` got a tool whose `Name()` is blank, after
  `strings.TrimSpace`.
- `ErrDuplicateName` — `Add` got a name already registered.
- `ErrUnknownName` — `Run` got a name `Get` reports as absent.

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
- `Registry` is safe for concurrent `Add`, `Get`, `Remove`, and `Run`;
  a `sync.RWMutex` guards the map.

## Why this shape

`Tool` is an interface because tools are many and replaceable: an
agent's step names a tool by string, and the registry resolves it at
run time without the caller knowing the concrete type. `Registry`
mirrors `room.Room`'s membership shape: `Add` and `Remove` pair the
same way `Room.Admit` and `Room.Remove` do, so a tool can be
withdrawn, not only added.

## Cross-references

- [room.md](room.md) — `Room.Admit`/`Room.Remove` is the precedent
  for `Registry.Add`/`Registry.Remove`'s add-and-remove symmetry.
- `tools` imports no other package in this module; no package yet
  imports `tools`. The agent binding is a later phase.

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
