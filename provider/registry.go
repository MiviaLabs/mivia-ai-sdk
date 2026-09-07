package provider

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Sentinel errors for Registry operations; test with errors.Is.
var (
	// ErrInvalidOptions is Register's error for a caller-supplied
	// argument that fails a construction-time sanity check: a nil
	// Completer or a blank name.
	ErrInvalidOptions = errors.New("provider: invalid options")
	// ErrDuplicateName is Register's error for a name already
	// registered. A register-if-absent caller branches on it.
	ErrDuplicateName = errors.New("provider: name already registered")
	// ErrUnknownName is Route's error for a name in order that Get
	// cannot resolve. The error's text names the missing entry.
	ErrUnknownName = errors.New("provider: unknown name")
	// ErrEmptyOrder is Route's error for an order with no entries.
	// Route checks it before it calls any Completer.
	ErrEmptyOrder = errors.New("provider: order must not be empty")
	// ErrAllFailed is Route's error when every name in order was tried
	// and every attempt failed the retryable check. It carries the
	// last attempt's error; errors.Unwrap on Route's returned error
	// yields that error.
	ErrAllFailed = errors.New("provider: every name in order failed")
)

// Registry holds completers by name. Built only through New. Safe for
// concurrent Register, Get, Names, and Route; a sync.RWMutex guards
// the map.
type Registry struct {
	mu         sync.RWMutex
	completers map[string]Completer
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{completers: make(map[string]Completer)}
}

// Register adds c under name. Rejects a nil c (c == nil) with
// ErrInvalidOptions, before it calls any method on c. A typed nil
// pointer that implements Completer is caller error; ErrInvalidOptions
// also covers it. Rejects a blank name (empty after strings.TrimSpace)
// with ErrInvalidOptions. Rejects a name already registered with
// ErrDuplicateName, unwrapped. Register never replaces an existing
// entry.
func (r *Registry) Register(name string, c Completer) error {
	if c == nil {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Completer: must not be nil")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: %s", ErrInvalidOptions, "Name: must not be blank")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.completers[name]; ok {
		return ErrDuplicateName
	}
	r.completers[name] = c
	return nil
}

// Get resolves name to its registered Completer. Returns (nil, false)
// when name is absent.
func (r *Registry) Get(name string) (Completer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.completers[name]
	return c, ok
}

// Names lists every registered name. Order is unspecified; a caller
// that needs a stable order sorts the result itself.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.completers))
	for name := range r.completers {
		names = append(names, name)
	}
	return names
}
