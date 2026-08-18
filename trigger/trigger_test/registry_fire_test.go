package trigger_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/trigger"
)

func TestFireUnknownName(t *testing.T) {
	r := trigger.New()
	err := r.Fire(context.Background(), "missing")
	if !errors.Is(err, trigger.ErrUnknownName) {
		t.Fatalf("Fire(unknown) = %v, want ErrUnknownName", err)
	}
}

func TestFireNilConditionAlwaysCallsAction(t *testing.T) {
	r := trigger.New()
	sentinel := errors.New("action failed")
	calls := 0
	action := func(context.Context) error { calls++; return sentinel }
	if err := r.Add("nil-cond", nil, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := r.Fire(context.Background(), "nil-cond")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Fire = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Fatalf("Action called %d times, want 1", calls)
	}
}

func TestFireConditionFalse(t *testing.T) {
	r := trigger.New()
	calls := 0
	cond := func(context.Context) (bool, error) { return false, nil }
	action := func(context.Context) error { calls++; return nil }
	if err := r.Add("not-ready", cond, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := r.Fire(context.Background(), "not-ready")
	if !errors.Is(err, trigger.ErrConditionNotMet) {
		t.Fatalf("Fire = %v, want ErrConditionNotMet", err)
	}
	if calls != 0 {
		t.Fatalf("Action called %d times, want 0", calls)
	}
}

func TestFireConditionError(t *testing.T) {
	r := trigger.New()
	sentinel := errors.New("condition broke")
	calls := 0
	cond := func(context.Context) (bool, error) { return false, sentinel }
	action := func(context.Context) error { calls++; return nil }
	if err := r.Add("broken-cond", cond, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := r.Fire(context.Background(), "broken-cond")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Fire = %v, want wrapped sentinel", err)
	}
	if !strings.Contains(err.Error(), `"broken-cond"`) {
		t.Fatalf("Fire error %q does not mention the name", err.Error())
	}
	if calls != 0 {
		t.Fatalf("Action called %d times, want 0", calls)
	}
}

func TestFireConditionTrueCallsActionOnce(t *testing.T) {
	r := trigger.New()
	sentinel := errors.New("action result")
	calls := 0
	cond := func(context.Context) (bool, error) { return true, nil }
	action := func(context.Context) error { calls++; return sentinel }
	if err := r.Add("ready", cond, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := r.Fire(context.Background(), "ready")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Fire = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Fatalf("Action called %d times, want 1", calls)
	}
}

func TestFireConditionTrueActionSuccess(t *testing.T) {
	r := trigger.New()
	calls := 0
	cond := func(context.Context) (bool, error) { return true, nil }
	action := func(context.Context) error { calls++; return nil }
	if err := r.Add("ready-ok", cond, action); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Fire(context.Background(), "ready-ok"); err != nil {
		t.Fatalf("Fire = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("Action called %d times, want 1", calls)
	}
}
