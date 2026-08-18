package flow_test

// The New-validation phase 23 cases split out of fallback_test.go,
// matching the package's convention of a dedicated *_new_test.go file
// (see chain_new_test.go, panel_new_test.go, routing_new_test.go), to
// keep each test file at or below the 500-line structure cap.

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// TestNewRejectsAdmissionOnFailedRoot proves New rejects an
// AdmissionOnFailed step with no needs, with the pinned message. A
// root always admits, so the rule would be dead weight.
func TestNewRejectsAdmissionOnFailedRoot(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "fallback", When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "fallback" admits on failure but needs nothing`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestNewRejectsAdmissionOnFailedPanelMember proves New rejects an
// AdmissionOnFailed step named in a panel, with the pinned message. A
// wave shares one ctx across every member, with no per-member home
// for the failure a fallback would catch.
func TestNewRejectsAdmissionOnFailedPanelMember(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "x"},
		{ID: "sibling", Needs: []string{"risky"}, To: "x"},
	}, []flow.Panel{{"fallback", "sibling"}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: panel 0 names failure-admitted step "fallback"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
