package agentrun

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// ValidateMatrix checks that the plan's transition rows exist in m.
// It walks plan.Steps() and plan.Panels() and computes each logical
// step's predecessor status set. For every predecessor the machine
// must hold exactly one row From=p To=target. Zero rows and two rows
// both fail, naming the step, the predecessor, and the target. It is
// a static check; it does not prove the walk never aborts. Route
// exclusions, skipped needs, and forward-type mismatches can still
// abort mid-run after New passes.
func ValidateMatrix(plan *flow.Definition, m *machine.Definition) error {
	if plan == nil {
		return fmt.Errorf("agentrun: plan must not be nil")
	}
	if m == nil {
		return fmt.Errorf("agentrun: machine must not be nil")
	}
	w := &walker{m: m}
	return w.walk(plan.Steps(), plan.Panels())
}

// walker holds one machine and computes predecessor status sets for
// one plan during a ValidateMatrix call.
type walker struct {
	m *machine.Definition
}

// walk validates one definition's steps and panels. It validates each
// two-or-more-member panel as one wave, unioning the members'
// predecessor sets against the homogeneous To flow.New already proved.
// Every other step validates alone against its own predecessor set.
func (w *walker) walk(steps []flow.Step, panels []flow.Panel) error {
	wave := map[string]bool{}
	for _, p := range panels {
		if len(p) < 2 {
			continue
		}
		union := map[machine.Status]bool{}
		first := w.find(steps, p[0])
		for _, id := range p {
			for _, st := range w.preds(steps, w.find(steps, id)) {
				union[st] = true
			}
		}
		target := []machine.Status{machine.Status(first.To)}
		if err := w.checkUnit(joinIDs(p), predSlice(union), target); err != nil {
			return err
		}
		for _, id := range p {
			wave[id] = true
		}
	}
	for _, s := range steps {
		if wave[s.ID] {
			continue
		}
		if err := w.checkUnit(s.ID, w.preds(steps, s), w.targets(s)); err != nil {
			return err
		}
	}
	return nil
}

// targets returns the statuses a step's transition fires to. A plain
// step targets its own To. A Sub or Loop step targets its child
// workflow's terminal statuses, because the runner uses the child's
// final status instead of the parent's To.
func (w *walker) targets(s flow.Step) []machine.Status {
	if s.Sub != nil {
		return w.childFinals(s.Sub)
	}
	return []machine.Status{machine.Status(s.To)}
}

// preds returns the predecessor status set for step s in its own
// definition. A step with no needs starts from the machine initial
// status. A plain need contributes its To. A Sub or Loop need
// contributes the child's terminal statuses. An AdmissionOnFailed step
// also adds every need's own predecessor set: a Fire failure leaves
// the pre-fire status while a Route or loop failure leaves the
// post-step status.
func (w *walker) preds(steps []flow.Step, s flow.Step) []machine.Status {
	if len(s.Needs) == 0 {
		return []machine.Status{w.m.Initial()}
	}
	set := map[machine.Status]bool{}
	for _, nid := range s.Needs {
		for _, st := range w.contributed(steps, nid) {
			set[st] = true
		}
	}
	if s.When == flow.AdmissionOnFailed {
		for _, nid := range s.Needs {
			for _, st := range w.preds(steps, w.find(steps, nid)) {
				set[st] = true
			}
		}
	}
	return predSlice(set)
}

// contributed returns the statuses a need named by nid adds to a
// predecessor set. A plain need contributes its To. A Sub or Loop need
// contributes the child workflow's terminal statuses, because the
// runner targets the child's final status, not the parent's To.
func (w *walker) contributed(steps []flow.Step, nid string) []machine.Status {
	need := w.find(steps, nid)
	if need.Sub == nil {
		return []machine.Status{machine.Status(need.To)}
	}
	return w.childFinals(need.Sub)
}

// childFinals returns the terminal statuses of a child workflow: the
// status each step with no sibling dependent leaves after it fires. A
// Sub step contributes its own child's terminal statuses instead of
// its To. Panel members contribute their homogeneous To when terminal.
func (w *walker) childFinals(d *flow.Definition) []machine.Status {
	steps := d.Steps()
	needed := make(map[string]bool, len(steps))
	for _, s := range steps {
		for _, n := range s.Needs {
			needed[n] = true
		}
	}
	set := map[machine.Status]bool{}
	for _, s := range steps {
		if needed[s.ID] {
			continue
		}
		if s.Sub != nil {
			for _, st := range w.childFinals(s.Sub) {
				set[st] = true
			}
			continue
		}
		set[machine.Status(s.To)] = true
	}
	return predSlice(set)
}

// checkUnit verifies every predecessor-to-target pair in a step's set
// holds exactly one machine row. It names the step, the predecessor,
// and the target on a miss or an ambiguity.
func (w *walker) checkUnit(label string, preds, targets []machine.Status) error {
	for _, from := range preds {
		for _, to := range targets {
			if err := w.checkRow(from, to); err != nil {
				return fmt.Errorf("agentrun: step %q: %w", label, err)
			}
		}
	}
	return nil
}

// checkRow verifies exactly one machine row runs from to from. Zero
// rows and two rows both fail and changed nothing.
func (w *walker) checkRow(from, to machine.Status) error {
	count := 0
	for _, t := range w.m.Transitions() {
		if t.From == from && t.To == to {
			count++
		}
	}
	switch count {
	case 0:
		return fmt.Errorf("no transition from %q to %q", from, to)
	case 1:
		return nil
	default:
		return fmt.Errorf("ambiguous transition from %q to %q (%d rows)", from, to, count)
	}
}

// find returns the step in steps whose ID equals id, or the zero Step
// when no step matches. flow.New already proved every need and panel
// entry resolves, so the zero-value branch is unreachable here.
func (w *walker) find(steps []flow.Step, id string) flow.Step {
	for _, s := range steps {
		if s.ID == id {
			return s
		}
	}
	return flow.Step{}
}

// predSlice returns the set's statuses sorted, for deterministic
// output.
func predSlice(set map[machine.Status]bool) []machine.Status {
	out := make([]machine.Status, 0, len(set))
	for st := range set {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// joinIDs joins panel member IDs with a space for an error label.
func joinIDs(p []string) string {
	return strings.Join(p, " ")
}
