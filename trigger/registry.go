// Package trigger gives every part of this SDK one shared vocabulary
// for "a condition fired, so run this": Condition, Action, and a
// Registry mapping a name to one of each. A leaf package: no I/O, no
// goroutine, no persistence, no polling loop of its own.
package trigger

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
	ErrBlankName = errors.New("trigger: name must not be blank")
	// ErrNilAction is Add's error for a nil Action; a trigger with
	// nothing to run has no purpose.
	ErrNilAction = errors.New("trigger: action must not be nil")
	// ErrDuplicateName is Add's error for a name already registered.
	ErrDuplicateName = errors.New("trigger: name already registered")
	// ErrUnknownName is Fire's error when name is not registered.
	ErrUnknownName = errors.New("trigger: unknown name")
	// ErrConditionNotMet is Fire's error when the named entry's
	// Condition evaluates false. Fire does not call Action in this
	// case.
	ErrConditionNotMet = errors.New("trigger: condition not met")
)

// Condition reports whether a named trigger's Action should run. A
// nil Condition passed to Add means "always ready," matching
// machine.Guard's own nil convention.
type Condition func(ctx context.Context) (bool, error)

// Action is the invocable a named trigger runs once its Condition is
// satisfied. Add rejects a nil Action.
type Action func(ctx context.Context) error

// entry pairs one Condition and one Action under a registered name.
type entry struct {
	condition Condition
	action    Action
}

// Registry holds named triggers. Built only through New. Safe for
// concurrent Add, Remove, and Fire; a sync.Mutex guards the map.
type Registry struct {
	mu      sync.Mutex
	entries map[string]entry
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{entries: make(map[string]entry)}
}

// Add registers c and a under name. Rejects a blank name (empty
// after strings.TrimSpace) with ErrBlankName, a nil a with
// ErrNilAction, and a duplicate name with ErrDuplicateName. A nil c
// is accepted; see Condition.
func (r *Registry) Add(name string, c Condition, a Action) error {
	if strings.TrimSpace(name) == "" {
		return ErrBlankName
	}
	if a == nil {
		return ErrNilAction
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[name]; ok {
		return ErrDuplicateName
	}
	r.entries[name] = entry{condition: c, action: a}
	return nil
}

// Remove removes name from the registry. Returns whether name was
// present. Removing an absent name is not a fault; it returns false
// and changes nothing.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[name]; !ok {
		return false
	}
	delete(r.entries, name)
	return true
}

// Fire resolves name, evaluates its Condition (a nil Condition reads
// as true), and, when true, calls its Action and returns that call's
// error. Returns ErrUnknownName when name is not registered. Returns
// ErrConditionNotMet when the Condition evaluates false, without
// calling Action. Returns a Condition evaluation error wrapped
// `trigger: %q: %w`, without calling Action. An Action already
// resolved by Fire runs to completion even if a concurrent Remove
// deletes the entry mid-call.
func (r *Registry) Fire(ctx context.Context, name string) error {
	r.mu.Lock()
	e, ok := r.entries[name]
	r.mu.Unlock()
	if !ok {
		return ErrUnknownName
	}
	if e.condition != nil {
		ready, err := e.condition(ctx)
		if err != nil {
			return fmt.Errorf("trigger: %q: %w", name, err)
		}
		if !ready {
			return ErrConditionNotMet
		}
	}
	return e.action(ctx)
}
