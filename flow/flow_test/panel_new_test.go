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

// panelOverlapNewCases lists the panel shapes for the cross-panel
// overlap test. want is the pinned error text. An empty want expects
// New to accept the shape.
var panelOverlapNewCases = []struct {
	name   string
	steps  []flow.Step
	panels []flow.Panel
	want   string
}{
	{
		name: "two panels share one member",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
		},
		panels: []flow.Panel{{"a"}, {"a", "b"}},
		want:   `flow: step "a" is named in panels 0 and 1`,
	},
	{
		name: "two panels share a middle step",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
			{ID: "c", To: "x"},
		},
		panels: []flow.Panel{{"a", "b"}, {"b", "c"}},
		want:   `flow: step "b" is named in panels 0 and 1`,
	},
	{
		name: "one panel duplicated",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
		},
		panels: []flow.Panel{{"a", "b"}, {"a", "b"}},
		want:   `flow: step "a" is named in panels 0 and 1`,
	},
	{
		name: "panels apart report both indexes",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
		},
		panels: []flow.Panel{{"a"}, {"b"}, {"a"}},
		want:   `flow: step "a" is named in panels 0 and 2`,
	},
	{
		name: "later panel scanned in member order",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
		},
		panels: []flow.Panel{{"a", "b"}, {"b", "a"}},
		want:   `flow: step "b" is named in panels 0 and 1`,
	},
	{
		name: "three panels report the first repeat",
		steps: []flow.Step{
			{ID: "a", To: "x"},
		},
		panels: []flow.Panel{{"a"}, {"a"}, {"a"}},
		want:   `flow: step "a" is named in panels 0 and 1`,
	},
	{
		name: "unknown step beats the overlap scan",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
		},
		panels: []flow.Panel{{"a"}, {"a", "nope"}},
		want:   `flow: panel 1 names unknown step "nope"`,
	},
	{
		name: "panels with no shared member accept",
		steps: []flow.Step{
			{ID: "a", To: "x"},
			{ID: "b", To: "x"},
			{ID: "c", To: "x"},
		},
		panels: []flow.Panel{{"a", "b"}, {"c"}},
		want:   "",
	},
}

// TestNewPanelStepNamedInTwoPanels proves New rejects a step ID named
// by two panels, with the pinned message. The per-panel checks run
// before the overlap scan: a panel with both a repeat and an unknown
// step reports the unknown step.
func TestNewPanelStepNamedInTwoPanels(t *testing.T) {
	t.Parallel()
	for _, tc := range panelOverlapNewCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := flow.New(tc.steps, tc.panels)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("New: got error %q, want nil", err.Error())
				}
				if d == nil {
					t.Fatal("New: got nil definition, want non-nil")
				}
				return
			}
			if err == nil {
				t.Fatalf("New: got nil error, want %q", tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("New: error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}
