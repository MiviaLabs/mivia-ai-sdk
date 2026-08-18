package flow

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Confirm gates a step's ack. Run calls it after Fire moves the
// status, for a step named in no panel and for a one-member panel,
// and again for a chained step after its child workflow completes and
// the parent transition fires. Run does not call Confirm for a step
// in a panel of two or more members. A nil return means the ack
// confirmed; the walk advances.
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
// Sub runs its child workflow to completion, then uses the child final
// status as the parent step's target status; the child runs with a
// nil bus. Run keeps the current status and one record through the
// walk. Run rejects a nil d, a nil m, and a nil confirm at entry,
// checking d first, then m, then confirm, so a nil m never panics
// inside a d-nil or m-nil check.
//
// onCheckpoint, when non-nil, fires immediately after each step or
// wave resolves OutcomeSucceeded, with a fresh Checkpoint. A nil
// onCheckpoint skips the call. See Checkpoint and Resume.
//
// Before each step or wave starts, Run checks ctx for cancellation. A
// canceled ctx stops the walk before the next step starts and returns
// the pinned pause error, wrapping ctx.Err(); the last Checkpoint
// onCheckpoint delivered is the resume point. A step already running
// keeps running to its own completion, unless cancellation is observed
// inside a retry loop or a looped child workflow; in those cases the
// current step aborts and returns the context error.
//
// Run returns a Report holding the final status, the final record,
// and every resolved step's Outcome. On every abort, Run returns the
// Report built so far, alongside the error. A step whose Fire fails
// or whose Confirm is rejected is marked OutcomeFailed before the
// return. A wave's shared, pre-spawn transition failure marks no
// member of that wave; a per-member Fire failure inside a wave marks
// every member OutcomeFailed, whether or not a dependent's
// AdmissionOnFailed rule catches the failure. A step admitted through
// a failed need (AdmissionOnFailed) is a fallback; Run injects a
// Failure into its transition's context, and FailureFrom reads it
// back. A caught Fire or Route failure continues down the fallback
// path; a Confirm rejection or a missing transition row stays fatal.
// Checkpoint's Failed field preserves an already-caught failure's
// outcome across a pause; see Checkpoint for what does not survive.
//
// A panel member's Input and Output must be an immutable value, or a
// value the caller already cloned per step. Run's copy of each
// member's InOut is shallow: a map, a slice, or a pointer an Input or
// Output field holds is not copied. Two members that alias the same
// data still race if either mutates it in place.
func Run(
	ctx context.Context, d *Definition, m *machine.Definition,
	in machine.InOut, confirm Confirm, bus *events.Bus,
	onCheckpoint func(Checkpoint),
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

	outcomes := make(map[string]Outcome)
	return runLoop(ctx, d, m, m.Initial(), in, outcomes, confirm, bus, onCheckpoint)
}

