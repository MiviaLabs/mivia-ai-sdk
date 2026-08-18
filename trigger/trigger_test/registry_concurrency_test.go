package trigger_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

// TestConcurrentAddDistinctNamesAllLand runs N goroutines each Add-ing
// a distinct name concurrently, then Fires every one, proving
// concurrent Add calls all land with no data race.
func TestConcurrentAddDistinctNamesAllLand(t *testing.T) {
	r := trigger.New()
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("trigger-%03d", i)
			action := func(context.Context) error { return nil }
			if err := r.Add(name, nil, action); err != nil {
				t.Errorf("Add(%s) error = %v, want nil", name, err)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("trigger-%03d", i)
		if err := r.Fire(context.Background(), name); err != nil {
			t.Errorf("Fire(%s) error = %v, want nil", name, err)
		}
	}
}

// TestConcurrentRemoveRacesFire registers one trigger, then races N
// Remove calls for its name against N Fire calls for the same name.
// Every Fire call must return nil (it ran before removal) or
// ErrUnknownName (it ran after removal); no other outcome is valid.
func TestConcurrentRemoveRacesFire(t *testing.T) {
	r := trigger.New()
	const n = 100
	action := func(context.Context) error { return nil }
	if err := r.Add("shared", nil, action); err != nil {
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
			err := r.Fire(context.Background(), "shared")
			if err != nil && !errors.Is(err, trigger.ErrUnknownName) {
				t.Errorf("Fire(shared) error = %v, want nil or ErrUnknownName", err)
			}
		}()
	}
	wg.Wait()
}

// TestSlowActionDoesNotBlockAddOrRemove starts one Fire call whose
// Action blocks until a channel closes, then proves a concurrent Add
// and Remove for other names complete well before the slow Action
// finishes. This proves Fire releases the mutex before it calls the
// resolved Action.
func TestSlowActionDoesNotBlockAddOrRemove(t *testing.T) {
	r := trigger.New()
	release := make(chan struct{})
	started := make(chan struct{})
	action := func(context.Context) error {
		close(started)
		<-release
		return nil
	}
	if err := r.Add("slow", nil, action); err != nil {
		t.Fatalf("Add(slow) error = %v, want nil", err)
	}

	fireDone := make(chan error, 1)
	go func() {
		fireDone <- r.Fire(context.Background(), "slow")
	}()

	<-started // the slow Action is now running, holding no lock

	other := func(context.Context) error { return nil }
	addErr := make(chan error, 1)
	go func() {
		addErr <- r.Add("other", nil, other)
	}()

	select {
	case err := <-addErr:
		if err != nil {
			t.Errorf("Add(other) error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Add(other) blocked on a slow Action; want it to proceed")
	}

	removed := make(chan bool, 1)
	go func() {
		removed <- r.Remove("other")
	}()

	select {
	case ok := <-removed:
		if !ok {
			t.Error("Remove(other) = false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("Remove(other) blocked on a slow Action; want it to proceed")
	}

	close(release)
	if err := <-fireDone; err != nil {
		t.Errorf("Fire(slow) error = %v, want nil", err)
	}
}

// TestConcurrentAddSameNameExactlyOneWins runs N goroutines all
// calling Add with the identical name. Exactly one call must succeed
// (nil); every other call must return ErrDuplicateName.
func TestConcurrentAddSameNameExactlyOneWins(t *testing.T) {
	r := trigger.New()
	const n = 50
	var successes int32
	var duplicates int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			action := func(context.Context) error { return nil }
			err := r.Add("contested", nil, action)
			switch {
			case err == nil:
				atomic.AddInt32(&successes, 1)
			case errors.Is(err, trigger.ErrDuplicateName):
				atomic.AddInt32(&duplicates, 1)
			default:
				t.Errorf("Add(contested) error = %v, want nil or ErrDuplicateName", err)
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf("successes = %d, want 1", successes)
	}
	if duplicates != n-1 {
		t.Errorf("duplicates = %d, want %d", duplicates, n-1)
	}
}
