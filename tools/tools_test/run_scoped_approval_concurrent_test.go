package tools_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

// TestRunScopedApprovalReleasesLockBeforeApprove proves RunScoped
// releases the registry lookup lock before it calls scope.approve, as
// documented on RunScoped: a blocking Approve must never block another
// registry caller. It starts a RunScoped call whose Approve blocks on
// a channel, waits until Approve is confirmed running, then races an
// Add for a distinct name against it. The Add must complete well
// before Approve is released; if RunScoped held the lock across
// approve, the Add would block until release closes and this test
// would time out.
func TestRunScopedApprovalReleasesLockBeforeApprove(t *testing.T) {
	r := tools.New()
	if err := r.Add(&stubTool{name: "slow", result: "done"}); err != nil {
		t.Fatalf("Add(slow) error = %v, want nil", err)
	}

	approveStarted := make(chan struct{})
	release := make(chan struct{})
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			close(approveStarted)
			<-release
			return true, nil
		},
	})

	runDone := make(chan error, 1)
	go func() {
		_, err := r.RunScoped(context.Background(), "slow", tools.InOut{}, scope)
		runDone <- err
	}()

	select {
	case <-approveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Approve never started")
	}

	addDone := make(chan error, 1)
	go func() {
		addDone <- r.Add(&stubTool{name: "other", result: "ok"})
	}()

	select {
	case err := <-addDone:
		if err != nil {
			t.Fatalf("Add(other) error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Add(other) did not complete while Approve was still running; RunScoped appears to hold the registry lock across approve")
	}

	close(release)
	if err := <-runDone; err != nil {
		t.Fatalf("RunScoped(slow) error = %v, want nil", err)
	}
}
