package flow

import (
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Checkpoint is the full resumable state of a Run: the current
// machine.Status, the current machine.InOut record, and the sorted
// step IDs of every step that resolved OutcomeSucceeded so far.
// Done's order is a sort, not a completion order: two steps that
// complete in one order can appear in Done in the opposite order, if
// their IDs sort the other way. Done lists only step IDs that
// resolved OutcomeSucceeded; Run aborts on the first OutcomeFailed
// step, and no code path yet produces OutcomeSkipped.
type Checkpoint struct {
	Status machine.Status
	Record machine.InOut
	Done   []string
}

// Validate rejects an empty Status. Encode and Decode both call it.
func (c Checkpoint) Validate() error {
	if c.Status == machine.Status("") {
		return errorf("checkpoint: status must not be empty")
	}
	return nil
}

// doneFrom returns the lexicographically sorted step IDs of every
// OutcomeSucceeded entry in outcomes.
func doneFrom(outcomes map[string]Outcome) []string {
	done := make([]string, 0, len(outcomes))
	for id, o := range outcomes {
		if o == OutcomeSucceeded {
			done = append(done, id)
		}
	}
	sort.Strings(done)
	return done
}
