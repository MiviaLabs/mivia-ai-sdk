package flow

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// verdict is the result of evaluating one step's or one panel's
// admission against the outcomes resolved so far.
type verdict int

const (
	// verdictWait means at least one need has not yet resolved.
	verdictWait verdict = iota
	// verdictAdmit means every need satisfies the admission rule.
	verdictAdmit
	// verdictSkip means at least one need fails the admission rule.
	verdictSkip
)

// admissionVerdict evaluates s's admission rule against outcomes. It
// returns verdictWait when any of s's needs has not yet resolved,
// verdictAdmit when every need satisfies s.When, and verdictSkip
// otherwise. A step with no needs always admits. AdmissionOnFailed is
// an any-of rule over Needs; admissionVerdict delegates to
// admitsOnFailed instead of running the all-of loop below.
func admissionVerdict(s Step, outcomes map[string]Outcome) verdict {
	if s.When == AdmissionOnFailed {
		return admitsOnFailed(s.Needs, outcomes)
	}
	for _, need := range s.Needs {
		if _, ok := outcomes[need]; !ok {
			return verdictWait
		}
	}
	for _, need := range s.Needs {
		if !admits(s.When, outcomes[need]) {
			return verdictSkip
		}
	}
	return verdictAdmit
}

// admits reports whether outcome o satisfies admission rule when.
// AdmissionOnSucceeded, the zero value, accepts only
// OutcomeSucceeded. AdmissionOnFinished, the explicit opt-in,
// accepts OutcomeSucceeded and OutcomeSkipped.
func admits(when Admission, o Outcome) bool {
	if when == AdmissionOnSucceeded {
		return o == OutcomeSucceeded
	}
	return o == OutcomeSucceeded || o == OutcomeSkipped
}

// panelVerdict reports p's admission verdict as one atomic unit. It
// returns verdictWait the moment any member's needs are not yet
// terminal, so the panel skip decision never fires on a partial view
// across loop iterations. Once every member's needs are terminal, it
// returns verdictAdmit only when every member's own verdict admits,
// and verdictSkip otherwise: one unadmitted member skips the whole
// panel.
func panelVerdict(p Panel, steps []Step, outcomes map[string]Outcome) verdict {
	allAdmit := true
	for _, id := range p {
		if _, resolved := outcomes[id]; resolved {
			// Unreachable through Run: New bars a panel member from
			// being a direct dependent of a branch step, so route
			// exclusion never resolves a member ahead of its panel.
			// panelVerdict itself only ever resolves every member of
			// p at once, through markOutcome. This guard stays as a
			// defensive mirror of the pre-routing panelReady check.
			return verdictWait
		}
		switch admissionVerdict(stepByID(steps, id), outcomes) {
		case verdictWait:
			return verdictWait
		case verdictSkip:
			allAdmit = false
		}
	}
	if allAdmit {
		return verdictAdmit
	}
	return verdictSkip
}

// applyRoute runs a branch step s's Route function with the post-fire
// status and record, then marks every direct dependent of s that
// Route did not name OutcomeSkipped. The skip is final: it overrides
// any other pending need the excluded dependent has. A duplicate ID
// in Route's return collapses to one admission. An empty return skips
// every direct dependent. It returns an error when Route itself
// fails, or when Route names an ID that is not a direct dependent of
// s; both errors mark no dependent skipped.
func applyRoute(
	ctx context.Context, s Step, cur machine.Status, rec machine.InOut,
	steps []Step, outcomes map[string]Outcome,
) error {
	deps := directDependents(s.ID, steps)
	kept, err := s.Route(ctx, cur, rec)
	if err != nil {
		return errorf("step %q: route: %w", s.ID, err)
	}
	keptSet := make(map[string]bool, len(kept))
	for _, id := range kept {
		if !isDependent(id, deps) {
			return errorf("step %q: route named %q, not a direct dependent", s.ID, id)
		}
		keptSet[id] = true
	}
	for _, d := range deps {
		if !keptSet[d.ID] {
			outcomes[d.ID] = OutcomeSkipped
		}
	}
	return nil
}

// isDependent reports whether id names a step in deps.
func isDependent(id string, deps []Step) bool {
	for _, d := range deps {
		if d.ID == id {
			return true
		}
	}
	return false
}

// directDependents returns every step in steps whose Needs names id,
// in declaration order. A direct dependent is a step that names id in
// its own Needs; it excludes a step that reaches id only through a
// transitive chain.
func directDependents(id string, steps []Step) []Step {
	var out []Step
	for _, s := range steps {
		for _, need := range s.Needs {
			if need == id {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// validateRouting checks every routing constraint New enforces: a
// step cannot combine Sub and Route, a branch step must have at least
// one dependent, no panel may name a branch step, and no panel may
// name a direct dependent of a branch step. The last two rules close
// the stall risk a mid-panel route exclusion would otherwise create,
// since panelVerdict treats a panel as one atomic unit.
func validateRouting(steps []Step, panels []Panel, ids map[string]int) error {
	for i := range steps {
		if steps[i].Sub != nil && steps[i].Route != nil {
			return errorf("step %q has both Sub and Route", steps[i].ID)
		}
	}
	for i := range steps {
		s := steps[i]
		if s.Route == nil {
			continue
		}
		if len(directDependents(s.ID, steps)) == 0 {
			return errorf("step %q has a route but no dependent", s.ID)
		}
	}
	for i, p := range panels {
		for _, id := range p {
			if steps[ids[id]].Route != nil {
				return errorf("panel %d names routed step %q", i, id)
			}
		}
	}
	for i, p := range panels {
		for _, id := range p {
			if branch, ok := routedParentOf(id, steps); ok {
				return errorf(
					"panel %d names step %q, a direct dependent of routed step %q",
					i, id, branch,
				)
			}
		}
	}
	return nil
}

// routedParentOf reports the ID of a branch step that id directly
// depends on, and whether one exists.
func routedParentOf(id string, steps []Step) (string, bool) {
	s := stepByID(steps, id)
	for _, need := range s.Needs {
		if parent := stepByID(steps, need); parent.Route != nil {
			return parent.ID, true
		}
	}
	return "", false
}
