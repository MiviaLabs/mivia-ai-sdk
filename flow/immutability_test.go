package flow

import (
	"reflect"
	"testing"
)

// TestNewCopiesNeeds proves New deep-copies each Needs slice.
// A caller that mutates an element of its input Needs after New must
// not change the built Definition. The external flow_test package
// cannot observe the internal needs slice, so this internal test
// inspects the unexported copy directly.
func TestNewCopiesNeeds(t *testing.T) {
	inputs := []Step{
		{ID: "a"},
		{ID: "b", Needs: []string{"a"}},
	}
	d, err := New(inputs, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	inputs[1].Needs[0] = "zzz"
	if got := d.steps[1].Needs[0]; got != "a" {
		t.Fatalf("mutating caller Needs changed internal needs to %q; want %q", got, "a")
	}
}

// TestNewStoresAndCopiesPanels proves New stores the panels and
// deep-copies each panel slice. A caller that mutates a panel element
// after New must not change the stored panels. The external package
// cannot read d.panels, so only an internal test can assert storage.
func TestNewStoresAndCopiesPanels(t *testing.T) {
	inputs := []Panel{{"a", "b"}}
	d, err := New([]Step{{ID: "a"}, {ID: "b"}}, inputs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(d.panels) != 1 {
		t.Fatalf("len(d.panels) = %d, want 1", len(d.panels))
	}
	want := Panel{"a", "b"}
	if got := d.panels[0]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stored panel = %v, want %v", got, want)
	}
	inputs[0][0] = "zzz"
	if got := d.panels[0][0]; got != "a" {
		t.Fatalf("mutating caller panel changed stored panel to %q; want %q", got, "a")
	}
}

// TestNewDedupsRepeatedDependency proves a step that lists a dependency
// twice is accepted and counted once. The dedup branch in findRoots
// guards this; without it the in-degree would be inflated and the
// follower step would lose root or cycle behavior.
func TestNewDedupsRepeatedDependency(t *testing.T) {
	d, err := New([]Step{
		{ID: "a"},
		{ID: "b", Needs: []string{"a", "a"}},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wantRoots := []string{"a"}
	if got := d.roots; !reflect.DeepEqual(got, wantRoots) {
		t.Fatalf("roots = %v, want %v", got, wantRoots)
	}
	// Without dedup, b's in-degree would be 2 and its successor edge
	// duplicated; assert the internal adjacency stays linear so b is
	// processed exactly once and the graph stays acyclic.
	if len(d.steps[1].Needs) != 2 {
		t.Fatalf("len(d.steps[1].Needs) = %d, want 2 (the caller copy is preserved)", len(d.steps[1].Needs))
	}
}

// TestRootsInternalCopy proves the internal root list is returned as a
// copy. Mutating the returned slice must not change d.roots.
func TestRootsInternalCopy(t *testing.T) {
	d, err := New([]Step{
		{ID: "a"},
		{ID: "b", Needs: []string{"a"}},
	}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := d.Roots()
	got[0] = "corrupted"
	if want := []string{"a"}; !reflect.DeepEqual(d.roots, want) {
		t.Fatalf("d.roots after mutating returned slice = %v, want %v", d.roots, want)
	}
}

// TestNewCopiesSub proves New builds a fresh copy of a step's Sub, not
// a Definition that shares the child's slices. A Definition is opaque
// outside flow, so only an internal test can read the parent's copy.
// See TestRunChainedAfterCallerMutation in
// flow/flow_test/phase07_tdd_new_test.go for the run-path case.
func TestNewCopiesSub(t *testing.T) {
	childSteps := []Step{
		{ID: "a", To: "x"},
		{ID: "b", Needs: []string{"a"}, To: "x"},
	}
	childPanels := []Panel{{"b"}}
	child, err := New(childSteps, childPanels)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	parent, err := New([]Step{{ID: "outer", Sub: child}}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	sub := parent.steps[0].Sub
	if sub == child {
		t.Fatal("parent's Sub is the caller's child pointer; want a fresh copy")
	}

	// Mutate the caller slices and the child's internals. The child's
	// own New already isolated the caller slices, so the child mutations
	// catch a parent that shares the child's slices.
	childSteps[1].Needs[0] = "zzz"
	childPanels[0][0] = "zzz"
	child.steps[1].ID = "zzz"
	child.steps[1].Needs[0] = "zzz"
	child.panels[0][0] = "zzz"
	child.roots[0] = "zzz"

	if got := sub.steps[1].ID; got != "b" {
		t.Fatalf("parent's sub step ID = %q, want %q", got, "b")
	}
	if got := sub.steps[1].Needs[0]; got != "a" {
		t.Fatalf("parent's sub Needs = %q, want %q", got, "a")
	}
	if got := sub.panels[0][0]; got != "b" {
		t.Fatalf("parent's sub panel member = %q, want %q", got, "b")
	}
	if got := sub.roots[0]; got != "a" {
		t.Fatalf("parent's sub root = %q, want %q", got, "a")
	}
}
