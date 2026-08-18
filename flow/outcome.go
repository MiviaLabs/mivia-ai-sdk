package flow

import "github.com/MiviaLabs/mivia-ai-sdk/machine"

// Outcome is the terminal state of one step after Run resolves it.
type Outcome int

const (
	// OutcomeSucceeded means the step fired and its ack confirmed.
	OutcomeSucceeded Outcome = iota
	// OutcomeFailed means the step's Fire failed or its ack was
	// rejected.
	OutcomeFailed
	// OutcomeSkipped means admission or routing excluded the step.
	OutcomeSkipped
)

// Report is the result of one Run call: the final status, the final
// record, and every resolved step's Outcome.
type Report struct {
	status   machine.Status
	record   machine.InOut
	outcomes map[string]Outcome
}

// Status returns the run's final current status.
func (r Report) Status() machine.Status {
	return r.status
}

// Record returns the run's final record.
func (r Report) Record() machine.InOut {
	return r.record
}

// Outcome returns the outcome of the step named id, and whether that
// step resolved. The boolean is false when the step never resolved:
// the run aborted before it, or it sits in an unreached wave.
func (r Report) Outcome(id string) (Outcome, bool) {
	o, ok := r.outcomes[id]
	return o, ok
}

// Outcomes returns a copy of every resolved step's Outcome, keyed by
// step ID. Caller mutation of the returned map cannot change the
// Report.
func (r Report) Outcomes() map[string]Outcome {
	out := make(map[string]Outcome, len(r.outcomes))
	for id, o := range r.outcomes {
		out[id] = o
	}
	return out
}
