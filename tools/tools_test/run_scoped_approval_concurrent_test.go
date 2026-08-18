package tools_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestConcurrentRunScopedApprovingRacesRemove registers a tool
// requiring approval, then races N RunScoped calls under a Scope
// whose Approve always approves against N Remove calls for the same
// name. Every call must return either the tool's result or
// ErrUnknownName; no call panics.
func TestConcurrentRunScopedApprovingRacesRemove(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) { return true, nil },
	})

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			r.Remove("shared")
		}()
	}
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			out, err := r.RunScoped(context.Background(), "shared", tools.InOut{}, scope)
			if err == nil {
				if out.Value != "shared-result" {
					t.Errorf("RunScoped(shared) Value = %v, want shared-result", out.Value)
				}
				return
			}
			if !errors.Is(err, tools.ErrUnknownName) {
				t.Errorf("RunScoped(shared) error = %v, want nil or ErrUnknownName", err)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentRunScopedDenyingApprovalRacesRemove races N
// RunScoped calls under a Scope whose Approve always denies against N
// Remove calls for the same name. Every call must return either
// ErrToolDeclined or ErrUnknownName; no call panics.
func TestConcurrentRunScopedDenyingApprovalRacesRemove(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) { return false, nil },
	})

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			r.Remove("shared")
		}()
	}
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := r.RunScoped(context.Background(), "shared", tools.InOut{}, scope)
			if !errors.Is(err, tools.ErrToolDeclined) && !errors.Is(err, tools.ErrUnknownName) {
				t.Errorf("RunScoped(shared) error = %v, want ErrToolDeclined or ErrUnknownName", err)
			}
		}()
	}
	wg.Wait()
}
