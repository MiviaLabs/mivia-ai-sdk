package tools

import (
	"errors"
	"time"
)

// ExecutionClass is a tool's execution-risk category. Validate
// enforces the declared set.
type ExecutionClass string

// The declared ExecutionClass values. ExecutionClassUnclassified is
// the zero value: the default for a tool with no ExecutionProfile.
const (
	ExecutionClassUnclassified ExecutionClass = ""
	ExecutionClassRead         ExecutionClass = "read"
	ExecutionClassWrite        ExecutionClass = "write"
	ExecutionClassExternal     ExecutionClass = "external"
)

// errInvalidExecutionClass is Validate's error for a value outside
// the declared ExecutionClass set. Not exported: the plan locks no
// sentinel error for this check.
var errInvalidExecutionClass = errors.New("tools: invalid execution class")

// Validate rejects any ExecutionClass value outside
// ExecutionClassUnclassified, ExecutionClassRead, ExecutionClassWrite,
// and ExecutionClassExternal.
func (c ExecutionClass) Validate() error {
	switch c {
	case ExecutionClassUnclassified, ExecutionClassRead, ExecutionClassWrite, ExecutionClassExternal:
		return nil
	default:
		return errInvalidExecutionClass
	}
}

// ExecutionProfile is execution-risk metadata for one tool: its
// class, its per-turn dedup key, and its timeout. This package
// publishes these fields; it does not enforce them. See
// docs/packages/tools.md's "Published, not enforced" section.
type ExecutionProfile struct {
	Class       ExecutionClass
	ResourceKey string
	Timeout     time.Duration
}

// ProfiledTool is an optional interface. A Tool implements it to
// publish an ExecutionProfile.
type ProfiledTool interface {
	ExecutionProfile() ExecutionProfile
}

// ResultBudgetTool is an optional interface. A Tool implements it to
// bound its output size.
type ResultBudgetTool interface {
	MaxResultBytes() int
}

// PrivilegedTool is an optional interface. A Tool implements it to
// mark itself as needing explicit allowlisting.
type PrivilegedTool interface {
	Privileged() bool
}

// ExecutionProfileOf returns t's published ExecutionProfile when t
// implements ProfiledTool; else it returns the zero ExecutionProfile,
// whose Class is ExecutionClassUnclassified. It never calls Validate;
// an out-of-enum Class passes through unchanged.
func ExecutionProfileOf(t Tool) ExecutionProfile {
	if pt, ok := t.(ProfiledTool); ok {
		return pt.ExecutionProfile()
	}
	return ExecutionProfile{}
}

// ResultBudgetOf returns t.MaxResultBytes() and true when t
// implements ResultBudgetTool; else it returns 0, false.
func ResultBudgetOf(t Tool) (int, bool) {
	if rb, ok := t.(ResultBudgetTool); ok {
		return rb.MaxResultBytes(), true
	}
	return 0, false
}

// IsPrivileged returns t.Privileged() when t implements
// PrivilegedTool; else it returns false.
func IsPrivileged(t Tool) bool {
	if pt, ok := t.(PrivilegedTool); ok {
		return pt.Privileged()
	}
	return false
}
