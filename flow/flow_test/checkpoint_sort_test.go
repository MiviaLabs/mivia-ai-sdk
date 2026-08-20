package flow_test

// Six single-assertion cases pinning Checkpoint.Validate's sortedness
// check. Kept out of checkpoint_test.go, which sits at 404 of the
// 500-line structure cap; see docs/plans/flow.md's checkpoint
// correctness fix for the line-count reasoning.

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestCheckpointValidateRejectsUnsortedDone fails against today's code,
// which never checks Done's order.
func TestCheckpointValidateRejectsUnsortedDone(t *testing.T) {
	c := flow.Checkpoint{Status: machine.Status("s"), Done: []string{"b", "a"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "flow: checkpoint: Done is not sorted") {
		t.Fatalf("Validate() = %v, want an error matching %q", err, "flow: checkpoint: Done is not sorted")
	}
}

// TestCheckpointValidateRejectsUnsortedSkipped kills a mutation that
// checks only the first group; Skipped is a separate loop iteration.
func TestCheckpointValidateRejectsUnsortedSkipped(t *testing.T) {
	c := flow.Checkpoint{Status: machine.Status("s"), Skipped: []string{"b", "a"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "flow: checkpoint: Skipped is not sorted") {
		t.Fatalf("Validate() = %v, want an error matching %q", err, "flow: checkpoint: Skipped is not sorted")
	}
}

// TestCheckpointValidateRejectsUnsortedFailed kills a mutation that
// skips the last group's check.
func TestCheckpointValidateRejectsUnsortedFailed(t *testing.T) {
	c := flow.Checkpoint{Status: machine.Status("s"), Failed: []string{"b", "a"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "flow: checkpoint: Failed is not sorted") {
		t.Fatalf("Validate() = %v, want an error matching %q", err, "flow: checkpoint: Failed is not sorted")
	}
}

// TestCheckpointValidateAcceptsSortedLists is a positive control: the
// common, already-sorted case still passes.
func TestCheckpointValidateAcceptsSortedLists(t *testing.T) {
	c := flow.Checkpoint{
		Status:  machine.Status("s"),
		Done:    []string{"a", "b"},
		Skipped: []string{"c"},
		Failed:  []string{"d"},
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestCheckpointValidateAcceptsEmptyLists pins that
// sort.StringsAreSorted(nil) is true, so the zero-list case Run's
// single-step short-circuit produces still passes.
func TestCheckpointValidateAcceptsEmptyLists(t *testing.T) {
	c := flow.Checkpoint{Status: machine.Status("s")}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestCheckpointDecodeRejectsUnsortedDone proves the untrusted entry
// point, not only Validate directly, rejects the shape.
func TestCheckpointDecodeRejectsUnsortedDone(t *testing.T) {
	data := []byte(`{"Status":"s","Done":["b","a"]}`)
	if _, err := flow.Decode(data); err == nil {
		t.Fatal("Decode() = nil error, want an error for unsorted Done")
	}
}
