package agentrun

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// ValidateMatrix checks that the plan's transition rows exist in m.
// It simulates the runner's declaration-order scan on the all-run
// path, so each step's rows start from the statuses the walk can
// rest on, not from the machine's initial status: sequential roots
// and siblings chain. For every such status the machine must hold
// exactly one row From=p To=target. Zero rows and two rows both
// fail, naming the step, the source, and the target. It recurses
// into every Sub child at every depth: a child Run starts from the
// machine's initial status, so the child's own walk is checked the
// same way. A Loop step that can run a second iteration also needs a
// re-entry row between every pair of distinct child finals. It is a
// static check; it does not prove the walk never aborts. Route
// exclusions, skipped needs, forward-type mismatches, and a loop
// landing the same final twice (machine.New forbids the self row
// that needs) can still abort mid-run after New passes.
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

// walk validates one definition by simulating the runner's
// declaration-order scan on the all-run path. The standing set holds
// every status the walk can rest on at each point; a step requires
// rows from that set to its targets. Fallback steps fire from their
// needs' failure spans instead. A panel fires as one wave from the
// standing set to its shared To.
func (w *walker) walk(steps []flow.Step, panels []flow.Panel) error {
	s := &walkSim{
		w:         w,
		steps:     steps,
		panels:    panels,
		resolved:  map[string]bool{},
		curs:      map[machine.Status]bool{w.m.Initial(): true},
		firedFrom: map[string][]machine.Status{},
		firedTo:   map[string][]machine.Status{},
	}
	return s.run()
}

// walkSim tracks one simulated walk: which steps resolved, the
// standing status set, and each step's fire span for its fallback
// dependents.
type walkSim struct {
	w         *walker
	steps     []flow.Step
	panels    []flow.Panel
	resolved  map[string]bool
	curs      map[machine.Status]bool
	firedFrom map[string][]machine.Status
	firedTo   map[string][]machine.Status
}

// run scans in declaration order, mirroring nextReadyGroup's shape,
// until every step resolves.
func (s *walkSim) run() error {
	for {
		step, panel, isPanel, found := s.nextUnit()
		if !found {
			return nil
		}
		var err error
		if isPanel {
			err = s.runPanel(panel)
		} else {
			err = s.runStep(step)
		}
		if err != nil {
			return err
		}
	}
}

// nextUnit returns the next ready unit in declaration order: a
// singleton step, or a whole panel once every member's needs
// resolved. A waiting step never blocks a later ready one.
func (s *walkSim) nextUnit() (flow.Step, flow.Panel, bool, bool) {
	for _, st := range s.steps {
		if s.resolved[st.ID] {
			continue
		}
		if p, ok := simPanelOf(st.ID, s.panels); ok && len(p) >= 2 {
			if s.panelReady(p) {
				return flow.Step{}, p, true, true
			}
			continue
		}
		if s.needsReady(st) {
			return st, nil, false, true
		}
	}
	return flow.Step{}, nil, false, false
}

// runStep validates one step, records its fire span, and advances
// the standing set to its targets.
func (s *walkSim) runStep(st flow.Step) error {
	preds := s.standingFor(st)
	targets := s.w.targets(st)
	if err := s.w.checkUnit(st.ID, preds, targets); err != nil {
		return err
	}
	if err := s.w.checkLoopReentry(st); err != nil {
		return err
	}
	if st.Sub != nil {
		if err := s.w.walk(st.Sub.Steps(), st.Sub.Panels()); err != nil {
			return err
		}
	}
	s.resolved[st.ID] = true
	s.firedFrom[st.ID] = preds
	s.firedTo[st.ID] = targets
	s.advance(targets, s.keepsPriorSet(st.ID, st.When == flow.AdmissionOnFailed))
	return nil
}

// runPanel validates one wave from the standing set and lands the
// walk on the shared To.
func (s *walkSim) runPanel(p flow.Panel) error {
	targets := []machine.Status{machine.Status(s.w.find(s.steps, p[0]).To)}
	if err := s.w.checkUnit(joinIDs(p), predSlice(s.curs), targets); err != nil {
		return err
	}
	keep := false
	preds := predSlice(s.curs)
	for _, id := range p {
		s.resolved[id] = true
		s.firedFrom[id] = preds
		s.firedTo[id] = targets
		if s.keepsPriorSet(id, false) {
			keep = true
		}
	}
	s.advance(targets, keep)
	return nil
}

// standingFor returns the statuses st fires from. A normal step fires
// from the standing set. A fallback fires from each need's failure
// span: the statuses the need fired from, for a failed Fire, and its
// targets, for a failed Route.
func (s *walkSim) standingFor(st flow.Step) []machine.Status {
	if st.When != flow.AdmissionOnFailed {
		return predSlice(s.curs)
	}
	set := map[machine.Status]bool{}
	for _, n := range st.Needs {
		for _, f := range s.firedFrom[n] {
			set[f] = true
		}
		for _, to := range s.firedTo[n] {
			set[to] = true
		}
	}
	return predSlice(set)
}

// advance moves the standing set after one unit ran. The walk rests
// on the unit's targets. keepPrior also carries the prior set
// forward, for a step whose failure or skipped branch stays live.
func (s *walkSim) advance(targets []machine.Status, keepPrior bool) {
	if !keepPrior {
		s.curs = map[machine.Status]bool{}
	}
	for _, t := range targets {
		s.curs[t] = true
	}
}

// keepsPriorSet reports whether the failure or skipped branch stays
// live after id ran: id has a fallback dependent, or id is itself a
// fallback whose success branch skipped it.
func (s *walkSim) keepsPriorSet(id string, isFallback bool) bool {
	if isFallback {
		return true
	}
	for _, st := range s.steps {
		if st.When == flow.AdmissionOnFailed {
			for _, n := range st.Needs {
				if n == id {
					return true
				}
			}
		}
	}
	return false
}

// needsReady reports whether every need of st resolved.
func (s *walkSim) needsReady(st flow.Step) bool {
	for _, n := range st.Needs {
		if !s.resolved[n] {
			return false
		}
	}
	return true
}

// panelReady reports whether every member of p resolved its needs.
func (s *walkSim) panelReady(p flow.Panel) bool {
	for _, id := range p {
		if !s.needsReady(s.w.find(s.steps, id)) {
			return false
		}
	}
	return true
}

// simPanelOf returns the first panel naming id, and whether one was
// found.
func simPanelOf(id string, panels []flow.Panel) (flow.Panel, bool) {
	for _, p := range panels {
		for _, member := range p {
			if member == id {
				return p, true
			}
		}
	}
	return nil, false
}

// checkLoopReentry requires a row between every pair of distinct child
// finals when s's loop policy can run a second iteration. machine.New
// forbids a From-equals-To row, so a child that lands the same final
// twice always faults at runtime; that limit stays disclosed in
// ValidateMatrix's comment, not checked here.
func (w *walker) checkLoopReentry(s flow.Step) error {
	if s.Loop == nil || s.Loop.Max == 1 || s.Sub == nil {
		return nil
	}
	finals := w.childFinals(s.Sub)
	for _, from := range finals {
		for _, to := range finals {
			if from == to {
				continue
			}
			if err := w.checkRow(from, to); err != nil {
				return fmt.Errorf("agentrun: step %q: loop re-entry: %w", s.ID, err)
			}
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
