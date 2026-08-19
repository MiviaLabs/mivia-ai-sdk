# Package reference: hooks

The hooks package gives a caller a named, multi-handler registry for
a lifecycle point. `Fire` runs every handler at a point in
registration order and stops at the first veto. The exported surface
below mirrors `api/hooks.txt`.

## Types

- `Point` — a lifecycle point, an int-based enum. The zero value is
  invalid; name one of the three constants. `Validate` rejects the
  zero value and any out-of-range value. `String` renders a short
  label for error text.
- `Handler` — one registered hook. It reads a context and an opaque
  payload and returns `true, nil` to allow, `false, nil` to veto, or
  a non-nil error when the handler itself failed to decide.

## Constants

- `PointPreTool` — before a tool call runs.
- `PointPostTool` — after a tool call's ack confirms.
- `PointStop` — at a run's stop.

## Functions and methods

- `New()` — creates an empty `Registry`.
- `Registry.Add(point, name, h)` — registers `h` under `name` at
  `point`. Rejects an invalid point, a blank name, a nil handler,
  and a name already registered at the same point. The same name may
  register at two different points.
- `Registry.Remove(point, name)` — removes `name` from `point`.
  Returns whether the pair was present.
- `Registry.Fire(ctx, point, payload)` — runs every handler at
  `point`, in registration order. An empty point returns nil at
  once. A veto or a handler error stops the chain at once. All-allow
  returns nil.

## Failure modes

Use `errors.Is` to test these.

- `ErrBlankName` ("hooks: name must not be blank") — `Add` returns
  it when `name` is empty after `strings.TrimSpace`. Pinned by
  `hooks/hooks_test/registry_add_test.go`.
- `ErrNilHandler` ("hooks: handler must not be nil") — `Add` returns
  it for a nil `Handler`. Pinned by
  `hooks/hooks_test/registry_add_test.go`.
- `ErrDuplicateName` ("hooks: name already registered at point") —
  `Add` returns it for a name already registered at the same point.
  The first registration stays live; `Add` never replaces an entry.
  Pinned by `hooks/hooks_test/registry_add_test.go`.
- `ErrVetoed` ("hooks: handler vetoed") — `Fire` returns it wrapped
  `hooks: %s: handler %q: %w` when a handler returns `false` with a
  nil error. Pinned by
  `hooks/hooks_test/registry_fire_test.go`.

## Invariants

- `Add` checks in order: the `Point`, then the name, then the
  handler, then the duplicate. A call invalid in two ways reports the
  earlier check.
- `name` scopes to one `Point`, not to the whole `Registry`. Two
  points may hold the same label.
- `Fire` validates the `Point` before it calls any handler. An
  invalid point never runs a handler.
- `Fire` runs handlers in registration order. A veto or a handler
  error stops the chain; no later handler runs.
- The veto wrap embeds `ErrVetoed`; the failure wrap embeds the
  handler's own error. `errors.Is` tells the two apart.
- Every handler run receives the exact payload value passed to
  `Fire`. `hooks` never inspects or rewrites it.
- `Fire` treats a point with no registered handlers as a no-op nil.
- `Registry` is safe for concurrent `Add`, `Remove`, and `Fire`. A
  `sync.Mutex` guards the map. `Fire` releases the mutex before it
  calls a handler, so a slow handler never blocks a concurrent `Add`
  or `Remove`.

## Wire contract

`hooks` defines no wire format. It holds in-process handler values;
no conformance vector applies.

## Usage

```go
r := hooks.New()

observer := func(_ context.Context, payload any) (bool, error) {
    fmt.Println("tool call:", payload)
    return true, nil
}
blocker := func(_ context.Context, _ any) (bool, error) {
    return false, nil // veto: stop the action
}

_ = r.Add(hooks.PointPreTool, "audit-log", observer)
_ = r.Add(hooks.PointPreTool, "policy-gate", blocker)

err := r.Fire(context.Background(), hooks.PointPreTool, "rm -rf /tmp/x")
// err wraps hooks.ErrVetoed: "policy-gate" said no, so the action
// does not run. The observer's log line landed first.
```

### What the program shows

`Fire` runs the two `PointPreTool` handlers in registration order.
The observer allows and its side effect lands. The blocker vetoes,
so `Fire` returns the `ErrVetoed` wrap and the action never runs.
