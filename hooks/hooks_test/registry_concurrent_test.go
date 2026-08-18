package hooks_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
)

// TestConcurrentAddDistinctNamesAllLand runs N goroutines each
// Add-ing a distinct counting handler at one point, then Fires that
// point once. Every Add must land with no data race, and the Fire
// must run all N handlers.
func TestConcurrentAddDistinctNamesAllLand(t *testing.T) {
	r := hooks.New()
	const n = 100
	var calls int32
	counter := func(context.Context, any) (bool, error) {
		atomic.AddInt32(&calls, 1)
		return true, nil
	}
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("hook-%03d", i)
			if err := r.Add(hooks.PointStop, name, counter); err != nil {
				t.Errorf("Add(%s) error = %v, want nil", name, err)
			}
		}()
	}
	wg.Wait()

	if err := r.Fire(context.Background(), hooks.PointStop, nil); err != nil {
		t.Fatalf("Fire(stop) = %v, want nil", err)
	}
	if got := atomic.LoadInt32(&calls); got != n {
		t.Fatalf("handlers ran %d times, want %d", got, n)
	}
}

// TestConcurrentRemoveRacesFire races churn goroutines, each cycling
// Add and Remove for its own handler's name, against Fire goroutines,
// with a bystander handler always registered. Every Remove shifts
// live entries, so a Fire call may walk the slice mid-shift; the
// race detector proves the copy-on-write keeps that safe. Fire holds
// zero or more churned handlers plus the bystander; every call
// returns nil, and the bystander runs exactly once per Fire.
func TestConcurrentRemoveRacesFire(t *testing.T) {
	r := hooks.New()
	var bystander int32
	churnH := func(context.Context, any) (bool, error) { return true, nil }
	bystanderH := func(context.Context, any) (bool, error) {
		atomic.AddInt32(&bystander, 1)
		return true, nil
	}
	if err := r.Add(hooks.PointPostTool, "bystander", bystanderH); err != nil {
		t.Fatalf("Add(bystander): %v", err)
	}

	const pairs = 50
	const rounds = 20
	const fires = pairs * rounds
	var wg sync.WaitGroup
	wg.Add(2 * pairs)
	for i := 0; i < pairs; i++ {
		name := fmt.Sprintf("churn-%02d", i)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				if err := r.Add(hooks.PointPostTool, name, churnH); err != nil {
					t.Errorf("Add(%s) = %v, want nil", name, err)
					return
				}
				if !r.Remove(hooks.PointPostTool, name) {
					t.Errorf("Remove(%s) = false after its own Add, want true", name)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				if err := r.Fire(context.Background(), hooks.PointPostTool, nil); err != nil {
					t.Errorf("Fire = %v, want nil", err)
				}
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&bystander); got != fires {
		t.Fatalf("bystander ran %d times, want exactly %d", got, fires)
	}
}

// TestSlowHandlerDoesNotBlockAddOrRemove starts one Fire call whose
// handler blocks, then proves Add and Remove at the same point
// finish before the handler releases. Fire releases the mutex before
// it calls any handler.
func TestSlowHandlerDoesNotBlockAddOrRemove(t *testing.T) {
	r := hooks.New()
	release := make(chan struct{})
	started := make(chan struct{})
	slow := func(context.Context, any) (bool, error) {
		close(started)
		<-release
		return true, nil
	}
	if err := r.Add(hooks.PointPreTool, "slow", slow); err != nil {
		t.Fatalf("Add(slow): %v", err)
	}

	fireDone := make(chan error, 1)
	go func() {
		fireDone <- r.Fire(context.Background(), hooks.PointPreTool, nil)
	}()

	<-started // the slow handler runs now, holding no lock

	addErr := make(chan error, 1)
	go func() {
		addErr <- r.Add(hooks.PointPreTool, "other", allowHandler())
	}()
	select {
	case err := <-addErr:
		if err != nil {
			t.Errorf("Add(other) = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Add(other) blocked on a slow handler; want it to proceed")
	}

	removed := make(chan bool, 1)
	go func() {
		removed <- r.Remove(hooks.PointPreTool, "other")
	}()
	select {
	case ok := <-removed:
		if !ok {
			t.Error("Remove(other) = false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("Remove(other) blocked on a slow handler; want it to proceed")
	}

	close(release)
	if err := <-fireDone; err != nil {
		t.Errorf("Fire(slow) = %v, want nil", err)
	}
}
