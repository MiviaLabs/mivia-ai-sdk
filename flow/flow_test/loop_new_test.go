package flow_test

// The LoopPolicy.Validate and New-validation cases live here, to keep
// loop_test.go at or below the 500-line structure cap. See
// loop_test.go for the shared loopMachine and loopChild fixtures.

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// TestLoopPolicyValidateRejectsNegativeMax pins the exact message for
// a LoopPolicy with a negative Max.
func TestLoopPolicyValidateRejectsNegativeMax(t *testing.T) {
	t.Parallel()
	p := flow.LoopPolicy{Max: -1}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := "flow: loop: max must be at least 0"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestLoopPolicyValidateAcceptsZeroAndOne proves Validate accepts Max
// zero (unbounded) and Max one.
func TestLoopPolicyValidateAcceptsZeroAndOne(t *testing.T) {
	t.Parallel()
	for _, max := range []int{0, 1} {
		if err := (flow.LoopPolicy{Max: max}).Validate(); err != nil {
			t.Fatalf("Validate() for Max %d = %v, want nil", max, err)
		}
	}
}

// TestNewRejectsLoopWithNilSub pins the exact message for a Loop
// policy on a step with a nil Sub.
func TestNewRejectsLoopWithNilSub(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Loop: &flow.LoopPolicy{}},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "a" has a loop policy but no sub-workflow`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsLoopedPanelMember pins the exact message for a Loop
// policy declared on a panel member.
func TestNewRejectsLoopedPanelMember(t *testing.T) {
	t.Parallel()
	sub, err := flow.New([]flow.Step{{ID: "inner"}}, nil)
	if err != nil {
		t.Fatalf("flow.New(sub): %v", err)
	}
	_, err = flow.New([]flow.Step{
		{ID: "a", To: "done", Sub: sub, Loop: &flow.LoopPolicy{}},
		{ID: "b", To: "done"},
	}, []flow.Panel{{"a", "b"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names looped step "a"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsLoopWithNegativeMax pins the exact step-scoped message
// for a LoopPolicy with a negative Max, proving New enforces
// LoopPolicy.Validate's rule itself.
func TestNewRejectsLoopWithNegativeMax(t *testing.T) {
	t.Parallel()
	sub, err := flow.New([]flow.Step{{ID: "inner"}}, nil)
	if err != nil {
		t.Fatalf("flow.New(sub): %v", err)
	}
	_, err = flow.New([]flow.Step{
		{ID: "a", To: "done", Sub: sub, Loop: &flow.LoopPolicy{Max: -1}},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "a" loop: max must be at least 0`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewAcceptsLoopedStepNoSubNoPanel proves New accepts a looped
// step with a non-nil Sub and no panel.
func TestNewAcceptsLoopedStepNoSubNoPanel(t *testing.T) {
	t.Parallel()
	sub, err := flow.New([]flow.Step{{ID: "inner"}}, nil)
	if err != nil {
		t.Fatalf("flow.New(sub): %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "done", Sub: sub, Loop: &flow.LoopPolicy{}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}
