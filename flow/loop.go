package flow

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// LoopPolicy is a step's loop rule for its Sub child workflow.
// Guard reuses machine.Guard's exact type; a nil Guard means "always
// continue," matching machine's own nil convention. Max caps the
// iteration count; zero means unbounded, bounded only by the
// caller's own ctx. A negative Max is invalid.
type LoopPolicy struct {
	Guard machine.Guard
	Max   int
}

// Validate rejects Max < 0 with the pinned message
// "flow: loop: max must be at least 0". Validate has no step ID to
// report, so its message names no step; New builds a step-scoped
// message through loopValidateMessage.
func (p LoopPolicy) Validate() error {
	if msg := loopValidateMessage(p); msg != "" {
		return errorf("loop: %s", msg)
	}
	return nil
}

// loopValidateMessage returns the unprefixed Validate failure message
// for p, or "" when p is valid. Both Validate and validateLoop build
// their pinned error text from this one check, so the rule lives in
// one place.
func loopValidateMessage(p LoopPolicy) string {
	if p.Max < 0 {
		return "max must be at least 0"
	}
	return ""
}

// LoopState is the loop context a Guard closure reads. Iteration
// counts completed iterations, starting at zero before the first
// Guard call. Record carries the most recent child workflow's
// output.
type LoopState struct {
	Iteration int
	Record    machine.InOut
}

// loopContextKey is the unexported key withLoopState stores a
// LoopState under.
type loopContextKey struct{}

// withLoopState stores s in ctx under the loop state context key.
// runLoopedChild calls this before each Guard evaluation.
func withLoopState(ctx context.Context, s LoopState) context.Context {
	return context.WithValue(ctx, loopContextKey{}, s)
}

// LoopStateFrom reads the LoopState runLoopedChild injects before
// each Guard call of a loop step. The boolean is false outside a
// loop step's Guard evaluation, matching FailureFrom's shape.
func LoopStateFrom(ctx context.Context) (LoopState, bool) {
	s, ok := ctx.Value(loopContextKey{}).(LoopState)
	return s, ok
}

// runChild runs a chained step's child workflow to completion, with
// the same machine definition, a fresh InOut, and the same confirm.
// A chained step's own child workflow reports no checkpoint; nested
// resumability is a future phase's concern.
func runChild(
	ctx context.Context, child *Definition, m *machine.Definition, confirm Confirm,
) (machine.Status, error) {
	report, err := Run(ctx, child, m, machine.InOut{}, confirm, nil, nil)
	return report.Status(), err
}

// runLoopChild is runChild's loop-aware variant: it starts the child
// workflow from start instead of a fresh machine.InOut{}, so a
// looped step's later iteration can carry the previous iteration's
// output forward.
func runLoopChild(
	ctx context.Context, child *Definition, m *machine.Definition, confirm Confirm,
	start machine.InOut,
) (machine.Status, error) {
	report, err := Run(ctx, child, m, start, confirm, nil, nil)
	return report.Status(), err
}

// runLoopedChild drives step.Loop's iteration. It runs step.Sub
// through runLoopChild, starting from rec on the first iteration and
// from the previous iteration's output record thereafter, then fires
// the parent's own transition through fireFromChild. ctx gates every
// iteration's cancellation check and every Guard call; fireCtx is the
// context fireFromChild fires with, carrying a Failure when step
// itself is admitted through a failed need.
//
// Before every iteration, including the first, a non-nil ctx.Err()
// stops the loop at once and fails the step, wrapped
// "flow: step %q: %w", tagged failureKindFire so a declared fallback
// can catch it. A Guard error stops the loop the same way. Max,
// when non-zero, stops the loop once reached, without a further
// Guard call. A false Guard result stops the loop as a normal,
// successful exit.
func runLoopedChild(
	ctx, fireCtx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, confirm Confirm,
) (machine.Status, machine.InOut, error) {
	policy := step.Loop
	start := rec
	iteration := 0
	for {
		if err := ctx.Err(); err != nil {
			return cur, rec, newFailureError(failureKindFire, errorf("step %q: %w", step.ID, err))
		}
		childStatus, err := runLoopChild(ctx, step.Sub, m, confirm, start)
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
		cur, rec, err = fireFromChild(fireCtx, m, cur, rec, step, childStatus)
		if err != nil {
			return cur, rec, err
		}
		guardCtx := withLoopState(ctx, LoopState{Iteration: iteration, Record: rec})
		iteration++
		start = rec
		if policy.Max != 0 && iteration >= policy.Max {
			return cur, rec, nil
		}
		if policy.Guard == nil {
			continue
		}
		cont, gerr := policy.Guard(guardCtx)
		if gerr != nil {
			return cur, rec, newFailureError(failureKindFire, errorf("step %q: %w", step.ID, gerr))
		}
		if !cont {
			return cur, rec, nil
		}
	}
}

// validateLoop rejects a LoopPolicy with Max < 0, a Loop set on a
// step with a nil Sub, and a Loop set on a panel member.
func validateLoop(steps []Step, panels []Panel, ids map[string]int) error {
	for i := range steps {
		if steps[i].Loop == nil {
			continue
		}
		if msg := loopValidateMessage(*steps[i].Loop); msg != "" {
			return errorf("step %q loop: %s", steps[i].ID, msg)
		}
		if steps[i].Sub == nil {
			return errorf("step %q has a loop policy but no sub-workflow", steps[i].ID)
		}
	}
	for i, p := range panels {
		for _, id := range p {
			if steps[ids[id]].Loop != nil {
				return errorf("panel %d names looped step %q", i, id)
			}
		}
	}
	return nil
}
