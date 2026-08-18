package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunScopedApprovalOrderDenylistGatesBeforeApprove proves a name
// denied by ExtraDenylist returns ErrScopeDenied and never calls
// Approve, even when Approve is set and would return true.
func TestRunScopedApprovalOrderDenylistGatesBeforeApprove(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "delete", result: "gone"}); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	called := false
	scope := tools.NewScope(tools.ScopeOptions{
		ExtraDenylist: []string{"delete"},
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			called = true
			return true, nil
		},
	})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped(delete) error = %v, want ErrScopeDenied", err)
	}
	if called {
		t.Fatalf("Approve was called, want it skipped")
	}
}

// TestRunScopedApprovalOrderAbsentAllowlistGatesBeforeApprove proves a
// name absent from a non-empty Allowlist returns ErrScopeDenied and
// never calls Approve.
func TestRunScopedApprovalOrderAbsentAllowlistGatesBeforeApprove(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "delete", result: "gone"}); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	called := false
	scope := tools.NewScope(tools.ScopeOptions{
		Allowlist: []string{"echo"},
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			called = true
			return true, nil
		},
	})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped(delete) error = %v, want ErrScopeDenied", err)
	}
	if called {
		t.Fatalf("Approve was called, want it skipped")
	}
}

// TestRunScopedApprovalOrderUnapprovedPrivilegedGatesBeforeApprove
// proves an unapproved privileged tool returns ErrScopeDenied and
// never calls Approve.
func TestRunScopedApprovalOrderUnapprovedPrivilegedGatesBeforeApprove(t *testing.T) {
	r := tools.New()
	priv := &privilegedMarkerTool{stubTool: stubTool{name: "delete", result: "gone"}, privileged: true}
	if err := r.Add(priv); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	called := false
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			called = true
			return true, nil
		},
	})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped(delete) error = %v, want ErrScopeDenied", err)
	}
	if called {
		t.Fatalf("Approve was called, want it skipped")
	}
}
