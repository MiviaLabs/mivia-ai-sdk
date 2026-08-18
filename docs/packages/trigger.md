# Package reference: trigger

The trigger package gives every part of this SDK one shared vocabulary
for "a condition fired, so run this": `Condition`, `Action`, and a
`Registry` that maps a name to one of each. `trigger` is a leaf
package: no I/O, no goroutine, no persistence, and no polling loop of
its own. The exported surface below mirrors `api/trigger.txt`.

## Types

- `Condition` — `func(ctx context.Context) (bool, error)`. Reports
  whether a named trigger's `Action` should run. A nil `Condition`
  passed to `Add` means "always ready," matching `machine.Guard`'s own
  nil convention.
- `Action` — `func(ctx context.Context) error`. The invocable a named
  trigger runs once its `Condition` is satisfied. `Add` rejects a nil
  `Action`.
- `Registry` — holds named triggers. Built only through `New`. Safe
  for concurrent `Add`, `Remove`, and `Fire`; a `sync.Mutex` guards the
  map.

## Functions and methods

- `New()` — creates an empty `Registry`.
- `Registry.Add(name, c, a)` — registers `c` and `a` under `name`.
- `Registry.Remove(name)` — removes `name`. Returns whether `name` was
  present.
- `Registry.Fire(ctx, name)` — resolves `name`, evaluates its
  `Condition`, and, when true, calls its `Action`.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrBlankName` — `Add`'s error when `name` is empty after
  `strings.TrimSpace`.
- `ErrNilAction` — `Add`'s error for a nil `Action`.
- `ErrDuplicateName` — `Add`'s error for a `name` already registered.
- `ErrUnknownName` — `Fire`'s error when `name` is not registered.
- `ErrConditionNotMet` — `Fire`'s error when the named entry's
  `Condition` evaluates false. `Fire` does not call `Action` in this
  case.

## Invariants

- `Add` rejects a blank `name` (empty after `strings.TrimSpace`) with
  `ErrBlankName`, checked before the nil-`Action` check.
- `Add` rejects a nil `a` with `ErrNilAction`.
- `Add` rejects a duplicate `name` with `ErrDuplicateName`.
- `Add` accepts a nil `c`; `Fire` reads a nil `Condition` as always
  true.
- `Remove` on an absent `name` is not a fault. It returns `false` and
  changes nothing.
- `Fire` returns `ErrUnknownName` when `name` is not registered.
- `Fire` returns a `Condition` evaluation error wrapped
  `trigger: %q: %w`, without calling `Action`.
- `Fire` returns `ErrConditionNotMet` when the `Condition` evaluates
  false, without calling `Action`.
- `Fire` releases the registry's mutex before it calls the resolved
  `Action`, so a slow `Action` never blocks a concurrent `Add` or
  `Remove`. An `Action` already resolved by `Fire` runs to completion
  even if a concurrent `Remove` deletes the entry mid-call.

## Why this shape

`Condition` matches `machine.Guard`'s exact signature. `trigger`
reuses it rather than inventing a new predicate shape. `Action`'s
signature is chosen to match a planned scheduler package's job shape
(not yet shipped in this module), so the two stay interchangeable with
no adapter once that package lands. `trigger` declines an import edge
to `channel` and `events`, and will decline one to the future
scheduler package too: composition with each happens in one line of
caller code, not inside this package. See
[../plans/trigger.md](../plans/trigger.md) for the full design
rationale.

## Wire contract

`trigger` defines no wire format. `Condition` and `Action` are plain
func values that cross no boundary inside this package; no
conformance vector applies.

## Usage

```go
package main

import (
    "context"
    "errors"
    "fmt"

    "github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

func main() {
    r := trigger.New()

    ready := func(ctx context.Context) (bool, error) { return true, nil }
    run := func(ctx context.Context) error {
        fmt.Println("action ran")
        return nil
    }

    if err := r.Add("deploy", ready, run); err != nil {
        panic(err)
    }

    err := r.Fire(context.Background(), "deploy")
    if errors.Is(err, trigger.ErrConditionNotMet) {
        fmt.Println("not ready yet")
        return
    }
    if err != nil {
        panic(err)
    }
}
```

### What the program shows

`Add` registers a trigger named `"deploy"` with an always-true
`Condition` and an `Action` that prints a line. `Fire` evaluates the
`Condition`, finds it true, and calls the `Action`. The program prints
`action ran`.

Cross-reference: [../plans/trigger.md](../plans/trigger.md) documents
three composition patterns in caller code — scheduled polling through
a `scheduler.Job`, event-driven firing through an `events.Handler`,
and answer-gated firing through a `channel.Notifier` — none of which
adds an import edge to `trigger`.
