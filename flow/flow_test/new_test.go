package flow_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// flowCase holds one New validation case.
type flowCase struct {
	name    string
	steps   []flow.Step
	panels  []flow.Panel
	wantErr bool
	errSub  string
}

// newCases lists the New assertions for the step graph.
var newCases = []flowCase{
	{
		name: "accepts a single root",
		steps: []flow.Step{
			{ID: "a"},
		},
		wantErr: false,
	},
	{
		name: "accepts a linear chain",
		steps: []flow.Step{
			{ID: "a"},
			{ID: "b", Needs: []string{"a"}},
		},
		wantErr: false,
	},
	{
		name: "accepts panels that name known steps",
		steps: []flow.Step{
			{ID: "a", To: "done"},
			{ID: "b", To: "done"},
		},
		panels:  []flow.Panel{{"a", "b"}},
		wantErr: false,
	},
	{
		name: "rejects an empty step ID",
		steps: []flow.Step{
			{ID: ""},
		},
		wantErr: true,
		errSub:  "empty ID",
	},
	{
		name: "rejects a duplicate step ID",
		steps: []flow.Step{
			{ID: "a"},
			{ID: "a"},
		},
		wantErr: true,
		errSub:  "duplicate step ID",
	},
	{
		name: "rejects a missing dependency",
		steps: []flow.Step{
			{ID: "a"},
			{ID: "b", Needs: []string{"zzz"}},
		},
		wantErr: true,
		errSub:  "needs unknown step",
	},
	{
		name: "rejects a panel that names an unknown step",
		steps: []flow.Step{
			{ID: "a"},
		},
		panels:  []flow.Panel{{"nope"}},
		wantErr: true,
		errSub:  "panel ",
	},
	{
		name: "accepts a repeated dependency",
		steps: []flow.Step{
			{ID: "a"},
			{ID: "b", Needs: []string{"a", "a"}},
		},
		wantErr: false,
	},
	{
		name: "rejects a self cycle",
		steps: []flow.Step{
			{ID: "a", Needs: []string{"a"}},
		},
		wantErr: true,
		errSub:  "cycle detected",
	},
	{
		name: "rejects a two-step cycle",
		steps: []flow.Step{
			{ID: "a", Needs: []string{"b"}},
			{ID: "b", Needs: []string{"a"}},
		},
		wantErr: true,
		errSub:  "cycle detected",
	},
}

// TestNewTable drives the New validation cases.
func TestNewTable(t *testing.T) {
	t.Parallel()
	for _, tt := range newCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := flow.New(tt.steps, tt.panels)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d == nil {
				t.Fatal("New returned a nil definition on valid input")
			}
		})
	}
}

// TestNewRootsStable proves Roots are reported in declaration order.
func TestNewRootsStable(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "x"},
		{ID: "y"},
		{ID: "z", Needs: []string{"x"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := d.Roots()
	want := []string{"x", "y"}
	if len(got) != len(want) {
		t.Fatalf("len(Roots()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Roots()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Red step: New and Roots did not exist on an empty package, so
	// case did not compile. New and Roots added; the cases passed.
}
