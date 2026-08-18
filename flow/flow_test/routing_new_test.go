package flow_test

// Red step: before phase 22, Step had no When or Route field, and
// flow.Admission and flow.Route did not exist. This file did not
// compile: `go build ./flow/...` failed with "undefined: flow.Route"
// and "unknown field Route in struct literal". When, Route, Admission,
// and the four New validations landed in flow/step.go and
// flow/routing.go; the cases below then passed.
//
// These New-validation cases split out of routing_test.go, matching
// the package's convention of a dedicated *_new_test.go file (see
// chain_new_test.go, panel_new_test.go).

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// keepAll is a Route stand-in that keeps every direct dependent.
func keepAll(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
	return nil, nil
}

// TestNewRejectsRouteWithNoDependent proves New rejects a branch step
// that no step needs, with the pinned message.
func TestNewRejectsRouteWithNoDependent(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "branch", To: "x", Route: keepAll},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "branch" has a route but no dependent`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsRoutedStepInPanel proves New rejects a branch step
// named in a panel, with the pinned message.
func TestNewRejectsRoutedStepInPanel(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "branch", To: "x", Route: keepAll},
		{ID: "sibling", To: "x"},
		{ID: "dep", Needs: []string{"branch"}, To: "y"},
	}, []flow.Panel{{"branch", "sibling"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names routed step "branch"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsSubAndRoute proves New rejects a step with both Sub
// and Route non-nil, with the pinned message.
func TestNewRejectsSubAndRoute(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{{ID: "c", To: "z"}}, nil)
	if err != nil {
		t.Fatalf("flow.New(child): %v", err)
	}
	_, err = flow.New([]flow.Step{
		{ID: "branch", To: "x", Sub: child, Route: keepAll},
		{ID: "dep", Needs: []string{"branch"}, To: "y"},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "branch" has both Sub and Route`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsPanelNamingDirectDependentOfRoutedStep proves New
// rejects a panel that names a direct dependent of a branch step,
// with the pinned message.
func TestNewRejectsPanelNamingDirectDependentOfRoutedStep(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "branch", To: "x", Route: keepAll},
		{ID: "dep", Needs: []string{"branch"}, To: "y"},
		{ID: "other", To: "y"},
	}, []flow.Panel{{"dep", "other"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names step "dep", a direct dependent of routed step "branch"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewAcceptsBranchStepWithTwoDependents proves New accepts a
// branch step with two direct dependents.
func TestNewAcceptsBranchStepWithTwoDependents(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "x", Route: keepAll},
		{ID: "left", Needs: []string{"branch"}, To: "y"},
		{ID: "right", Needs: []string{"branch"}, To: "y"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}
