package flow

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// Failure is the failed step's context a fallback step receives.
// Step names the failed step. Err is that step's recorded error.
type Failure struct {
	Step string
	Err  error
}

// failureContextKey is the unexported key withFailure stores a
// Failure under.
type failureContextKey struct{}

// continueOrAbort turns a resolveCatchable or resolvePanelFailure
// result into the error advanceGroup returns: nil on handled, resolved
// otherwise. Factored out so advanceGroup's four call sites stay
// within the function-length gate.
func continueOrAbort(resolved error, handled bool) error {
	if handled {
		return nil
	}
	return resolved
}

// withFailure stores f in ctx under the failure context key. Run
// calls this before it fires a step admitted through a failed need.
func withFailure(ctx context.Context, f Failure) context.Context {
	return context.WithValue(ctx, failureContextKey{}, f)
}

// FailureFrom reads the failure context Run injects into a fallback
// step's Fire. The boolean is false outside a fallback firing.
func FailureFrom(ctx context.Context) (Failure, bool) {
	f, ok := ctx.Value(failureContextKey{}).(Failure)
	return f, ok
}

// handledFailure records one failed step's Failure and the set of
// unresolved AdmissionOnFailed step IDs that made the failure
// handled. A handler that runs, or a Route exclusion that removes
// the last handler, ends this entry's life; see prunePendingHandler
// and prunePendingOnRoute.
type handledFailure struct {
	fail     Failure
	handlers map[string]bool
}

// failureForStep scans step.Needs in declaration order and returns
// the first pending entry a need names, and true. No hit after the
// full scan returns the zero Failure and false. A nil pending map
// behaves like an empty one.
func failureForStep(step Step, pending map[string]*handledFailure) (Failure, bool) {
	for _, need := range step.Needs {
		if hf, ok := pending[need]; ok {
			return hf.fail, true
		}
	}
	return Failure{}, false
}

// failureKind distinguishes the three failure sources runSingleton's
// straight-line branch and its Sub branch can report. Only
// failureKindFire is catchable by a fallback.
type failureKind int

const (
	// failureKindFire marks an m.Fire failure. A fallback may catch it.
	failureKindFire failureKind = iota
	// failureKindConfirm marks a Confirm rejection. Never catchable.
	failureKindConfirm
	// failureKindTransition marks a pickTransition failure. Never
	// catchable: no fallback can fix a missing transition row.
	failureKindTransition
)

// failureError tags an error with the failure source that produced
// it. It satisfies error and unwraps to the underlying error, so
// errors.Is and errors.As still see through it.
type failureError struct {
	kind failureKind
	err  error
}

// newFailureError wraps err with kind.
func newFailureError(kind failureKind, err error) *failureError {
	return &failureError{kind: kind, err: err}
}

// Error returns the wrapped error's message.
func (e *failureError) Error() string { return e.err.Error() }

// Unwrap returns the wrapped error.
func (e *failureError) Unwrap() error { return e.err }

// pickTransitionFor picks step's transition row from rows targeting
// to. A miss or an ambiguity wraps as failureKindTransition.
func pickTransitionFor(step Step, rows []machine.Transition, to machine.Status) (machine.Transition, error) {
	row, err := pickTransition(rows, to)
	if err != nil {
		return machine.Transition{}, newFailureError(failureKindTransition, errorf("step %q: %w", step.ID, err))
	}
	return row, nil
}

// fireStep fires step's transition row. A Fire error wraps as
// failureKindFire.
func fireStep(
	ctx context.Context, m *machine.Definition, cur machine.Status,
	rec machine.InOut, step Step, row machine.Transition,
) (machine.Status, machine.InOut, error) {
	cur, rec, err := m.Fire(ctx, cur, row.Trigger, rec)
	if err != nil {
		return cur, rec, newFailureError(failureKindFire, errorf("step %q: %w", step.ID, err))
	}
	return cur, rec, nil
}

// confirmStep calls confirm for step. A rejection wraps as
// failureKindConfirm.
func confirmStep(ctx context.Context, confirm Confirm, step Step) error {
	if err := confirm(ctx, step); err != nil {
		return newFailureError(failureKindConfirm, errorf("step %q: ack not confirmed: %w", step.ID, err))
	}
	return nil
}

// admitsOnFailed evaluates the any-of admission rule AdmissionOnFailed
// uses over needs: verdictWait while any need is unresolved,
// verdictAdmit once at least one resolved need is OutcomeFailed, and
// verdictSkip when every need resolved and none failed.
func admitsOnFailed(needs []string, outcomes map[string]Outcome) verdict {
	anyFailed := false
	for _, need := range needs {
		o, ok := outcomes[need]
		if !ok {
			return verdictWait
		}
		if o == OutcomeFailed {
			anyFailed = true
		}
	}
	if anyFailed {
		return verdictAdmit
	}
	return verdictSkip
}

