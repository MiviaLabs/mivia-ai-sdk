package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunScopedApprovalApprovedRuns proves Approve returning
// (true, nil) runs the tool and returns its result.
func TestRunScopedApprovalApprovedRuns(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "echo", result: "echoed"}); err != nil {
		t.Fatalf("Add(echo) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) { return true, nil },
	})
	out, err := r.RunScoped(context.Background(), "echo", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(echo) error = %v, want nil", err)
	}
	if out.Value != "echoed" {
		t.Fatalf("RunScoped(echo).Value = %v, want echoed", out.Value)
	}
}

// TestRunScopedApprovalDeclinedNeverRuns proves Approve returning
// (false, nil) returns ErrToolDeclined and never calls the tool's
// Run, proven by a counter on a stub Tool.
func TestRunScopedApprovalDeclinedNeverRuns(t *testing.T) {
	r := tools.New()
	ran := false
	if err := r.Add(&runFlagTool{name: "delete", ran: &ran}); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) { return false, nil },
	})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrToolDeclined) {
		t.Fatalf("RunScoped(delete) error = %v, want ErrToolDeclined", err)
	}
	if ran {
		t.Fatalf("RunScoped(delete) ran the tool, want it never to run")
	}
}

// TestRunScopedApprovalErrorPropagatesUnwrapped proves Approve
// returning a non-nil error returns that exact error, unwrapped, and
// never calls Run.
func TestRunScopedApprovalErrorPropagatesUnwrapped(t *testing.T) {
	r := tools.New()
	ran := false
	if err := r.Add(&runFlagTool{name: "delete", ran: &ran}); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	approvalErr := errors.New("approval mechanism unavailable")
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) { return false, approvalErr },
	})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
	if !errors.Is(err, approvalErr) {
		t.Fatalf("RunScoped(delete) error = %v, want %v", err, approvalErr)
	}
	if err != approvalErr {
		t.Fatalf("RunScoped(delete) error = %v (%p), want the exact approvalErr value %v (%p), unwrapped", err, err, approvalErr, approvalErr)
	}
	if errors.Is(err, tools.ErrToolDeclined) {
		t.Fatalf("RunScoped(delete) error = %v, want distinct from ErrToolDeclined", err)
	}
	if ran {
		t.Fatalf("RunScoped(delete) ran the tool, want it never to run")
	}
}

// TestRunScopedApprovalNilApproveSkipsCheck proves a nil Approve
// field skips the check entirely, matching a Scope with no approval
// configured.
func TestRunScopedApprovalNilApproveSkipsCheck(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "echo", result: "echoed"}); err != nil {
		t.Fatalf("Add(echo) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{})
	out, err := r.RunScoped(context.Background(), "echo", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(echo) error = %v, want nil", err)
	}
	if out.Value != "echoed" {
		t.Fatalf("RunScoped(echo).Value = %v, want echoed", out.Value)
	}
}

// TestRunScopedApprovalNilScopeSkipsCheck proves a nil scope argument
// skips the check, matching phase 31's existing nil-scope behavior.
func TestRunScopedApprovalNilScopeSkipsCheck(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "echo", result: "echoed"}); err != nil {
		t.Fatalf("Add(echo) error = %v, want nil", err)
	}
	out, err := r.RunScoped(context.Background(), "echo", tools.InOut{}, nil)
	if err != nil {
		t.Fatalf("RunScoped(echo, nil scope) error = %v, want nil", err)
	}
	if out.Value != "echoed" {
		t.Fatalf("RunScoped(echo, nil scope).Value = %v, want echoed", out.Value)
	}
}
