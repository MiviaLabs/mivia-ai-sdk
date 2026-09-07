package tools_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestScopeOptionsValidate pins the approval-threshold invariant: the
// four declared classes pass, anything else fails with
// ErrUnknownApprovalThreshold, because RunScoped would silently treat
// an unknown class as "never approve".
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
	if !errors.Is(err, tools.ErrUnknownApprovalThreshold) {
		t.Fatalf("Validate(admin) = %v, want ErrUnknownApprovalThreshold", err)
	}
}
