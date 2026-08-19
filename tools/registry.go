package tools

import (
	"context"
	"errors"
	"sort"
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
	// ErrScopeDenied is RunScoped's error when scope.Allowed returns
	// false for the resolved tool.
	ErrScopeDenied = errors.New("tools: tool denied by scope")
	// ErrToolDeclined is RunScoped's error when scope.Approve returns
	// (false, nil). Test with errors.Is. Phase 36 addition.
	ErrToolDeclined = errors.New("tools: tool declined by approval")
)

// ToolCall describes one call RunScoped is about to make, passed to
// a Scope's Approve function. Name is the resolved tool's
// registration name. In is the caller's input payload, unchanged from
// the RunScoped call. Profile is ExecutionProfileOf(t) for the
// resolved tool.
type ToolCall struct {
	Name    string
	In      InOut
	Profile ExecutionProfile
}

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

// Tools returns a snapshot of every registered Tool, sorted by name.
// The result is a fresh slice; mutating it does not affect the
// Registry. The result is empty and non-nil for an empty Registry.
func (r *Registry) Tools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, r.tools[name])
	}
	return out
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

// RunScoped resolves name through Get, checks scope.Allowed when
// scope is non-nil, then calls the tool the same way Run does.
// Returns ErrUnknownName for an unresolved name and ErrScopeDenied
// for a name the scope excludes. A nil scope allows every resolved
// tool, matching Run's behavior. After scope.Allowed passes, when
// scope.approve is non-nil and the resolved tool's rank meets or
// exceeds scope.approvalThreshold's rank, RunScoped calls
// scope.approve before it calls Run. approve returning (true, nil)
// proceeds to Run. approve returning (false, nil) returns
// ErrToolDeclined. approve returning a non-nil error returns that
// error unchanged. The map lookup lock is released before approve
// runs, so a blocking approve never blocks other registry callers.
func (r *Registry) RunScoped(ctx context.Context, name string, in InOut, scope *Scope) (Out, error) {
	t, ok := r.Get(name)
	if !ok {
		return Out{}, ErrUnknownName
	}
	if scope == nil {
		return t.Run(ctx, in)
	}
	if !scope.Allowed(name, t) {
		return Out{}, ErrScopeDenied
	}
	profile := ExecutionProfileOf(t)
	if scope.approve != nil && executionClassRank(profile.Class) >= executionClassRank(scope.approvalThreshold) {
		approved, err := scope.approve(ctx, ToolCall{Name: name, In: in, Profile: profile})
		if err != nil {
			return Out{}, err
		}
		if !approved {
			return Out{}, ErrToolDeclined
		}
	}
	return t.Run(ctx, in)
}