// failureHandlersFor returns the set of unresolved AdmissionOnFailed
// step IDs in steps whose Needs names failedID.
func failureHandlersFor(failedID string, steps []Step, outcomes map[string]Outcome) map[string]bool {
	handlers := map[string]bool{}
	for _, s := range steps {
		if _, resolved := outcomes[s.ID]; resolved {
			continue
		}
		if s.When != AdmissionOnFailed {
			continue
		}
		for _, need := range s.Needs {
			if need == failedID {
				handlers[s.ID] = true
				break
			}
		}
	}
	return handlers
}

// resolveCatchable decides whether err is a catchable failure for the
// step named failedID. Only a failureKindFire failure is catchable,
// and only when at least one unresolved AdmissionOnFailed step names
// failedID in its own Needs. A catchable failure writes a new pending
// entry and returns (nil, true); every other case returns (err,
// false).
func resolveCatchable(
	err error, failedID string, steps []Step, outcomes map[string]Outcome,
	pending map[string]*handledFailure,
) (error, bool) {
	var fe *failureError
	if !errors.As(err, &fe) || fe.kind != failureKindFire {
		return err, false
	}
	handlers := failureHandlersFor(failedID, steps, outcomes)
	if len(handlers) == 0 {
		return err, false
	}
	pending[failedID] = &handledFailure{
		fail:     Failure{Step: failedID, Err: fe.err},
		handlers: handlers,
	}
	return nil, true
}

// resolvePanelFailure marks every member of group OutcomeFailed, then
// evaluates the continue rule for the whole group. It returns (nil,
// true) only when every failed member has at least one
// AdmissionOnFailed dependent; it writes one pending entry per member
// in that case, all sharing err as their recorded Failure.Err.
// Otherwise it returns (err, false) and writes no pending entry.
func resolvePanelFailure(
	err error, group []Step, steps []Step, outcomes map[string]Outcome,
	pending map[string]*handledFailure,
) (error, bool) {
	markOutcome(outcomes, group, OutcomeFailed)
	handlersByMember := make(map[string]map[string]bool, len(group))
	for _, member := range group {
		handlers := failureHandlersFor(member.ID, steps, outcomes)
		if len(handlers) == 0 {
			return err, false
		}
		handlersByMember[member.ID] = handlers
	}
	for id, handlers := range handlersByMember {
		pending[id] = &handledFailure{
			fail:     Failure{Step: id, Err: err},
			handlers: handlers,
		}
	}
	return nil, true
}

// prunePendingHandler removes every pending entry whose handlers set
// contains handlerID. runSingletonAndMark calls this once a pending
// handler runs, whichever way it resolved: the failure it caught can
// never lose its only runner.
func prunePendingHandler(pending map[string]*handledFailure, handlerID string) {
	for failedID, hf := range pending {
		if hf.handlers[handlerID] {
			delete(pending, failedID)
		}
	}
}

// prunePendingOnRoute runs after a successful applyRoute call for
// next. For each direct dependent of next that applyRoute marked
// OutcomeSkipped, it removes that dependent's ID from every pending
// entry's handlers set. When an entry's handlers set becomes empty,
// prunePendingOnRoute returns that entry's recorded Failure.Err at
// once: a declared handler a Route exclusion removed would otherwise
// consume the failure silently.
func prunePendingOnRoute(
	next Step, steps []Step, outcomes map[string]Outcome, pending map[string]*handledFailure,
) error {
	deps := directDependents(next.ID, steps)
	for _, d := range deps {
		o, ok := outcomes[d.ID]
		if !ok || o != OutcomeSkipped {
			continue
		}
		for failedID, hf := range pending {
			if !hf.handlers[d.ID] {
				continue
			}
			delete(hf.handlers, d.ID)
			if len(hf.handlers) == 0 {
				delete(pending, failedID)
				return hf.fail.Err
			}
		}
	}
	return nil
}

// validateFailureAdmission rejects an AdmissionOnFailed step with no
// needs, and an AdmissionOnFailed step named in a panel.
func validateFailureAdmission(steps []Step, panels []Panel, ids map[string]int) error {
	for i := range steps {
		if steps[i].When == AdmissionOnFailed && len(steps[i].Needs) == 0 {
			return errorf("step %q admits on failure but needs nothing", steps[i].ID)
		}
	}
	for i, p := range panels {
		for _, id := range p {
			if steps[ids[id]].When == AdmissionOnFailed {
				return errorf("panel %d names failure-admitted step %q", i, id)
			}
		}
	}
	return nil
}
