package providerregistry

import (
	"errors"
	"strings"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// Sentinel errors for Registry operations; test with errors.Is.
var (
	// ErrNilCompleter is Register's error for a nil Completer
	// interface value, the c == nil case. Register checks c == nil
	// before it calls any method on c. A typed nil pointer that
	// implements Completer is not nil as an interface value; Register
	// cannot detect it without reflection, which this module forbids
	// in packages. Passing one is caller error.
	ErrNilCompleter = errors.New("providerregistry: completer must not be nil")
	// ErrBlankName is Register's error when name is empty after
	// strings.TrimSpace. A completer needs a real name to register
	// under and to look up later.
	ErrBlankName = errors.New("providerregistry: name must not be blank")
	// ErrDuplicateName is Register's error for a name already
	// registered. Register never replaces an entry.
	ErrDuplicateName = errors.New("providerregistry: name already registered")
	// ErrUnknownName is Route's error for a name in order that Get
	// cannot resolve. The error's text names the missing entry.
	ErrUnknownName = errors.New("providerregistry: unknown name")
	// ErrEmptyOrder is Route's error for an order with no entries.
	// Route checks it before it calls any Completer.
	ErrEmptyOrder = errors.New("providerregistry: order must not be empty")
	// ErrAllFailed is Route's error when every name in order was tried
	// and every attempt failed the retryable check. It carries the
	// last attempt's error; errors.Unwrap on Route's returned error
	// yields that error.
	ErrAllFailed = errors.New("providerregistry: every name in order failed")
)

// Registry holds completers by name. Built only through New. Safe for
// concurrent Register, Get, Names, and Route; a sync.RWMutex guards
// the map.
type Registry struct {
	mu         sync.RWMutex
	completers map[string]provider.Completer
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{completers: make(map[string]provider.Completer)}
}

// Register adds c under name. Rejects a nil c (c == nil) with
// ErrNilCompleter, before it calls any method on c. A typed nil
// pointer that implements Completer is caller error; see
// ErrNilCompleter. Rejects a blank name (empty after
// strings.TrimSpace) with ErrBlankName. Rejects a name already
// registered with ErrDuplicateName. Register never replaces an
// existing entry.
func (r *Registry) Register(name string, c provider.Completer) error {
	if c == nil {
		return ErrNilCompleter
	}
	if strings.TrimSpace(name) == "" {
		return ErrBlankName
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
func (r *Registry) Get(name string) (provider.Completer, bool) {
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
