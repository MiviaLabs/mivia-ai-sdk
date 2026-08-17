package flow_test

// Red step: before phase 6, New accepted a panel whose members
// disagreed on To, a panel that named one step twice, and a panel
// whose members needed each other. `go test ./flow/...` passed those
// shapes with a nil error. validatePanels and validatePanelIndependence
// landed in flow/validate.go; the cases below then failed on the bad
// shapes and passed on the good ones.
//
// These New-validation cases split out of phase06_tdd_test.go to keep
// each test file at or below the 500-line structure cap. The
// Run-level phase 6 cases stay in phase06_tdd_test.go.

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// TestNewPanelDisagreeOnTo proves New rejects a panel whose members
// name a different To, with the pinned message.
func TestNewPanelDisagreeOnTo(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
		{ID: "b", To: "y"},
	}, []flow.Panel{{"a", "b"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0: step "a" and step "b" disagree on To ("x" vs "y")`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewPanelAgreeOnTo proves New accepts a panel whose members
// share one To.
func TestNewPanelAgreeOnTo(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
		{ID: "b", To: "x"},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil definition on valid input")
	}
}

// TestNewPanelDuplicateStepID proves New rejects a panel that names
// the same step ID twice, with the pinned message.
func TestNewPanelDuplicateStepID(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
	}, []flow.Panel{{"a", "a"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names step "a" twice`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewPanelUnknownAndDuplicateReportsUnknown proves the
// unknown-step check runs before the duplicate-ID check: a panel
// with both an unknown step and a duplicate returns the unknown-step
// message.
func TestNewPanelUnknownAndDuplicateReportsUnknown(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
	}, []flow.Panel{{"a", "a", "nope"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names unknown step "nope"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewPanelUnknownAndToDisagreementReportsUnknown proves the
// unknown-step check runs before the homogeneity check: a panel with
// both an unknown step and a To disagreement returns the unknown-step
// message.
func TestNewPanelUnknownAndToDisagreementReportsUnknown(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
		{ID: "b", To: "y"},
	}, []flow.Panel{{"a", "b", "nope"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names unknown step "nope"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewPanelIndependenceDirect proves New rejects a panel where one
// member directly needs a fellow member, with the pinned message.
func TestNewPanelIndependenceDirect(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
		{ID: "b", Needs: []string{"a"}, To: "x"},
	}, []flow.Panel{{"a", "b"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0: step "b" needs step "a", a member of the same panel`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewPanelIndependenceTransitive proves New rejects a panel where
// one member's Needs closure reaches a fellow member through a chain
// of dependencies, not only through a direct edge.
func TestNewPanelIndependenceTransitive(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "a", To: "x"},
		{ID: "mid", Needs: []string{"a"}, To: "y"},
		{ID: "b", Needs: []string{"mid"}, To: "x"},
	}, []flow.Panel{{"a", "b"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0: step "b" needs step "a", a member of the same panel`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
