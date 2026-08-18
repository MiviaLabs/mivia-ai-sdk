package flow

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Confirm gates a step's ack. Run calls it after Fire moves the
// status, for a step named in no panel and for a one-member panel.
// Run does not call Confirm for a step in a panel of two or more
// members. Run calls Confirm again for a chained step after its
// child workflow completes and the parent transition fires. A nil
// return means the ack confirmed; the walk advances.
type Confirm func(ctx context.Context, step Step) error

// Run walks the step graph in topological order. A step named in no
// panel runs alone, in declaration order, as it did before panels
// existed; Run calls confirm for that step. See Confirm. A step named
// in a panel of one member runs alone the same way, and Run calls
// confirm for it too. A step named in a panel of two or more members
// runs as part of that panel's wave, once every member is ready; the
// wave fires every member's transition concurrently through the one
// shared row every member's homogeneous To selects. Run does not call
// confirm for a wave of two or more members. A step with a non-nil
// Sub runs its child workflow to completion, then uses the child
// final status as the parent step's target status. A chained step's
// child workflow runs with a nil bus, and its child steps emit no
// events. Run keeps the
// current status and one record through the walk; each wave reads the
// record and writes the next. Run rejects a nil d, a nil m, and a
// nil confirm at entry, before it dereferences d or m. It checks d
// first, then m, then confirm, so a nil m never panics inside a
// d-nil or m-nil check.
//
// Run returns a Report holding the final status, the final record,
// and every resolved step's Outcome. On every abort, Run returns the
// Report built so far, alongside the error. A step whose Fire fails
// or whose Confirm is rejected is marked OutcomeFailed before the
// return. A wave error marks no member of that wave: neither the
// failing member nor a successful sibling.
//
// A panel member's Input and Output must be either an immutable
// value, or a value the caller has already cloned per step. Run
// copies each member's InOut struct before it fires that member's
// transition, but the copy is shallow: a map, a slice, or a pointer
// an Input or Output field holds is not copied. Two panel members
// that alias the same underlying data still race if either mutates it
// in place. flow cannot deep-copy an arbitrary any value; this is a
// caller contract, not a runtime check.
func Run(
	ctx context.Context, d *Definition, m *machine.Definition,
	in machine.InOut, confirm Confirm, bus *events.Bus,
) (Report, error) {
	if d == nil {
		return Report{status: machine.Status(""), record: in}, errorf("d must not be nil")
	}
	if m == nil {
		return Report{status: machine.Status(""), record: in}, errorf("m must not be nil")
	}
	if confirm == nil {
		return Report{status: m.Initial(), record: in}, errorf("confirm must not be nil")
	}

	cur := m.Initial()
	rec := in
	outcomes := make(map[string]Outcome)

	if len(d.steps) == 0 {
		return Report{status: cur, record: rec, outcomes: outcomes}, nil
	}
	if len(d.steps) == 1 {
		var err error
		cur, rec, err = runSingletonAndMark(ctx, m, cur, rec, d.steps[0], confirm, bus, outcomes)
		return Report{status: cur, record: rec, outcomes: outcomes}, err
	}

	for len(outcomes) < len(d.steps) {
		next, group, ok := nextReadyGroup(d.steps, d.panels, outcomes)
		if !ok {
			// Unreachable for a same-panel Needs cycle: validatePanelIndependence
			// rejects that shape in New. Still reachable for a cross-panel
			// scheduling deadlock: a member of one panel needs a member of
			// another panel, and vice versa, with no cycle in the Needs graph and
			// no single panel's closure violation. New does not validate
			// cross-panel scheduling feasibility; a future phase may close this
			// gap.
			return Report{status: cur, record: rec, outcomes: outcomes}, errorf("no ready step; graph stalled")
		}

		if group == nil {
			var err error
			cur, rec, err = runSingletonAndMark(ctx, m, cur, rec, next, confirm, bus, outcomes)
			if err != nil {
				return Report{status: cur, record: rec, outcomes: outcomes}, err
			}
			continue
		}

		if len(group) == 1 {
			var err error
			cur, rec, err = runSingletonAndMark(ctx, m, cur, rec, group[0], confirm, bus, outcomes)
			if err != nil {
				return Report{status: cur, record: rec, outcomes: outcomes}, err
			}
			continue
		}

		var err error
		cur, rec, err = runWave(ctx, m, cur, rec, group)
		if err != nil {
			return Report{status: cur, record: rec, outcomes: outcomes}, err
		}

		// Emit event after successful wave execution
		for _, step := range group {
			emitStep(ctx, bus, step.ID)
		}

		markOutcome(outcomes, group, OutcomeSucceeded)
	}

	return Report{status: cur, record: rec, outcomes: outcomes}, nil
}

