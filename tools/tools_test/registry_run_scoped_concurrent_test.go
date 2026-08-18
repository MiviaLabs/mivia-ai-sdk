package tools_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestConcurrentRunScopedAllowingRacesRemove registers one tool, then
// races N RunScoped calls under an allowing Scope against N Remove
// calls for the same name. Every RunScoped call must return either
// the tool's result or ErrUnknownName (removed before Get resolved
// it), never ErrScopeDenied.
func TestConcurrentRunScopedAllowingRacesRemove(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"shared"}})

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

// TestConcurrentRunScopedDenyingRacesRemove races N RunScoped calls
// under a denying Scope against N Remove calls for the same name.
// Every call must return either ErrScopeDenied or ErrUnknownName. No
// call may panic.
func TestConcurrentRunScopedDenyingRacesRemove(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{ExtraDenylist: []string{"shared"}})

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
			if !errors.Is(err, tools.ErrScopeDenied) && !errors.Is(err, tools.ErrUnknownName) {
				t.Errorf("RunScoped(shared) error = %v, want ErrScopeDenied or ErrUnknownName", err)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentRunScopedAndAdd races N RunScoped calls for a
// registered name under an allowing Scope against N goroutines
// calling Add for N distinct other names. Every RunScoped call must
// return the tool's result with no error, and a following Get loop
// must find all N added names.
func TestConcurrentRunScopedAndAdd(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"shared"}})

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			out, err := r.RunScoped(context.Background(), "shared", tools.InOut{}, scope)
			if err != nil {
				t.Errorf("RunScoped(shared) error = %v, want nil", err)
				return
			}
			if out.Value != "shared-result" {
				t.Errorf("RunScoped(shared).Value = %v, want shared-result", out.Value)
			}
		}()
	}
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("added-%03d", i)
			if err := r.Add(&stubTool{name: name}); err != nil {
				t.Errorf("Add(%s) error = %v, want nil", name, err)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("added-%03d", i)
		if _, ok := r.Get(name); !ok {
			t.Errorf("Get(%s) ok = false, want true", name)
		}
	}
}
