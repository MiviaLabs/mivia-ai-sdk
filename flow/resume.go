package flow

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Resume restarts a graph walk from a stored checkpoint. It seeds
// outcomes from checkpoint.Done (every listed ID set to
// OutcomeSucceeded), cur from checkpoint.Status, and rec from
// checkpoint.Record, then continues the same graph walk Run uses.
// Resume runs five entry checks in order, before any seeding happens:
// a nil d, a nil m, a nil confirm, a checkpoint that fails Validate,
// and a checkpoint.Done entry naming a step ID absent from d. The
// first failing check returns an error immediately; no step runs.
//
// Resume performs no topology check across Done: it never confirms
// that a step named in Done has every one of its own Needs also named
// in Done. A topologically-inconsistent checkpoint is not rejected at
// entry; nextReadyGroup treats a missing prerequisite as unresolved
// and selects it to run again, and the resulting pickTransition or
// machine.Fire call fails, because checkpoint.Status no longer names a
// status the seeded walk can reach that step from. Resume returns
// that failure as an ordinary error.
func Resume(
	ctx context.Context, d *Definition, m *machine.Definition,
	checkpoint Checkpoint, confirm Confirm, bus *events.Bus,
	onCheckpoint func(Checkpoint),
) (Report, error) {
	if d == nil {
		return Report{status: machine.Status(""), record: checkpoint.Record}, errorf("d must not be nil")
	}
	if m == nil {
		return Report{status: machine.Status(""), record: checkpoint.Record}, errorf("m must not be nil")
	}
	if confirm == nil {
		return Report{status: m.Initial(), record: checkpoint.Record}, errorf("confirm must not be nil")
	}
	if err := checkpoint.Validate(); err != nil {
		return Report{status: machine.Status(""), record: checkpoint.Record}, err
	}

	outcomes := make(map[string]Outcome, len(checkpoint.Done))
	for _, id := range checkpoint.Done {
		if !hasStep(d.steps, id) {
			return Report{status: checkpoint.Status, record: checkpoint.Record},
				errorf("resume: checkpoint names unknown step %q", id)
		}
		outcomes[id] = OutcomeSucceeded
	}

	return runLoop(ctx, d, m, checkpoint.Status, checkpoint.Record, outcomes, confirm, bus, onCheckpoint)
}

// hasStep reports whether steps contains a step whose ID equals id.
func hasStep(steps []Step, id string) bool {
	for _, s := range steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

// runLoop is the shared graph walk Run and Resume both drive. It
// takes the seed cur, rec, and outcomes: Run seeds outcomes empty and
// cur at m.Initial(); Resume seeds outcomes from a checkpoint's Done
// and cur/rec from the checkpoint's Status/Record.
func runLoop(
	ctx context.Context, d *Definition, m *machine.Definition,
	cur machine.Status, rec machine.InOut, outcomes map[string]Outcome,
	confirm Confirm, bus *events.Bus, onCheckpoint func(Checkpoint),
) (Report, error) {
	if len(d.steps) == 0 {
		return Report{status: cur, record: rec, outcomes: outcomes}, nil
	}
	if len(d.steps) == 1 {
		if _, done := outcomes[d.steps[0].ID]; done {
			return Report{status: cur, record: rec, outcomes: outcomes}, nil
		}
		if err := ctx.Err(); err != nil {
			return Report{status: cur, record: rec, outcomes: outcomes}, errorf("run paused: %w", err)
		}
		var err error
		cur, rec, err = runSingletonAndMark(ctx, m, cur, rec, d.steps[0], confirm, bus, outcomes)
		if err != nil {
			return Report{status: cur, record: rec, outcomes: outcomes}, err
		}
		fireCheckpoint(onCheckpoint, cur, rec, outcomes)
		return Report{status: cur, record: rec, outcomes: outcomes}, nil
	}

	for len(outcomes) < len(d.steps) {
		if err := ctx.Err(); err != nil {
			return Report{status: cur, record: rec, outcomes: outcomes}, errorf("run paused: %w", err)
		}

		next, group, res := nextReadyGroup(d.steps, d.panels, outcomes)
		var err error
		cur, rec, err = advanceGroup(ctx, m, cur, rec, next, group, res, d.steps, confirm, bus, outcomes, onCheckpoint)
		if err != nil {
			return Report{status: cur, record: rec, outcomes: outcomes}, err
		}
	}

	return Report{status: cur, record: rec, outcomes: outcomes}, nil
}

// fireCheckpoint calls onCheckpoint with a fresh Checkpoint built from
// cur, rec, and the OutcomeSucceeded entries of outcomes. A nil
// onCheckpoint skips the call; the loop pays no cost building the
// checkpoint value when the hook is nil.
func fireCheckpoint(onCheckpoint func(Checkpoint), cur machine.Status, rec machine.InOut, outcomes map[string]Outcome) {
	if onCheckpoint == nil {
		return
	}
	onCheckpoint(Checkpoint{Status: cur, Record: rec, Done: doneFrom(outcomes)})
}
