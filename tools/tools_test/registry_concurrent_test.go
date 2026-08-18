package tools_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestConcurrentAddDistinctNamesAllLand runs N goroutines each Add-ing
// a distinct name concurrently, then joins. A following Get loop must
// find every one of the N names, proving concurrent Add calls all
// land.
func TestConcurrentAddDistinctNamesAllLand(t *testing.T) {
	r := tools.New()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("tool-%03d", i)
			if err := r.Add(&stubTool{name: name, result: i}); err != nil {
				t.Errorf("Add(%s) error = %v, want nil", name, err)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool-%03d", i)
		if _, ok := r.Get(name); !ok {
			t.Errorf("Get(%s) ok = false, want true", name)
		}
	}
}

// TestConcurrentRunAndAdd registers one tool up front, then runs N
// goroutines calling Run for its name concurrently while N other
// goroutines call Add for N distinct other names concurrently. Every
// Run call must return the registered tool's result with no error,
// and a following Get loop must find all N added names, proving reads
// and writes on the map do not corrupt each other under -race.
func TestConcurrentRunAndAdd(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	wg.Add(2 * n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			out, err := r.Run(context.Background(), "shared", tools.InOut{})
			if err != nil {
				t.Errorf("Run(shared) error = %v, want nil", err)
				return
			}
			if out.Value != "shared-result" {
				t.Errorf("Run(shared).Value = %v, want shared-result", out.Value)
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

// TestConcurrentRemoveRacesRun registers one tool, then races N Remove
// calls for its name against N Run calls for the same name. Exactly
// one outcome is valid per Run call: either the tool's result (it ran
// before removal) or ErrUnknownName (it ran after removal). No call
// may panic or return any other error, proving Remove and Run
// serialize correctly against each other.
func TestConcurrentRemoveRacesRun(t *testing.T) {
	r := tools.New()
	const n = 100
	if err := r.Add(&stubTool{name: "shared", result: "shared-result"}); err != nil {
		t.Fatalf("Add(shared) error = %v, want nil", err)
	}

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
			out, err := r.Run(context.Background(), "shared", tools.InOut{})
			if err == nil {
				if out.Value != "shared-result" {
					t.Errorf("Run(shared).Value = %v, want shared-result", out.Value)
				}
				return
			}
			if !errors.Is(err, tools.ErrUnknownName) {
				t.Errorf("Run(shared) error = %v, want nil or ErrUnknownName", err)
			}
		}()
	}
	wg.Wait()
}