// advanceGroup runs, skips, or rejects the group nextReadyGroup found,
// for one loop iteration of Run. It marks every resolved step's
// Outcome before it returns, and fires onCheckpoint once a step's or a
// wave's outcomes settle at OutcomeSucceeded, after any route-driven
// skip so a checkpoint never captures a state a route rejection later
// overwrites. A catchable failure resolves through resolveCatchable
// or resolvePanelFailure, fires no checkpoint, and returns nil.
func advanceGroup(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, next Step, group []Step, res scanResult,
	steps []Step, confirm Confirm, bus *events.Bus, outcomes map[string]Outcome,
	pending map[string]*handledFailure, onCheckpoint func(Checkpoint),
) (machine.Status, machine.InOut, error) {
	switch res {
	case scanNone:
		// Unreachable for a same-panel Needs cycle; reachable for a
		// cross-panel scheduling deadlock, which New does not validate for.
		return cur, rec, errorf("no ready step; graph stalled")

	case scanSkipSingleton:
		outcomes[next.ID] = OutcomeSkipped
		return cur, rec, nil

	case scanSkipPanel:
		markOutcome(outcomes, group, OutcomeSkipped)
		return cur, rec, nil

	case scanSingleton:
		cur, rec, err := runSingletonAndMark(ctx, m, cur, rec, next, confirm, bus, outcomes, pending)
		if err != nil {
			resolved, handled := resolveCatchable(err, next.ID, steps, outcomes, pending)
			return cur, rec, continueOrAbort(resolved, handled)
		}
		if next.Route == nil {
			fireCheckpoint(onCheckpoint, cur, rec, outcomes)
			return cur, rec, nil
		}
		if rerr := applyRoute(ctx, next, cur, rec, steps, outcomes); rerr != nil {
			outcomes[next.ID] = OutcomeFailed
			resolved, handled := resolveCatchable(newFailureError(failureKindFire, rerr), next.ID, steps, outcomes, pending)
			return cur, rec, continueOrAbort(resolved, handled)
		}
		if aerr := prunePendingOnRoute(next, steps, outcomes, pending); aerr != nil {
			return cur, rec, aerr
		}
		fireCheckpoint(onCheckpoint, cur, rec, outcomes)
		return cur, rec, nil

	case scanPanel:
		if len(group) == 1 {
			cur, rec, err := runSingletonAndMark(ctx, m, cur, rec, group[0], confirm, bus, outcomes, pending)
			if err != nil {
				resolved, handled := resolveCatchable(err, group[0].ID, steps, outcomes, pending)
				return cur, rec, continueOrAbort(resolved, handled)
			}
			fireCheckpoint(onCheckpoint, cur, rec, outcomes)
			return cur, rec, nil
		}
		cur, rec, err := runWave(ctx, m, cur, rec, group)
		if err != nil {
			var fe *failureError
			if errors.As(err, &fe) {
				// The shared pre-spawn transition failed: uncatchable, no
				// member's outcome marked.
				return cur, rec, err
			}
			resolved, handled := resolvePanelFailure(err, group, steps, outcomes, pending)
			return cur, rec, continueOrAbort(resolved, handled)
		}
		for _, step := range group {
			emitStep(ctx, bus, step.ID)
		}
		markOutcome(outcomes, group, OutcomeSucceeded)
		fireCheckpoint(onCheckpoint, cur, rec, outcomes)
		return cur, rec, nil

	default:
		// Unreachable: nextReadyGroup returns only the five scanResult
		// values this switch already handles.
		return cur, rec, errorf("nextReadyGroup returned an unknown result")
	}
}

// runSingletonAndMark runs step through runSingleton and marks its
// resolution: OutcomeSucceeded on success, OutcomeFailed on error.
// It then prunes step from every pending handler set: a handler that
// ran, either way, can never lose its only runner.
func runSingletonAndMark(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, confirm Confirm, bus *events.Bus,
	outcomes map[string]Outcome, pending map[string]*handledFailure,
) (machine.Status, machine.InOut, error) {
	cur, rec, err := runSingleton(ctx, m, cur, rec, step, confirm, bus, pending)
	if err != nil {
		outcomes[step.ID] = OutcomeFailed
		prunePendingHandler(pending, step.ID)
		return cur, rec, err
	}
	outcomes[step.ID] = OutcomeSucceeded
	prunePendingHandler(pending, step.ID)
	return cur, rec, nil
}