// runSingletonAndMark runs step through runSingleton and marks its
// resolution in outcomes: OutcomeSucceeded on success, OutcomeFailed
// on error, including every chained-step failure point runSingleton
// reports.
func runSingletonAndMark(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, confirm Confirm, bus *events.Bus,
	outcomes map[string]Outcome,
) (machine.Status, machine.InOut, error) {
	cur, rec, err := runSingleton(ctx, m, cur, rec, step, confirm, bus)
	if err != nil {
		outcomes[step.ID] = OutcomeFailed
		return cur, rec, err
	}
	outcomes[step.ID] = OutcomeSucceeded
	return cur, rec, nil
}

// runSingleton runs one ready step. It handles a chained step by
// running its child workflow first, then firing the parent transition.
// It returns the updated status and record, or an error.
func runSingleton(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, confirm Confirm, bus *events.Bus,
) (machine.Status, machine.InOut, error) {
	if step.Sub != nil {
		child, err := runChild(ctx, step.Sub, m, confirm)
		if err != nil {
			return cur, rec, err
		}
		cur, rec, err = fireFromChild(ctx, m, cur, rec, step, child)
		if err != nil {
			return cur, rec, err
		}
		if err := confirm(ctx, step); err != nil {
			return cur, rec, errorf("step %q: ack not confirmed: %w", step.ID, err)
		}
		emitStep(ctx, bus, step.ID)
		return cur, rec, nil
	}

	rows := m.AllowedTransitions(cur)
	row, err := pickTransition(rows, machine.Status(step.To))
	if err != nil {
		return cur, rec, errorf("step %q: %w", step.ID, err)
	}

	cur, rec, err = m.Fire(ctx, cur, row.Trigger, rec)
	if err != nil {
		return cur, rec, errorf("step %q: %w", step.ID, err)
	}

	if err := confirm(ctx, step); err != nil {
		return cur, rec, errorf("step %q: ack not confirmed: %w", step.ID, err)
	}
	emitStep(ctx, bus, step.ID)
	return cur, rec, nil
}

// runChild runs a chained step's child workflow to completion. It
// passes the same machine definition and a fresh InOut, starting from
// the machine's initial status. It uses the same confirm closure.
func runChild(
	ctx context.Context, child *Definition, m *machine.Definition, confirm Confirm,
) (machine.Status, error) {
	report, err := Run(ctx, child, m, machine.InOut{}, confirm, nil)
	return report.Status(), err
}

// fireFromChild picks the parent transition row from cur to child,
// fires it, and returns the updated status and record. It wraps
// errors with the parent step ID.
func fireFromChild(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, child machine.Status,
) (machine.Status, machine.InOut, error) {
	rows := m.AllowedTransitions(cur)
	row, err := pickTransition(rows, child)
	if err != nil {
		return cur, rec, errorf("step %q: %w", step.ID, err)
	}
	cur, rec, err = m.Fire(ctx, cur, row.Trigger, rec)
	if err != nil {
		return cur, rec, errorf("step %q: %w", step.ID, err)
	}
	return cur, rec, nil
}

// nextReadyGroup scans steps in declaration order for the next ready
// wave. A ready step named in no panel returns it as the singleton
// step with a nil group: the phase 5 path. A ready step named in a
// panel returns a zero step and the whole panel, once every member of
// that panel is ready. A partially-ready panel is skipped, not
// returned; the scan keeps looking for another ready step so a
// partially-ready panel never blocks the rest of the graph. Returns
// false when no step is ready.
func nextReadyGroup(steps []Step, panels []Panel, outcomes map[string]Outcome) (Step, []Step, bool) {
	for _, s := range steps {
		if _, resolved := outcomes[s.ID]; resolved {
			continue
		}
		if !needsMet(s.Needs, outcomes) {
			continue
		}
		p, found := panelFor(s.ID, panels)
		if !found {
			return s, nil, true
		}
		if panelReady(p, steps, outcomes) {
			return Step{}, panelMembers(p, steps), true
		}
	}
	return Step{}, nil, false
}

// needsMet reports whether every entry of needs already succeeded.
func needsMet(needs []string, outcomes map[string]Outcome) bool {
	for _, need := range needs {
		if o, ok := outcomes[need]; !ok || o != OutcomeSucceeded {
			return false
		}
	}
	return true
}

// panelFor returns the first panel in panels that names id, and
// whether one was found.
func panelFor(id string, panels []Panel) (Panel, bool) {
	for _, p := range panels {
		for _, member := range p {
			if member == id {
				return p, true
			}
		}
	}
	return nil, false
}

// panelReady reports whether every member of p is ready: not already
// resolved, and every entry of that member's Needs already succeeded.
func panelReady(p Panel, steps []Step, outcomes map[string]Outcome) bool {
	for _, id := range p {
		if _, resolved := outcomes[id]; resolved {
			return false
		}
		if !needsMet(stepByID(steps, id).Needs, outcomes) {
			return false
		}
	}
	return true
}

