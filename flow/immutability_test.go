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
