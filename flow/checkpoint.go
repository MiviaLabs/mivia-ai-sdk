package flow

import (
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Checkpoint is the full resumable state of a Run: the current
// machine.Status, the current machine.InOut record, the sorted step
// IDs of every step that resolved OutcomeSucceeded so far, and the
// sorted step IDs of every step that resolved OutcomeSkipped so far.
// Done's and Skipped's order is a sort, not a completion order: two
// steps that complete in one order can appear in the opposite order,
// if their IDs sort the other way. A route exclusion (applyRoute) or
// an admission skip (nextReadyGroup) is final regardless of the
// excluding step's later outcome; Skipped preserves that decision
// across a pause and a Resume the same way Done preserves a success.
// Run aborts on the first OutcomeFailed step, so Checkpoint never
// needs to record a failure.
type Checkpoint struct {
	Status  machine.Status
	Record  machine.InOut
	Done    []string
	Skipped []string
}

// Validate rejects an empty Status and a step ID named in both Done
// and Skipped: a step cannot have resolved both OutcomeSucceeded and
// OutcomeSkipped. Encode and Decode both call it.
func (c Checkpoint) Validate() error {
	if c.Status == machine.Status("") {
		return errorf("checkpoint: status must not be empty")
	}
	skipped := make(map[string]bool, len(c.Skipped))
	for _, id := range c.Skipped {
		skipped[id] = true
	}
	for _, id := range c.Done {
		if skipped[id] {
			return errorf("checkpoint: step %q named in both Done and Skipped", id)
		}
	}
	return nil
}

// doneFrom returns the lexicographically sorted step IDs of every
// OutcomeSucceeded entry in outcomes.
func doneFrom(outcomes map[string]Outcome) []string {
	return idsWithOutcome(outcomes, OutcomeSucceeded)
}

// skippedFrom returns the lexicographically sorted step IDs of every
// OutcomeSkipped entry in outcomes.
func skippedFrom(outcomes map[string]Outcome) []string {
	return idsWithOutcome(outcomes, OutcomeSkipped)
}

// idsWithOutcome returns the lexicographically sorted step IDs whose
// outcome in outcomes equals want.
func idsWithOutcome(outcomes map[string]Outcome, want Outcome) []string {
	ids := make([]string, 0, len(outcomes))
	for id, o := range outcomes {
		if o == want {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