// panelMembers resolves every ID in p to its Step, in p's declaration
// order.
func panelMembers(p Panel, steps []Step) []Step {
	out := make([]Step, 0, len(p))
	for _, id := range p {
		out = append(out, stepByID(steps, id))
	}
	return out
}

// stepByID returns the step in steps whose ID equals id, or the zero
// Step when no step matches. New already proves every panel entry
// resolves to a known step, so the zero-value branch is unreachable
// through nextReadyGroup.
func stepByID(steps []Step, id string) Step {
	for _, s := range steps {
		if s.ID == id {
			return s
		}
	}
	return Step{}
}

// markOutcome marks every member of group with outcome o, in one
// pass. Run calls it once per successful wave.
func markOutcome(outcomes map[string]Outcome, group []Step, o Outcome) {
	for _, s := range group {
		outcomes[s.ID] = o
	}
}

// waveResult carries one panel member's Fire outcome.
type waveResult struct {
	step Step
	to   machine.Status
	rec  machine.InOut
	err  error
}

// runWave resolves the shared transition row once, before it spawns
// any goroutine. validatePanels' homogeneity rule guarantees every
// member of group shares one To, so pickTransition(rows, to) is a
// pure function of (rows, to); it cannot fail for one member and
// succeed for a sibling. A pickTransition failure fails the whole
// group before any goroutine spawns: no member's Guard, OnExit, or
// OnEntry runs. Every member then fires row.Trigger from its own
// InOut copy, concurrently. A member Fire failure joins with its
// siblings' failures through errors.Join; cur and rec stay at their
// pre-wave values, and the caller must not mark any member done. On
// success runWave returns the first group member's result, by
// declaration order, looked up by ID after every goroutine finishes,
// not by channel arrival order.
func runWave(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, group []Step,
) (machine.Status, machine.InOut, error) {
	rows := m.AllowedTransitions(cur)
	to := machine.Status(group[0].To)
	row, err := pickTransition(rows, to)
	if err != nil {
		return cur, rec, errorf("panel: %w", err)
	}

	results := make(chan waveResult, len(group))
	var wg sync.WaitGroup
	for _, s := range group {
		wg.Add(1)
		go func(s Step) {
			defer wg.Done()
			recCopy := rec
			to, out, err := m.Fire(ctx, cur, row.Trigger, recCopy)
			if err != nil {
				results <- waveResult{step: s, err: errorf("step %q: %w", s.ID, err)}
				return
			}
			results <- waveResult{step: s, to: to, rec: out}
		}(s)
	}
	wg.Wait()
	close(results)

	var errs []error
	byID := make(map[string]waveResult, len(group))
	for r := range results {
		byID[r.step.ID] = r
		if r.err != nil {
			errs = append(errs, r.err)
		}
	}
	if len(errs) > 0 {
		return cur, rec, errors.Join(errs...)
	}
	first := firstByDeclaration(byID, group)
	return first.to, first.rec, nil
}

// firstByDeclaration returns the group member listed first in group,
// looked up in byID by ID. Declaration order, not map iteration order
// or channel arrival order, decides which member's result runWave
// forwards.
func firstByDeclaration(byID map[string]waveResult, group []Step) waveResult {
	return byID[group[0].ID]
}

// pickTransition filters rows to the one whose To equals to. Zero
// matches and more than one match both fail; the error names the
// candidate count so the ambiguity is visible.
func pickTransition(rows []machine.Transition, to machine.Status) (machine.Transition, error) {
	var found *machine.Transition
	count := 0
	for i := range rows {
		if rows[i].To == to {
			if found == nil {
				found = &rows[i]
			}
			count++
		}
	}
	switch count {
	case 0:
		return machine.Transition{}, fmt.Errorf(
			"no transition to status %q from %q", to, rowsFrom(rows),
		)
	case 1:
		return *found, nil
	default:
		return machine.Transition{}, fmt.Errorf(
			"ambiguous transition to status %q from %q (%d candidates)",
			to, rowsFrom(rows), count,
		)
	}
}

// rowsFrom returns the shared From status of rows, or the zero
// Status when rows is empty. AllowedTransitions always returns rows
// that share one From, so the first row's From names the source.
func rowsFrom(rows []machine.Transition) machine.Status {
	if len(rows) == 0 {
		return machine.Status("")
	}
	return rows[0].From
}

// emitStep emits a StepCompletedEvent onto the bus. It silently
// ignores a missing subscriber. A validation error or an unexpected
// error is also ignored; the caller owns the bus and decides what
// to log. Emit never fails the run.
func emitStep(ctx context.Context, bus *events.Bus, id string) {
	if bus == nil {
		return
	}
	_ = bus.Emit(ctx, events.Event{
		Name: StepCompletedEvent,
		Data: fmt.Sprintf("step %s completed", id),
	})
}
