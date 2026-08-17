package flow

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestFirstByDeclarationPicksDeclaredFirstMember proves
// firstByDeclaration selects the group member listed first in group,
// by ID, not the member listed last. The record's Output carries
// each entry's own step ID, so a wrong pick is visible directly, with
// no goroutine race and no arrival-order marker involved. A build
// that swaps group[0] for group[len(group)-1] fails this assertion.
func TestFirstByDeclarationPicksDeclaredFirstMember(t *testing.T) {
	group := []Step{{ID: "a", To: "target"}, {ID: "b", To: "target"}}
	byID := map[string]waveResult{
		"a": {step: group[0], rec: machine.InOut{Output: "a"}},
		"b": {step: group[1], rec: machine.InOut{Output: "b"}},
	}
	got := firstByDeclaration(byID, group)
	if got.rec.Output != "a" {
		t.Fatalf("firstByDeclaration.rec.Output = %v, want %q (group[0]'s own ID)", got.rec.Output, "a")
	}
}

// TestFirstByDeclarationIgnoresMapOrder proves the pick does not
// depend on byID's own iteration order: reversing the group slice
// while keeping byID fixed must select the other entry.
func TestFirstByDeclarationIgnoresMapOrder(t *testing.T) {
	group := []Step{{ID: "a", To: "target"}, {ID: "b", To: "target"}}
	byID := map[string]waveResult{
		"a": {step: group[0], rec: machine.InOut{Output: "a"}},
		"b": {step: group[1], rec: machine.InOut{Output: "b"}},
	}
	reversed := []Step{group[1], group[0]}
	got := firstByDeclaration(byID, reversed)
	if got.rec.Output != "b" {
		t.Fatalf("firstByDeclaration.rec.Output = %v, want %q (reversed group[0]'s own ID)", got.rec.Output, "b")
	}
}
