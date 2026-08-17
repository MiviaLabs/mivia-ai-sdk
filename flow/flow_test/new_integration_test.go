package flow_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
)

// TestDiamondRoots builds a diamond graph and proves the roots.
// The roots are the steps with no Needs, in declaration order.
func TestDiamondRoots(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "start"},
		{ID: "left", Needs: []string{"start"}},
		{ID: "right", Needs: []string{"start"}},
		{ID: "join", Needs: []string{"left", "right"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"start"}
	if got := d.Roots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Roots() = %v, want %v", got, want)
	}
}

// TestLinearRoots builds a linear chain and proves only the head is a
// root, even when a later step also has cross edges.
func TestLinearRoots(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a"},
		{ID: "b", Needs: []string{"a"}},
		{ID: "c", Needs: []string{"b"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a"}
	if got := d.Roots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Roots() = %v, want %v", got, want)
	}
}

// TestMultipleRoots proves several independent roots survive together.
func TestMultipleRoots(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "r1"},
		{ID: "r2"},
		{ID: "m", Needs: []string{"r1", "r2"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"r1", "r2"}
	if got := d.Roots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Roots() = %v, want %v", got, want)
	}
}

// TestCycleRejected feeds a diamond with a back edge and confirms New
// rejects the cycle.
func TestCycleRejected(t *testing.T) {
	t.Parallel()
	// loop -> join -> right -> start -> loop closes the graph.
	d, err := flow.New([]flow.Step{
		{ID: "start", Needs: []string{"loop"}},
		{ID: "left", Needs: []string{"start"}},
		{ID: "right", Needs: []string{"start"}},
		{ID: "join", Needs: []string{"left", "right"}},
		{ID: "loop", Needs: []string{"join"}},
	}, nil)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("error %q should mention cycle detected", err.Error())
	}
	if d != nil {
		t.Fatal("New must return nil definition on cycle")
	}
}

// TestPanelsKnownSteps proves panels compose with the graph and stay
// accepted when every entry names a real step.
func TestPanelsKnownSteps(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "start"},
		{ID: "left", Needs: []string{"start"}},
		{ID: "right", Needs: []string{"start"}},
		{ID: "join", Needs: []string{"left", "right"}},
	}, []flow.Panel{
		{"left", "right"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("New returned nil definition")
	}
}

// TestPanelsUnknownStepRejected proves a panel entry for a missing step
// fails New even when the rest of the graph is valid.
func TestPanelsUnknownStepRejected(t *testing.T) {
	t.Parallel()
	_, err := flow.New([]flow.Step{
		{ID: "start"},
		{ID: "left", Needs: []string{"start"}},
	}, []flow.Panel{
		{"ghost"},
	})
	if err == nil {
		t.Fatal("expected unknown-step error, got nil")
	}
	if !strings.Contains(err.Error(), "panel ") {
		t.Fatalf("error %q should mention the panel source", err.Error())
	}
}

// TestRootsReturnsCopy proves a caller cannot corrupt the definition by
// mutating the slice that Roots returns. The returned slice must be a
// copy, not the internal root list.
func TestRootsReturnsCopy(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a"},
		{ID: "b", Needs: []string{"a"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a"}
	got := d.Roots()
	got[0] = "corrupted"
	if r := d.Roots(); !reflect.DeepEqual(r, want) {
		t.Fatalf("Roots() after mutating returned slice = %v, want %v", r, want)
	}
}
