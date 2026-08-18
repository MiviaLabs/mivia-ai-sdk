package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunScopedUnknownName proves RunScoped returns ErrUnknownName
// for a name Get cannot resolve.
func TestRunScopedUnknownName(t *testing.T) {
	r := tools.New()
	scope := tools.NewScope(tools.ScopeOptions{})
	_, err := r.RunScoped(context.Background(), "missing", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrUnknownName) {
		t.Fatalf("RunScoped(missing) error = %v, want ErrUnknownName", err)
	}
}

// TestRunScopedDeniedNameNeverRuns proves a denied name returns
// ErrScopeDenied and never calls the tool's Run.
func TestRunScopedDeniedNameNeverRuns(t *testing.T) {
	r := tools.New()
	ran := false
	tool := &runFlagTool{name: "delete", ran: &ran}
	if err := r.Add(tool); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"echo"}})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped(delete) error = %v, want ErrScopeDenied", err)
	}
	if ran {
		t.Fatalf("RunScoped(delete) ran the tool, want it never to run")
	}
}

// TestRunScopedAllowedNameRuns proves an allowed name runs and
// returns the tool's result.
func TestRunScopedAllowedNameRuns(t *testing.T) {
	r := tools.New()
	echo := &stubTool{name: "echo", result: "echoed"}
	if err := r.Add(echo); err != nil {
		t.Fatalf("Add(echo) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"echo"}})
	out, err := r.RunScoped(context.Background(), "echo", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(echo) error = %v, want nil", err)
	}
	if out.Value != "echoed" {
		t.Fatalf("RunScoped(echo).Value = %v, want echoed", out.Value)
	}
}

// TestRunScopedNilScopeBehavesLikeRun proves a nil Scope allows every
// resolved tool, matching Run.
func TestRunScopedNilScopeBehavesLikeRun(t *testing.T) {
	r := tools.New()
	priv := &privilegedMarkerTool{stubTool: stubTool{name: "delete", result: "gone"}, privileged: true}
	if err := r.Add(priv); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}
	out, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, nil)
	if err != nil {
		t.Fatalf("RunScoped(delete, nil scope) error = %v, want nil", err)
	}
	if out.Value != "gone" {
		t.Fatalf("RunScoped(delete, nil scope).Value = %v, want gone", out.Value)
	}
}

// TestRunScopedPrivilegedToolGating proves RunScoped denies a
// privileged tool through a live Scope when the tool is absent from
// Allowlist, and runs it when the tool is present. This exercises
// privileged-tool gating through RunScoped itself, not just through a
// direct Scope.Allowed call.
func TestRunScopedPrivilegedToolGating(t *testing.T) {
	r := tools.New()
	priv := &privilegedMarkerTool{stubTool: stubTool{name: "delete", result: "gone"}, privileged: true}
	if err := r.Add(priv); err != nil {
		t.Fatalf("Add(delete) error = %v, want nil", err)
	}

	deny := tools.NewScope(tools.ScopeOptions{})
	_, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, deny)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped(delete, deny scope) error = %v, want ErrScopeDenied", err)
	}

	allow := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"delete"}})
	out, err := r.RunScoped(context.Background(), "delete", tools.InOut{}, allow)
	if err != nil {
		t.Fatalf("RunScoped(delete, allow scope) error = %v, want nil", err)
	}
	if out.Value != "gone" {
		t.Fatalf("RunScoped(delete, allow scope).Value = %v, want gone", out.Value)
	}
}

// runFlagTool is a Tool that records whether Run was called.
type runFlagTool struct {
	name string
	ran  *bool
}

func (r *runFlagTool) Name() string { return r.name }

func (r *runFlagTool) Run(_ context.Context, _ tools.InOut) (tools.Out, error) {
	*r.ran = true
	return tools.Out{}, nil
}
