package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
)

func TestAddBlankName(t *testing.T) {
	r := scheduler.NewRegistry()
	err := r.Add("   ", nil, func(context.Context) error { return nil })
	if !errors.Is(err, scheduler.ErrBlankName) {
		t.Fatalf("Add(blank) = %v, want ErrBlankName", err)
	}
}

func TestAddNilAction(t *testing.T) {
	r := scheduler.NewRegistry()
	err := r.Add("name", nil, nil)
	if !errors.Is(err, scheduler.ErrNilAction) {
		t.Fatalf("Add(nil action) = %v, want ErrNilAction", err)
	}
}

// TestAddBlankNameTakesPrecedenceOverNilAction pins the rejection
// order the plan and Add's doc comment promise: a blank name loses to
// a nil action when both are invalid at once. This distinguishes Add
// from an implementation that checks the fields in the other order.
func TestAddBlankNameTakesPrecedenceOverNilAction(t *testing.T) {
	r := scheduler.NewRegistry()
	err := r.Add("   ", nil, nil)
	if !errors.Is(err, scheduler.ErrBlankName) {
		t.Fatalf("Add(blank name, nil action) = %v, want ErrBlankName", err)
	}
}

func TestAddDuplicateName(t *testing.T) {
	r := scheduler.NewRegistry()
	action := func(context.Context) error { return nil }
	if err := r.Add("dup", nil, action); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	err := r.Add("dup", nil, action)
	if !errors.Is(err, scheduler.ErrDuplicateName) {
		t.Fatalf("Add(dup) = %v, want ErrDuplicateName", err)
	}
}

func TestAddNilConditionSucceeds(t *testing.T) {
	r := scheduler.NewRegistry()
	called := false
	action := func(context.Context) error { called = true; return nil }
	if err := r.Add("always", nil, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Fire(context.Background(), "always"); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !called {
		t.Fatal("Action was not called for nil Condition entry")
	}
}

func TestAddFullyPopulated(t *testing.T) {
	r := scheduler.NewRegistry()
	cond := func(context.Context) (bool, error) { return true, nil }
	action := func(context.Context) error { return nil }
	if err := r.Add("full", cond, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

// TestZeroValueRegistryIsReady pins that a zero-value Registry, built
// without New, accepts Add and Fire the same way New's result does.
func TestZeroValueRegistryIsReady(t *testing.T) {
	var r scheduler.Registry
	called := false
	action := func(context.Context) error { called = true; return nil }
	if err := r.Add("zero", nil, action); err != nil {
		t.Fatalf("Add on zero-value Registry: %v", err)
	}
	if err := r.Fire(context.Background(), "zero"); err != nil {
		t.Fatalf("Fire on zero-value Registry: %v", err)
	}
	if !called {
		t.Fatal("Action was not called on a zero-value Registry")
	}
}

func TestTriggerRemove(t *testing.T) {
	r := scheduler.NewRegistry()
	action := func(context.Context) error { return nil }
	if err := r.Add("present", nil, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if ok := r.Remove("present"); !ok {
		t.Fatal("Remove(present) = false, want true")
	}
	if ok := r.Remove("present"); ok {
		t.Fatal("Remove(already removed) = true, want false")
	}
	if ok := r.Remove("never-added"); ok {
		t.Fatal("Remove(absent) = true, want false")
	}
}
