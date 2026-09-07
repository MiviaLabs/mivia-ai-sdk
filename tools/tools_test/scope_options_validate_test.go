package tools_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestScopeOptionsValidate pins the approval-threshold invariant: the
// four declared classes pass, anything else fails with
// ErrInvalidOptions, since RunScoped would otherwise fall back to
// ranking the unknown class the same as ExecutionClassExternal.
func TestScopeOptionsValidate(t *testing.T) {
	for _, class := range []tools.ExecutionClass{
		tools.ExecutionClassUnclassified,
		tools.ExecutionClassRead,
		tools.ExecutionClassWrite,
		tools.ExecutionClassExternal,
	} {
		if err := (tools.ScopeOptions{ApprovalThreshold: class}).Validate(); err != nil {
			t.Fatalf("Validate(%q) = %v, want nil", class, err)
		}
	}
	err := (tools.ScopeOptions{ApprovalThreshold: tools.ExecutionClass("admin")}).Validate()
	if !errors.Is(err, tools.ErrInvalidOptions) {
		t.Fatalf("Validate(admin) = %v, want ErrInvalidOptions", err)
	}
	if !strings.Contains(err.Error(), "ApprovalThreshold") {
		t.Fatalf("Validate(admin) = %v, want it to name the ApprovalThreshold field", err)
	}
}

// TestNewScopeCheckedGatesOnValidate proves NewScopeChecked wires
// ScopeOptions.Validate into construction: an unknown
// ApprovalThreshold is rejected instead of silently accepted, unlike
// NewScope which builds a Scope regardless.
func TestNewScopeCheckedGatesOnValidate(t *testing.T) {
	opts := tools.ScopeOptions{ApprovalThreshold: tools.ExecutionClass("admin")}
	scope, err := tools.NewScopeChecked(opts)
	if !errors.Is(err, tools.ErrInvalidOptions) {
		t.Fatalf("NewScopeChecked(admin) error = %v, want ErrInvalidOptions", err)
	}
	if scope != nil {
		t.Fatalf("NewScopeChecked(admin) scope = %+v, want nil on Validate failure", scope)
	}
	scope, err = tools.NewScopeChecked(tools.ScopeOptions{ApprovalThreshold: tools.ExecutionClassRead})
	if err != nil {
		t.Fatalf("NewScopeChecked(Read) error = %v, want nil", err)
	}
	if scope == nil {
		t.Fatalf("NewScopeChecked(Read) scope = nil, want a built Scope")
	}
}
