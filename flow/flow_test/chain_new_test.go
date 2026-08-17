package flow_test

// Red step: before phase 7, Step had no Sub field, copySteps did not
// recurse, and New did not check nesting depth or panel chains.
// Adding Sub to Step, recursive copySteps, validatePanelChains, and
// validateDepth made the cases below pass.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// chainStatus returns a deterministic target status for the leaf of a
// depth chain. Status names stay within a single character to keep
// machine transitions small.
func chainStatus(depth int) string {
	return string(rune('a' + depth))
}

// chainSteps builds a linear chain of nested Sub definitions of the
// requested depth. Depth zero returns a single step with no Sub.
func chainSteps(depth int) []flow.Step {
	leaf := flow.Step{ID: "s0"}
	if depth > 0 {
		leaf.To = chainStatus(depth)
	}
	if depth == 0 {
		return []flow.Step{leaf}
	}
	parent := leaf
	for d := 1; d <= depth; d++ {
		childDef, _ := flow.New([]flow.Step{parent}, nil)
		id := "s" + string(rune('0'+d))
		parent = flow.Step{ID: id, Sub: childDef}
		if d < depth {
			parent.To = chainStatus(depth - d)
		}
	}
	return []flow.Step{parent}
}

// TestNewAcceptsNilSub proves New accepts a step whose Sub is nil.
func TestNewAcceptsNilSub(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{{ID: "a", To: "x"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}

// TestNewAcceptsValidSub proves New accepts a step with a valid,
// non-nil Sub definition.
func TestNewAcceptsValidSub(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{{ID: "inner", To: "x"}}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	d, err := flow.New([]flow.Step{{ID: "outer", Sub: child}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}

// TestNewRejectsSubDepthAboveEight proves New rejects a Sub chain
// deeper than eight levels.
func TestNewRejectsSubDepthAboveEight(t *testing.T) {
	t.Parallel()
	steps := chainSteps(9)
	_, err := flow.New(steps, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "nesting depth exceeds 8") {
		t.Fatalf("error %q should report depth exceeds 8", err.Error())
	}
}

// TestNewAcceptsSubDepthExactlyEight proves New accepts a Sub chain
// of exactly eight levels.
func TestNewAcceptsSubDepthExactlyEight(t *testing.T) {
	t.Parallel()
	steps := chainSteps(8)
	d, err := flow.New(steps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}

// TestNewRejectsChainedStepInMultiMemberPanel proves New rejects a
// panel of two or more members when any member has a non-nil Sub.
func TestNewRejectsChainedStepInMultiMemberPanel(t *testing.T) {
	t.Parallel()
	child, _ := flow.New([]flow.Step{{ID: "inner", To: "x"}}, nil)
	_, err := flow.New([]flow.Step{
		{ID: "a", Sub: child, To: "x"},
		{ID: "b", To: "x"},
	}, []flow.Panel{{"a", "b"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "chained and may not share a panel") {
		t.Fatalf("error %q should report chained panel restriction", err.Error())
	}
}

// TestNewAcceptsChainedStepAsSolePanelMember proves New accepts a
// chained step as the only member of a one-member panel.
func TestNewAcceptsChainedStepAsSolePanelMember(t *testing.T) {
	t.Parallel()
	child, _ := flow.New([]flow.Step{{ID: "inner", To: "x"}}, nil)
	d, err := flow.New([]flow.Step{
		{ID: "a", Sub: child},
	}, []flow.Panel{{"a"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}

// TestRunChainedAfterCallerMutation proves a chained parent keeps
// running after the caller mutates the slices it passed to the child's
// New. The child's own New copy isolates those slices; this test does
// not prove the parent's re-copy of Sub. TestNewCopiesSub in
// flow/immutability_test.go proves the parent's copy.
func TestRunChainedAfterCallerMutation(t *testing.T) {
	t.Parallel()
	childSteps := []flow.Step{
		{ID: "a", To: "x"},
		{ID: "b", To: "x"},
	}
	childPanels := []flow.Panel{{"a", "b"}}
	child, err := flow.New(childSteps, childPanels)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	parent, err := flow.New([]flow.Step{{ID: "outer", Sub: child}}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}

	// The mutations would fail a fresh New: a Needs cycle and a panel
	// that names an unknown step. The built parent must still run.
	childSteps[0].Needs = []string{"b"}
	childSteps[1].Needs = []string{"a"}
	childPanels[0][0] = "tampered"

	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("x"), Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	status, _, err := flow.Run(context.Background(), parent, m, machine.InOut{}, noopConfirm, nil)
	if err != nil {
		t.Fatalf("parent Run after caller mutation: %v", err)
	}
	if status != machine.Status("x") {
		t.Fatalf("status = %q, want %q", status, machine.Status("x"))
	}
}

// contains reports whether s contains substr. It avoids importing
// strings for this file when only this helper needs it.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
