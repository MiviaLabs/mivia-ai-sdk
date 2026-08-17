# Plan: events

Status: shipped through phases 17 and 18. This plan fixes
the boundary before any builder starts. It is the bus design contract.
See docs/research-agents.md for the block composition rule. The build
phases live in docs/plans/agents/. Phase 17 ships the leaf core.
Phase 18 proves the machine wiring on a caller-owned bus.

## Goal

React to a package's state change. A package emits a typed event after
a change. A consumer subscribes and runs one callback per event.
The dispatch runs in process, in order, one at a time. The caller owns
the bus it emits onto. The package imports nothing of this module; it
stays a leaf block.

## Scope

Inside: one event type, one subscription set, and a typed dispatch
loop. The event carries a change that the caller already saw in the
return value. The bus is caller-owned; the module has no single shared
bus. Each caller creates a bus and manages its lifetime.

Outside: persistence, replay, a distributed bus, ordering across
rooms, and a general publish-subscribe framework. The envelope
delivery path does not move into this package. Envelope carries no
dispatch; it only names a message's meaning. No envelope code and no
events code imports the other.

## API

The surface below is the lock target. It lands in `api/events.txt` via
make api-update.

- `type Name string` is the typed event kind that separates
  subscriptions. A domain owns its own `Name` constants, so a literal
  never survives in shared code.
- `type Event struct { Name Name; Data string }` holds the typed
  payload. A transition move uses `Name` for the event kind and
  `Data` for a description. The payload is opaque to the bus.
- `func (e Event) Validate() error` enforces the field rules. It
  rejects an empty `Name`. It rejects an empty `Data`.
- `type Handler func(ctx context.Context, e Event) error` is the
  subscriber callback.
- `type Bus struct` holds one subscription set. It is safe for
  concurrent use but it does not order goroutines. The name means one
  subscription set, not a general bus.
- `func New() *Bus` creates a bus with an empty subscription set. It
  has no error path. A fresh bus cannot reject the caller.
- `func (b *Bus) Subscribe(name Name, h Handler) error` adds a
  callback. It rejects an empty name and a nil handler.
- `func (b *Bus) Emit(ctx context.Context, e Event) error` validates
  the event, then it runs each handler for the event name. It rejects
  an unknown name with an error. It returns that error to the emitter.

`Subscribe` and `Emit` both return `error`. `Emit` copies the handler
slice under the mutex. It releases the mutex, then it runs each handler
unlocked. A handler may call `Subscribe` or `Emit`; such a call
dispatches on the inner bus state. Handlers for one event run in order.
A handler error does not stop `Emit`. All handlers still run even when
one fails. `Emit` does not propagate a handler error; its error covers
only unknown-name and `Event` validation. The caller logs a handler
failure. `Emit` never starts a goroutine.

The bus imports nothing of this module. It is a leaf block. The import
edge is one-directional; consumers import the bus, never the reverse.
The machine package imports the bus for its typed `MoveEvent` constant.
A caller at the composition layer owns a bus. It subscribes and emits
through the bus API. The `events` row in policy/layers.json is empty;
the `machine` row lists `events`.

## Tests

Tests live in `events/events_test/`. The API lock lands via make
api-update. There are no conformance vectors; the dispatch is in
process, not the wire form.

- `events_tdd_test.go` — red-green cases for `New`, `Subscribe`,
  `Emit`, and `Event.Validate`. Start with the assertions. Confirm they
  fail on the empty implementation. Implement and watch them pass.
- `events_integration_test.go` — subscribe and emit a flow. Prove the
  handler runs in order. Prove an unknown name returns an error.
  Prove a handler error does not stop later handlers. Prove a handler
  that calls `Subscribe` dispatches on the inner bus state.
- `events_perf_test.go` — benchmark `Emit` on one event with one
  handler. State the allocation budget.

Phase 17 table cases cover an empty name, a nil handler, an empty
`Event.Name`, an empty `Event.Data`, and an unknown name at `Emit`.
All non-test rejection branches are exercised by the table tests. The
module's own emitters always set both fields. Phase 18 proves a real
machine move arrives on a caller-owned bus.

## Verification

`make verify` passes. The coverage floor for `events` holds. The shape
lands in `api/events.txt` via make api-update. Concurrency uses
`go test -race ./...` for the mutex-guarded subscription set. A new
consumer decides when this package ships. Phase 18 is that consumer.
Do not build the package before the machine caller exists.

The build is phased. Phase 17 ships the leaf core. Phase 18 proves the
machine wiring on a caller-owned bus. Phase 19 wires the flow block
onto a caller-owned bus. Phase 20 wires the envelope delivery through
the composition layer. See the phase plans for each unit.
