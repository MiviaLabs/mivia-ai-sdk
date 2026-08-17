package flow

import "testing"

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
