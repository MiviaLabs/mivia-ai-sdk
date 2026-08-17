package events

import (
	"context"
	"fmt"
	"sync"
)

// Name is the typed event kind that separates subscriptions.
// A domain defines its own Name constants, so a literal never survives.
// The zero value is an empty name, which Validate rejects.
type Name string

// Event is one typed change a caller emits onto a bus.
// Name is the event kind. Data is the opaque payload.
type Event struct {
	Name Name
	Data string
}

// Validate checks the field rules on an event.
// It rejects an empty Name. It rejects an empty Data.
// Emit calls it before dispatch.
func (e Event) Validate() error {
	if e.Name == "" {
		return fmt.Errorf("events: event name must not be empty")
	}
	if e.Data == "" {
		return fmt.Errorf("events: event data must not be empty")
	}
	return nil
}

// Handler runs one callback for one event.
// It returns an error the bus does not propagate.
type Handler func(ctx context.Context, e Event) error

// Bus holds one subscription set for event dispatch.
// It is safe for concurrent use but it does not order goroutines.
// The caller owns the bus; the module has no shared bus.
// The zero value is not usable; create a bus with New.
type Bus struct {
	mu   sync.Mutex
	subs map[Name][]Handler
}

// New creates a bus with an empty subscription set.
// It has no error path; a fresh bus cannot reject the caller.
func New() *Bus {
	return &Bus{subs: make(map[Name][]Handler)}
}

// Subscribe adds a handler for one event name.
// It rejects an empty name and a nil handler.
func (b *Bus) Subscribe(name Name, h Handler) error {
	if name == "" {
		return fmt.Errorf("events: subscription name must not be empty")
	}
	if h == nil {
		return fmt.Errorf("events: handler must not be nil")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[name] = append(b.subs[name], h)
	return nil
}

// Emit validates an event, then runs each handler for its name.
// It rejects an unknown name and an invalid event with an error.
// Emit copies the handler slice under the mutex, then runs each
// handler unlocked. Handlers for one event run in order. A handler
// never stops Emit; all handlers still run after one fails. Emit
// does not propagate a handler error. Emit never starts a goroutine.
func (b *Bus) Emit(ctx context.Context, e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	handlers := append([]Handler(nil), b.subs[e.Name]...)
	b.mu.Unlock()
	if len(handlers) == 0 {
		return fmt.Errorf("events: no subscriber for name %q", e.Name)
	}
	for _, h := range handlers {
		_ = h(ctx, e)
	}
	return nil
}
