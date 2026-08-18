package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Sentinel errors for Registry operations; test with errors.Is.
var (
	// ErrNilTool is Add's error for a nil Tool interface value, the
	// t == nil case. Add checks t == nil before it calls any method
	// on t. A typed nil pointer that implements Tool is not nil as an
	// interface value; Add cannot detect it without reflection, which
	// this module forbids in packages. Passing one is caller error.
	ErrNilTool = errors.New("tools: tool must not be nil")
	// ErrBlankName is Add's error when t.Name() is empty after
	// strings.TrimSpace. A tool needs a real name to register under
	// and to look up later.
	ErrBlankName = errors.New("tools: tool name must not be blank")
	// ErrDuplicateName is Add's error for a name already registered.
	ErrDuplicateName = errors.New("tools: tool name already registered")
	// ErrUnknownName is Run's error when Get reports false for name.
	ErrUnknownName = errors.New("tools: unknown tool name")
)

// Registry holds tools by name. Built only through New. Safe for
// concurrent Add, Get, Remove, and Run; a sync.RWMutex guards the map.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Add registers t under t.Name(). Rejects a nil t (t == nil) with
// ErrNilTool, before it calls t.Name(). A typed nil pointer that
// implements Tool is caller error; see ErrNilTool. Rejects a blank
// name (empty after strings.TrimSpace) with ErrBlankName. Rejects a
// duplicate name with ErrDuplicateName.
func (r *Registry) Add(t Tool) error {
	if t == nil {
		return ErrNilTool
	}
	name := t.Name()
	if strings.TrimSpace(name) == "" {
		return ErrBlankName
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		return ErrDuplicateName
	}
	r.tools[name] = t
	return nil
}

// Get resolves name to a Tool. Returns false when name is absent.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Remove removes name from the registry. Returns whether name was
// present. Removing an absent name is not a fault; it returns false
// and changes nothing.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; !ok {
		return false
	}
	delete(r.tools, name)
	return true
}

// Run resolves name through Get and calls the tool's Run. Returns
// ErrUnknownName when Get reports false.
func (r *Registry) Run(ctx context.Context, name string, in InOut) (Out, error) {
	t, ok := r.Get(name)
	if !ok {
		return Out{}, ErrUnknownName
	}
	return t.Run(ctx, in)
}
