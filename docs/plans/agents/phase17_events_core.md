# Phase 17: events core

Status: done. Builds the events block. The events block plan lives in
`docs/plans/events.md`. This phase owns the dependency-free leaf: the
event type, the subscription, and the in-process dispatch loop. See
`docs/plans/agents/PHASES.md` for the contract.

## Goal

Provide one caller-owned, dependency-free reaction bus. A caller emits
a typed event. A subscriber registers one handler. `Emit` runs handlers
in order, in process, one at a time.

## Scope

Inside: `Event`, `Handler`, `Subscribe`, `Emit`, `New`, `Validate`, and
the mutex-guarded subscription set. Outside: the machine and flow
wiring, the envelope translation, and any general pub/sub framework.
Those belong to later event phases.

## API

- `type Event struct { Name string; Data string }`
- `func (e Event) Validate() error`. It rejects an empty `Name` and an
  empty `Data`. The rule set lives in `Validate`, not in comments.
- `type Handler func(ctx context.Context, e Event) error`
- `type Bus struct` holding one subscription set. It is safe for
  concurrent use but it does not order goroutines. The name means one
  subscription set, not a general bus.
- `func New() *Bus` creates a bus with an empty subscription set. It
  has no error path.
- `func (b *Bus) Subscribe(name string, h Handler) error` adds a
  callback. It rejects an empty name and a nil handler.
- `func (b *Bus) Emit(ctx context.Context, e Event) error` validates
  the event, then it runs each handler for the event name. It rejects
  an unknown name with an error.

`Emit` copies the handler slice under the mutex. It releases the mutex,
then it runs each handler unlocked. A handler may call `Subscribe` or
`Emit`; such a call dispatches on the inner bus state. Handlers for one
event run in order. A handler error does not stop `Emit`. All handlers
still run even when one fails. `Emit` does not propagate a handler
error. Its error covers only unknown-name and `Event` validation. The
caller logs a handler failure. `Emit` never starts a goroutine.

There is no `Shared` function. The module has no single shared bus. The
caller owns a bus and passes it to `Subscribe` and `Emit`. This phase
lands the lock in `api/events.txt`.

## Tests

Test files live in `events/events_test/`:

- `events_tdd_test.go` — the red-green cases for `Subscribe` and
  `Emit`. Start with the assertions. Confirm they fail on the empty
  implementation. Implement and watch them pass.
- `events_integration_test.go` — subscribe and emit a flow. Prove the
  handler runs in order. Prove an unknown name returns an error.
  Prove a handler error does not stop later handlers. Prove a handler
  that calls `Subscribe` dispatches on the inner bus state.
- `events_perf_test.go` — benchmark `Emit` on one event with one
  handler. State the allocation budget.

Phase 17 table cases cover an empty name, a nil handler, an empty
`Event.Name`, an empty `Event.Data`, and an unknown name at `Emit`.

## Verification

`make verify` passes. The coverage floor for `events` holds. The shape
lands in `api/events.txt` via make api-update. Concurrency uses
`go test -race ./...` for the mutex-guarded subscription set. The
machine wiring stays out of this phase. There are no conformance
vectors; the dispatch is in process, not the wire form.
