package flow

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// waveResult carries one panel member's Fire outcome.
type waveResult struct {
	step Step
	to   machine.Status
	rec  machine.InOut
	err  error
}

// runWave resolves the shared transition row once, before it spawns
// any goroutine: validatePanels' homogeneity rule guarantees every
// member of group shares one To, so this pick cannot fail for one
// member and succeed for a sibling. A failure here fails the whole
// group before any goroutine spawns: no member's Guard, OnExit, or
// OnEntry runs; it tags as failureKindTransition so advanceGroup's
// scanPanel case routes it straight to an uncatchable abort, distinct
// from a per-member Fire failure. Every member then fires row.Trigger
// from its own InOut copy, concurrently. A member Fire failure joins
// with its siblings' failures through errors.Join; cur and rec stay
// at their pre-wave values, and the caller must not mark any member
// done. On success runWave returns the first group member's result,
// by declaration order, looked up by ID after every goroutine
// finishes.
func runWave(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, group []Step,
) (machine.Status, machine.InOut, error) {
	rows := m.AllowedTransitions(cur)
	to := machine.Status(group[0].To)
	row, err := pickTransition(rows, to)
	if err != nil {
		return cur, rec, newFailureError(failureKindTransition, errorf("panel: %w", err))
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
