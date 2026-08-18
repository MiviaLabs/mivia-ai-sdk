package flow

import (
	"sort"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Checkpoint is the full resumable state of a Run: the current
// machine.Status, the current machine.InOut record, the sorted step
// IDs of every step that resolved OutcomeSucceeded so far, the sorted
// step IDs of every step that resolved OutcomeSkipped so far, and the
// sorted step IDs of every step that resolved OutcomeFailed so far,
// whether or not a fallback caught that failure. Done's, Skipped's,
// and Failed's order is a sort, not a completion order: two steps
// that complete in one order can appear in the opposite order, if
// their IDs sort the other way. A route exclusion (applyRoute) or an
// admission skip (nextReadyGroup) is final regardless of the
// excluding step's later outcome; Skipped preserves that decision
// across a pause and a Resume the same way Done preserves a success.
//
// Failed preserves only the resolved outcome of an already-caught
// failure; Resume does not restore the fallback bookkeeping a still-
// pending handler needs. A fallback step that resolves after a
// Resume still runs, admitted by AdmissionOnFailed the same way it
// would without a pause, but FailureFrom returns false inside it: the
// Failure a pre-pause fallback would have read does not survive the
// round trip. A Route exclusion that would have emptied a failure's
// last pending handler set, and so aborted the run with the recorded
// step error, instead resolves as an ordinary skip after a Resume; no
// error carries the lost failure across the boundary. Run never
// checkpoints this loss silently: see Run's doc comment.
type Checkpoint struct {
	Status  machine.Status
	Record  machine.InOut
	Done    []string
	Skipped []string
	Failed  []string
}

// Validate rejects an empty Status and a step ID named in more than
// one of Done, Skipped, and Failed: a step cannot resolve to two
// different outcomes. Encode and Decode both call it.
func (c Checkpoint) Validate() error {
	if c.Status == machine.Status("") {
		return errorf("checkpoint: status must not be empty")
	}
	seen := make(map[string]string, len(c.Done)+len(c.Skipped)+len(c.Failed))
	groups := []struct {
		name string
		ids  []string
	}{
		{"Done", c.Done},
		{"Skipped", c.Skipped},
		{"Failed", c.Failed},
	}
	for _, g := range groups {
		for _, id := range g.ids {
			if prior, ok := seen[id]; ok {
				return errorf("checkpoint: step %q named in both %s and %s", id, prior, g.name)
			}
			seen[id] = g.name
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

// failedFrom returns the lexicographically sorted step IDs of every
// OutcomeFailed entry in outcomes.
func failedFrom(outcomes map[string]Outcome) []string {
	return idsWithOutcome(outcomes, OutcomeFailed)
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
