package flow_test

// Red step: before phase 23, flow.AdmissionOnFailed and
// flow.FailureFrom did not exist. This file did not compile:
// `go build ./flow/...` failed with "undefined: flow.AdmissionOnFailed"
// and "undefined: flow.FailureFrom". admitsOnFailed, withFailure,
// FailureFrom, and the continue rule landed in flow/failure.go and
// flow/runner.go; the cases below then passed.
//
// The New-validation cases live in fallback_new_test.go. The panel,
// wave, chained-step, and route-related cases live in
// fallback_panel_test.go, to keep each file at or below the 500-line
// structure cap.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// rejectingGuard returns a machine.Guard that always fails Fire with
// err.
func rejectingGuard(err error) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		return false, err
	}
}

// TestFailedStepWithFallbackLetsRunComplete proves a fallback admitted
// on a failed need lets the run complete: the failed step, the
// fallback, and the join all resolve.
func TestFailedStepWithFallbackLetsRunComplete(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "join", Needs: []string{"fallback"}, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
		machine.Transition{From: machine.Status("f"), To: machine.Status("j"), Trigger: machine.Trigger("goJ")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	mustOutcome(t, report, "join", flow.OutcomeSucceeded)
	if report.Status() != machine.Status("j") {
		t.Fatalf("status = %q, want %q", report.Status(), "j")
	}
}

// TestFailureFromInsideFallbackOnEntry proves FailureFrom, called
// inside the fallback's OnEntry, returns the failed step's ID and a
// wrapped error that satisfies errors.Is.
func TestFailureFromInsideFallbackOnEntry(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	var gotStep string
	var gotErr error
	var gotOK bool
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				fail, ok := flow.FailureFrom(ctx)
				gotStep, gotErr, gotOK = fail.Step, fail.Err, ok
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !gotOK {
		t.Fatal("FailureFrom reported false inside the fallback's OnEntry")
	}
	if gotStep != "risky" {
		t.Fatalf("Failure.Step = %q, want %q", gotStep, "risky")
	}
	if !errors.Is(gotErr, riskyErr) {
		t.Fatalf("Failure.Err = %v, does not wrap %v", gotErr, riskyErr)
	}
}

// TestFallbackTwoFailedNeedsFirstInDeclarationOrder proves a fallback
// with two failed needs receives the first failed need in Needs
// declaration order through Failure.Step.
func TestFallbackTwoFailedNeedsFirstInDeclarationOrder(t *testing.T) {
	t.Parallel()
	errA := errors.New("a boom")
	errB := errors.New("b boom")
	var gotStep string
	d, err := flow.New([]flow.Step{
		{ID: "a", To: "a"},
		{ID: "b", To: "b"},
		{ID: "fallback", Needs: []string{"a", "b"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("a"), Trigger: machine.Trigger("goA"),
			Guard: rejectingGuard(errA)},
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB"),
			Guard: rejectingGuard(errB)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				fail, _ := flow.FailureFrom(ctx)
				gotStep = fail.Step
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "a", flow.OutcomeFailed)
	mustOutcome(t, report, "b", flow.OutcomeFailed)
	if gotStep != "a" {
		t.Fatalf("Failure.Step = %q, want %q (first in Needs declaration order)", gotStep, "a")
	}
}

// TestFallbackNeedsErrorFreeWaveMemberGetsJoinedError proves a
// fallback that needs an error-free wave member still receives the
// joined wave error through Failure.Err: resolvePanelFailure marks
// every member of a mixed wave OutcomeFailed with the same recorded
// error, whether or not that member's own Fire errored.
func TestFallbackNeedsErrorFreeWaveMemberGetsJoinedError(t *testing.T) {
	t.Parallel()
	errMixedMember := errors.New("mixed member failed")
	const panelTo = machine.Status("panel-done")
	var calls int64
	var gotErr error
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(panelTo)},
		{ID: "b", To: string(panelTo)},
		{ID: "fallbackA", Needs: []string{"a"}, When: flow.AdmissionOnFailed, To: "fa"},
		{ID: "fallbackB", Needs: []string{"b"}, When: flow.AdmissionOnFailed, To: "fb"},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) {
				if atomic.AddInt64(&calls, 1) == 1 {
					return true, nil
				}
				return false, errMixedMember
			}},
		machine.Transition{From: statusStart, To: machine.Status("fa"), Trigger: machine.Trigger("goFA")},
		machine.Transition{From: machine.Status("fa"), To: machine.Status("fb"), Trigger: machine.Trigger("goFB"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				fail, _ := flow.FailureFrom(ctx)
				gotErr = fail.Err
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "a", flow.OutcomeFailed)
	mustOutcome(t, report, "b", flow.OutcomeFailed)
	if !errors.Is(gotErr, errMixedMember) {
		t.Fatalf("Failure.Err = %v, does not wrap the joined wave error %v", gotErr, errMixedMember)
	}
}

// TestFailureFromHappyPathReturnsFalse proves FailureFrom returns
// false inside a happy-path step never admitted through a failed
// need.
func TestFailureFromHappyPathReturnsFalse(t *testing.T) {
	t.Parallel()
	var gotOK bool
	d, err := flow.New([]flow.Step{
		{ID: "solo", To: "s"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("s"), Trigger: triggerGo,
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				_, gotOK = flow.FailureFrom(ctx)
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotOK {
		t.Fatal("FailureFrom reported true inside a happy-path step")
	}
}

// TestFailedStepNoFallbackAborts proves a failed step with no
// fallback aborts the run with the standard step-scoped error wording
// this package has used since before panels or routing existed.
func TestFailedStepNoFallbackAborts(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "risky": risky boom`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
}

// TestAdmissionOnFailedAllSucceededSkips proves an AdmissionOnFailed
// step whose needs all succeeded becomes OutcomeSkipped, and its own
// dependents follow normal admission.
func TestAdmissionOnFailedAllSucceededSkips(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "after", Needs: []string{"fallback"}, To: "a"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR")},
		machine.Transition{From: machine.Status("r"), To: machine.Status("a"), Trigger: machine.Trigger("goA")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "risky", flow.OutcomeSucceeded)
	mustOutcome(t, report, "fallback", flow.OutcomeSkipped)
	mustOutcome(t, report, "after", flow.OutcomeSucceeded)
}

// TestHappyPathDependentSkippedWhenFailureHandled proves a happy-path
// dependent of the failed step (default AdmissionOnFinished) becomes
// OutcomeSkipped when the failure is handled, since a failed need
// never satisfies the default admission rule.
func TestHappyPathDependentSkippedWhenFailureHandled(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "happy", Needs: []string{"risky"}, To: "h"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	mustOutcome(t, report, "happy", flow.OutcomeSkipped)
}

// TestBranchLeavesSoleHandlerUnchosenAbortsWithStepError proves a
// branch step that leaves the sole handler of a handled failure
// unchosen aborts the run with the recorded step error: the
// handler-skip re-check preserves fail-fast.
func TestBranchLeavesSoleHandlerUnchosenAbortsWithStepError(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "branch", To: "b", Route: keeping("other")},
		{ID: "fallback", Needs: []string{"risky", "branch"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "other", Needs: []string{"branch"}, To: "o"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("o"), Trigger: machine.Trigger("goO")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, riskyErr) {
		t.Fatalf("error does not wrap the recorded step error: %v", err)
	}
	// applyRoute already marked fallback OutcomeSkipped before the
	// handler-skip re-check aborts the run: the re-check catches the
	// silent loss of the failure's sole handler after the fact.
	mustOutcome(t, report, "fallback", flow.OutcomeSkipped)
	if _, ok := report.Outcome("other"); ok {
		t.Fatal("\"other\" resolved despite the abort")
	}
}

// TestConfirmRejectionAbortsDespiteFallback proves a Confirm
// rejection aborts even when a fallback exists: failureKindConfirm is
// never catchable.
func TestConfirmRejectionAbortsDespiteFallback(t *testing.T) {
	t.Parallel()
	confirmErr := errors.New("confirm rejected")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	confirm := func(ctx context.Context, step flow.Step) error {
		if step.ID == "risky" {
			return confirmErr
		}
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, confirmErr) {
		t.Fatalf("error does not wrap the confirm error: %v", err)
	}
	if _, ok := report.Outcome("fallback"); ok {
		t.Fatal("\"fallback\" resolved despite the uncatchable confirm rejection")
	}
}

// TestFallbackTwoNeedsWaitsForBothBeforeAdmitting proves a fallback
// with two needs, declared between them, stays at verdictWait once its
// first need fails and its second need is still unresolved: it runs
// only after both needs settle, and it still catches the failure once
// they do. admitsOnFailed returns verdictWait, not a premature
// verdict, while any need has not yet resolved: this pins the
// unresolved-need branch nextReadyGroup's declaration-order scan does
// not otherwise reach, because a later step with no needs of its own
// always resolves before the scan revisits a still-waiting fallback.
func TestFallbackTwoNeedsWaitsForBothBeforeAdmitting(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky", "safe"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "safe", To: "s"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("s"), Trigger: machine.Trigger("goS")},
		machine.Transition{From: machine.Status("s"), To: machine.Status("f"), Trigger: machine.Trigger("goF")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
	mustOutcome(t, report, "safe", flow.OutcomeSucceeded)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
}