// runSingleton runs one ready step, handling a chained step by running
// its child workflow first, then firing the parent transition. A step
// admitted through a pending failed need fires with a Failure in ctx.
func runSingleton(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, confirm Confirm, bus *events.Bus,
	pending map[string]*handledFailure,
) (machine.Status, machine.InOut, error) {
	fireCtx := ctx
	if fail, ok := failureForStep(step, pending); ok {
		fireCtx = withFailure(ctx, fail)
	}

	if step.Sub != nil {
		if step.Loop != nil {
			var err error
			cur, rec, err = runLoopedChild(ctx, fireCtx, m, cur, rec, step, confirm)
			if err != nil {
				return cur, rec, err
			}
		} else {
			child, err := runChild(ctx, step.Sub, m, confirm)
			if err != nil {
				// The child Run already exhausted its own continue rule, so
				// any *failureError tag here is the child frame's, not this
				// one; strip it so it never matches a parent-level fallback.
				var fe *failureError
				for errors.As(err, &fe) {
					err = fe.err
				}
				return cur, rec, err
			}
			cur, rec, err = fireFromChild(fireCtx, m, cur, rec, step, child)
			if err != nil {
				return cur, rec, err
			}
		}
		if err := confirmStep(ctx, confirm, step, rec); err != nil {
			return cur, rec, err
		}
		emitStep(ctx, bus, step.ID)
		return cur, rec, nil
	}

	rows := m.AllowedTransitions(cur)
	row, err := pickTransitionFor(step, rows, machine.Status(step.To))
	if err != nil {
		return cur, rec, err
	}

	cur, rec, err = fireWithRetry(fireCtx, m, cur, rec, step, row)
	if err != nil {
		return cur, rec, err
	}

	if err := confirmStep(ctx, confirm, step, rec); err != nil {
		return cur, rec, err
	}
	emitStep(ctx, bus, step.ID)
	return cur, rec, nil
}

// fireFromChild picks the parent transition row from cur to child and
// fires it, through the same pickTransitionFor and fireStep helpers
// the straight-line branch uses, so its failures tag the same way. A
// cur already equal to child is a satisfied re-entry: no row, no
// fire, and the status and record stay unchanged.
func fireFromChild(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, child machine.Status,
) (machine.Status, machine.InOut, error) {
	if cur == child {
		return cur, rec, nil
	}
	rows := m.AllowedTransitions(cur)
	row, err := pickTransitionFor(step, rows, child)
	if err != nil {
		return cur, rec, err
	}
	cur, rec, err = fireStep(ctx, m, cur, rec, step, row)
	if err != nil {
		return cur, rec, err
	}
	return cur, rec, nil
}

// scanResult describes what nextReadyGroup found for one loop
// iteration of Run.
type scanResult int

const (
	// scanNone means no step is ready; the graph stalled.
	scanNone scanResult = iota
	// scanSingleton means the returned step is ready to run alone.
	scanSingleton
	// scanSkipSingleton means the returned step failed admission and
	// must skip.
	scanSkipSingleton
	// scanPanel means the returned group is ready to run as one wave.
	scanPanel
	// scanSkipPanel means one member of the returned group failed
	// admission; every member skips.
	scanSkipPanel
)

// nextReadyGroup scans steps in declaration order for the next group
// to run or skip. A step named in no panel evaluates its own
// admissionVerdict: verdictAdmit returns it as a singleton with
// scanSingleton, verdictSkip returns it with scanSkipSingleton,
// verdictWait moves the scan on. A step named in a panel evaluates
// panelVerdict for the whole panel: verdictAdmit returns every member
// with scanPanel, verdictSkip returns every member with scanSkipPanel,
// verdictWait moves the scan on so a partially-resolved panel never
// blocks the rest of the graph. Returns scanNone when no step is
// ready to run or skip.
func nextReadyGroup(steps []Step, panels []Panel, outcomes map[string]Outcome) (Step, []Step, scanResult) {
	for _, s := range steps {
		if _, resolved := outcomes[s.ID]; resolved {
			continue
		}
		p, found := panelFor(s.ID, panels)
		if found {
			switch panelVerdict(p, steps, outcomes) {
			case verdictAdmit:
				return Step{}, panelMembers(p, steps), scanPanel
			case verdictSkip:
				return Step{}, panelMembers(p, steps), scanSkipPanel
			default:
				continue
			}
		}
		switch admissionVerdict(s, outcomes) {
		case verdictAdmit:
			return s, nil, scanSingleton
		case verdictSkip:
			return s, nil, scanSkipSingleton
		default:
			continue
		}
	}
	return Step{}, nil, scanNone
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
