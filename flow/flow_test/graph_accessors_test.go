package flow_test

import (
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// TestStepsCopiesNeeds proves mutating a returned step's Needs slice
// never reaches the stored definition.
func TestStepsCopiesNeeds(t *testing.T) {
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "sa"},
		{ID: "b", To: "sb", Needs: []string{"a"}},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := d.Steps()
	got[1].Needs[0] = "mutated"
	if fresh := d.Steps(); fresh[1].Needs[0] != "a" {
		t.Fatalf("stored Needs = %q, want %q", fresh[1].Needs[0], "a")
	}
}

// TestStepsCopiesRetryAndLoop proves mutating a returned step's policy
// pointers never reaches the stored definition.
func TestStepsCopiesRetryAndLoop(t *testing.T) {
	child, err := flow.New([]flow.Step{{ID: "i", To: "done"}}, nil)
	if err != nil {
		t.Fatalf("New child: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "sa", Retry: &flow.RetryPolicy{MaxAttempts: 2, MaxDelay: time.Second}},
		{ID: "b", To: "sb", Sub: child, Loop: &flow.LoopPolicy{Max: 3}},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := d.Steps()
	got[0].Retry.MaxAttempts = 9
	got[1].Loop.Max = 9
	fresh := d.Steps()
	if fresh[0].Retry.MaxAttempts != 2 {
		t.Fatalf("stored Retry.MaxAttempts = %d, want 2", fresh[0].Retry.MaxAttempts)
	}
	if fresh[1].Loop.Max != 3 {
		t.Fatalf("stored Loop.Max = %d, want 3", fresh[1].Loop.Max)
	}
}

// TestStepsCopiesSubGraph proves mutating a returned copy's Sub child,
// at any depth, never reaches the stored definition. The probe mutates
// a Needs element, the shared backing array a shallow copy would leak.
func TestStepsCopiesSubGraph(t *testing.T) {
	inner, err := flow.New([]flow.Step{
		{ID: "i1", To: "deep"},
		{ID: "i2", To: "leaf", Needs: []string{"i1"}},
	}, nil)
	if err != nil {
		t.Fatalf("New inner: %v", err)
	}
	outer, err := flow.New([]flow.Step{{ID: "o1", Sub: inner, To: "oo"}}, nil)
	if err != nil {
		t.Fatalf("New outer: %v", err)
	}
	d, err := flow.New([]flow.Step{{ID: "root", Sub: outer, To: "rr"}}, nil)
	if err != nil {
		t.Fatalf("New root: %v", err)
	}
	got := d.Steps()
	got[0].Sub.Steps()[0].Sub.Steps()[1].Needs[0] = "mutated"
	fresh := d.Steps()
	if got := fresh[0].Sub.Steps()[0].Sub.Steps()[1].Needs[0]; got != "i1" {
		t.Fatalf("stored nested Needs = %q, want %q", got, "i1")
	}
}

// TestPanelsCopyMembers proves mutating a returned panel's member slice
// never reaches the stored definition.
func TestPanelsCopyMembers(t *testing.T) {
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "w"},
		{ID: "b", To: "w"},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := d.Panels()
	got[0][1] = "mutated"
	if fresh := d.Panels(); fresh[0][1] != "b" {
		t.Fatalf("stored panel member = %q, want %q", fresh[0][1], "b")
	}
}
