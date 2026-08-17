# Package reference: events

The events package is the in-process reaction bus. It is a building
block with one concern: a caller emits a typed event, and a subscriber
runs one callback per event. The bus is caller-owned; the module has no
shared bus. The package imports nothing of this module. The exported
surface below mirrors `api/events.txt`.

## Types

- `Event` — one typed change a caller emits. Fields: `Name` and `Data`.
  `Name` is the event kind. `Data` is the opaque payload.
- `Handler` — the subscriber callback. The signature is
  `func(context.Context, Event) error`. The bus does not propagate the
  returned error.
- `Bus` — one subscription set for event dispatch. It is safe for
  concurrent use but it does not order goroutines. The zero value is
  not usable; create a bus with `New`.

## Functions and methods

- `New()` — creates a bus with an empty subscription set. It has no
  error path.
- `Bus.Subscribe(name, handler)` — adds a handler for one event name.
  It rejects an empty name and a nil handler.
- `Bus.Emit(ctx, event)` — validates the event, then runs each handler
  for its name. It rejects an unknown name with an error.

## Invariants

`Validate`, `Subscribe`, and `Emit` enforce the rules below.

- `Event.Validate` rejects an empty `Name` and an empty `Data`.
- `Subscribe` rejects an empty name and a nil handler.
- `Emit` rejects an invalid event and an unknown name.
- `Emit` copies the handler slice under the mutex, then runs each
  handler unlocked. A handler may call `Subscribe` or `Emit`; such a
  call dispatches on the inner bus state.
- Handlers for one event run in order.
- A handler error does not stop `Emit`. All handlers still run when one
  fails. `Emit` does not propagate a handler error; its error covers
  only unknown-name and `Event` validation.
- `Emit` never starts a goroutine.
- The zero value of `Bus` is not usable. `New` is the only sanctioned
  construction.

## Usage

```go
b := events.New()
if err := b.Subscribe("machine.move", func(ctx context.Context, e events.Event) error {
    // react to the move
    return nil
}); err != nil {
    // the name or handler was invalid
}
if err := b.Emit(context.Background(), events.Event{Name: "machine.move", Data: "running"}); err != nil {
    // the event was invalid or had no subscriber
}
```
