package hooks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Sentinel errors for Registry operations; test with errors.Is.
var (
	// ErrBlankName is Add's error when name is empty after
	// strings.TrimSpace.
	ErrBlankName = errors.New("hooks: name must not be blank")
	// ErrNilHandler is Add's error for a nil Handler; a hook with
	// nothing to run has no purpose.
	ErrNilHandler = errors.New("hooks: handler must not be nil")
	// ErrDuplicateName is Add's error for a name already registered
	// at the same point. The same name may register at another point.
	ErrDuplicateName = errors.New("hooks: name already registered at point")
	// ErrVetoed is Fire's wrapped error when a handler returns false
	// with a nil error; test with errors.Is.
	ErrVetoed = errors.New("hooks: handler vetoed")
)

// Handler observes or vetoes one lifecycle point's action. payload is
// opaque to hooks: the caller that fires a point supplies whatever
// value that point's real action carries. Handler returns true, nil
// to allow the action to continue, false, nil to veto it, or a
// non-nil error when the handler itself failed to decide.
type Handler func(ctx context.Context, payload any) (bool, error)

// entry pairs one registered Handler with its name, in registration
// order.
type entry struct {
	name string
	h    Handler
}

// Registry holds named handlers grouped by Point, in registration
// order. Safe for concurrent Add, Remove, and Fire; a sync.Mutex
// guards the map. Build one with New.
type Registry struct {
	mu       sync.Mutex
	handlers map[Point][]entry
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{handlers: make(map[Point][]entry)}
}

// Add registers h under name at point. Rejects an invalid point with
// its Validate error, a blank name (empty after strings.TrimSpace)
// with ErrBlankName, a nil h with ErrNilHandler, and a name already
// registered at that same point with ErrDuplicateName. The same name
// may register at two different points; name scopes to one Point.
func (r *Registry) Add(point Point, name string, h Handler) error {
	if err := point.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return ErrBlankName
	}
	if h == nil {
		return ErrNilHandler
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.handlers[point]
	for _, e := range current {
		if e.name == name {
			return ErrDuplicateName
		}
	}
	// Copy-on-write: a Fire call may walk the old slice outside the
	// lock, so Add never appends into a backing array Fire reads.
	fresh := make([]entry, len(current), len(current)+1)
	copy(fresh, current)
	fresh = append(fresh, entry{name: name, h: h})
	if r.handlers == nil {
		r.handlers = make(map[Point][]entry)
	}
	r.handlers[point] = fresh
	return nil
}

// Remove removes name from point. Returns whether the pair was
// present. Removing an absent name is not a fault; it returns false
// and changes nothing.
func (r *Registry) Remove(point Point, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.handlers[point]
	for i, e := range current {
		if e.name != name {
			continue
		}
		fresh := make([]entry, 0, len(current)-1)
		fresh = append(fresh, current[:i]...)
		fresh = append(fresh, current[i+1:]...)
		r.handlers[point] = fresh
		return true
	}
	return false
}

// Fire runs every handler registered at point, in registration
// order. An invalid point returns its Validate error at once, with no
// handler call. A point with no registered handlers returns nil at
// once. A handler returning true, nil moves Fire to the next
// handler. A handler returning false, nil stops Fire and returns
// ErrVetoed wrapped `hooks: %s: handler %q: %w`. A handler returning
// a non-nil error stops Fire and returns that error wrapped the same
// way. Fire returns nil once every handler has allowed. Fire
// releases the mutex before it calls a handler, so a slow handler
// never blocks a concurrent Add or Remove.
func (r *Registry) Fire(ctx context.Context, point Point, payload any) error {
	if err := point.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	entries := r.handlers[point]
	r.mu.Unlock()
	for _, e := range entries {
		allow, err := e.h(ctx, payload)
		if err != nil {
			return fmt.Errorf("hooks: %s: handler %q: %w", point, e.name, err)
		}
		if !allow {
			return fmt.Errorf("hooks: %s: handler %q: %w", point, e.name, ErrVetoed)
		}
	}
	return nil
}
