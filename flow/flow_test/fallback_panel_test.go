package flow_test

// The panel, wave, chained-step, and route-related cases from
// fallback_test.go, split into this file to keep each file at or
// below the 500-line structure cap. See fallback_test.go's red-step
// note.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestWaveFailureFallbackForEveryMemberContinues proves a wave
// failure with a fallback for every failed member continues the run.
func TestWaveFailureFallbackForEveryMemberContinues(t *testing.T) {
	t.Parallel()
	const panelTo = machine.Status("panel-done")
	waveErr := errors.New("wave boom")
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
			Guard: rejectingGuard(waveErr)},
		machine.Transition{From: statusStart, To: machine.Status("fa"), Trigger: machine.Trigger("goFA")},
		machine.Transition{From: machine.Status("fa"), To: machine.Status("fb"), Trigger: machine.Trigger("goFB")},
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
	mustOutcome(t, report, "fallbackA", flow.OutcomeSucceeded)
	mustOutcome(t, report, "fallbackB", flow.OutcomeSucceeded)
}

// TestWaveFailureFallbackForOnlyOneMemberAbortsWithJoinedError proves
// a wave failure with a fallback for only one failed member aborts
// with the joined error: resolvePanelFailure requires every failed
// member to have a handler before it lets the run continue.
func TestWaveFailureFallbackForOnlyOneMemberAbortsWithJoinedError(t *testing.T) {
	t.Parallel()
	const panelTo = machine.Status("panel-done")
	waveErr := errors.New("wave boom")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(panelTo)},
		{ID: "b", To: string(panelTo)},
		{ID: "fallbackA", Needs: []string{"a"}, When: flow.AdmissionOnFailed, To: "fa"},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: panelTo, Trigger: triggerGo,
			Guard: rejectingGuard(waveErr)},
		machine.Transition{From: statusStart, To: machine.Status("fa"), Trigger: machine.Trigger("goFA")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, waveErr) {
		t.Fatalf("error does not wrap the wave error: %v", err)
	}
	mustOutcome(t, report, "a", flow.OutcomeFailed)
	mustOutcome(t, report, "b", flow.OutcomeFailed)
	if _, ok := report.Outcome("fallbackA"); ok {
		t.Fatal("\"fallbackA\" resolved despite the aborted, unhandled wave failure")
	}
}

// TestPanelSharedTransitionFailureUncatchable proves a panel's shared
// pickTransition failure, which fails before any member's Fire runs,
// aborts uncatchably even when every member has an AdmissionOnFailed
// fallback declared. No member's outcome is marked and no pending
// entry survives.
func TestPanelSharedTransitionFailureUncatchable(t *testing.T) {
	t.Parallel()
	const noMatch = machine.Status("nowhere")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(noMatch)},
		{ID: "b", To: string(noMatch)},
		{ID: "fallbackA", Needs: []string{"a"}, When: flow.AdmissionOnFailed, To: "fa"},
		{ID: "fallbackB", Needs: []string{"b"}, When: flow.AdmissionOnFailed, To: "fb"},
	}, []flow.Panel{{"a", "b"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("elsewhere"), Trigger: triggerGo},
		machine.Transition{From: statusStart, To: machine.Status("fa"), Trigger: machine.Trigger("goFA")},
		machine.Transition{From: statusStart, To: machine.Status("fb"), Trigger: machine.Trigger("goFB")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: panel:") {
		t.Fatalf("error %q should carry the panel wrap", err.Error())
	}
	if outcomes := report.Outcomes(); len(outcomes) != 0 {
		t.Fatalf("Outcomes() = %v, want empty: the pre-spawn failure marks no member", outcomes)
	}
}

// TestChainedStepChildFailureUncatchable proves a chained step's own
// nested Run call, failing inside its child workflow, aborts
// uncatchably even with a parent-level AdmissionOnFailed dependent
// declared for the chained step: runChild's returned error already
// exhausted the child's own continue logic and passes through
// unwrapped.
func TestChainedStepChildFailureUncatchable(t *testing.T) {
	t.Parallel()
	childErr := errors.New("child boom")
	child, err := flow.New([]flow.Step{
		{ID: "bad", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: child},
		{ID: "fallback", Needs: []string{"parent"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerGo,
			Guard: rejectingGuard(childErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var fe interface{ Unwrap() error }
	if errors.As(err, &fe) && !strings.Contains(err.Error(), `step "bad"`) {
		t.Fatalf("error %q should name the failing child step", err.Error())
	}
	if _, ok := report.Outcome("fallback"); ok {
		t.Fatal("\"fallback\" resolved despite the uncatchable chained-step failure")
	}
}

// TestRouteErrorWithFallbackContinuesFallbackPath proves a Route
// error on a branch step with a fallback continues down the fallback
// path.
func TestRouteErrorWithFallbackContinuesFallbackPath(t *testing.T) {
	t.Parallel()
	routeErr := errors.New("route boom")
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
			return nil, routeErr
		}},
		{ID: "left", Needs: []string{"branch"}, To: "l1"},
		{ID: "fallback", Needs: []string{"branch"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("l1"), Trigger: machine.Trigger("goL1")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("f"), Trigger: machine.Trigger("goF")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "branch", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	// left's default admission never accepts a failed need, so it
	// skips once branch's outcome goes terminal, the same way any
	// other dependent of a failed step skips once the failure is
	// handled.
	mustOutcome(t, report, "left", flow.OutcomeSkipped)
}

// TestFallbackMixedNeedsStillAdmits proves a fallback with mixed
// needs, one OutcomeFailed and one OutcomeSucceeded, still admits:
// AdmissionOnFailed is an any-of rule, unlike the all-of rule the
// other admission values keep.
func TestFallbackMixedNeedsStillAdmits(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "safe", To: "s"},
		{ID: "fallback", Needs: []string{"risky", "safe"}, When: flow.AdmissionOnFailed, To: "f"},
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

// TestFallbackOwnFireFailsNestedFallbackCatches proves a fallback
// step's own Fire can fail, and a second, nested fallback admitted on
// that failure lets the run complete: a fallback is an ordinary step
// with no restriction on declaring its own fallback.
func TestFallbackOwnFireFailsNestedFallbackCatches(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	fallbackErr := errors.New("fallback boom too")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "rescue", Needs: []string{"fallback"}, When: flow.AdmissionOnFailed, To: "res"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
			Guard: rejectingGuard(fallbackErr)},
		machine.Transition{From: statusStart, To: machine.Status("res"), Trigger: machine.Trigger("goRes")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "risky", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeFailed)
	mustOutcome(t, report, "rescue", flow.OutcomeSucceeded)
}

// TestAdmissionOnFailedStepAlsoRouteBranch proves an AdmissionOnFailed
// step may also be a Route branch step: its own Fire succeeds, Route
// then excludes one dependent, the kept dependent runs.
func TestAdmissionOnFailedStepAlsoRouteBranch(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f", Route: keeping("kept")},
		{ID: "kept", Needs: []string{"fallback"}, To: "k"},
		{ID: "dropped", Needs: []string{"fallback"}, To: "d"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(riskyErr)},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
		machine.Transition{From: machine.Status("f"), To: machine.Status("k"), Trigger: machine.Trigger("goK")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	mustOutcome(t, report, "kept", flow.OutcomeSucceeded)
	mustOutcome(t, report, "dropped", flow.OutcomeSkipped)
}

// TestTwoIndependentFailuresPendingDoesNotLeak proves two independent
// failed steps, each with its own fallback and its own pending-handler
// set, do not share bookkeeping: skipping the last handler of the
// first failure aborts the run, and the second failure's still-pending
// handler plays no part in that abort and never itself runs.
func TestTwoIndependentFailuresPendingDoesNotLeak(t *testing.T) {
	t.Parallel()
	errFirst := errors.New("first boom")
	errSecond := errors.New("second boom")
	var secondFallbackRan bool
	d, err := flow.New([]flow.Step{
		{ID: "first", To: "f1"},
		{ID: "branch", To: "b", Route: keeping("other")},
		{ID: "fallbackFirst", Needs: []string{"first", "branch"}, When: flow.AdmissionOnFailed, To: "ff"},
		{ID: "other", Needs: []string{"branch"}, To: "o"},
		{ID: "second", To: "f2"},
		{ID: "fallbackSecond", Needs: []string{"second"}, When: flow.AdmissionOnFailed, To: "fs"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("f1"), Trigger: machine.Trigger("go1"),
			Guard: rejectingGuard(errFirst)},
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("o"), Trigger: machine.Trigger("goO")},
		machine.Transition{From: statusStart, To: machine.Status("f2"), Trigger: machine.Trigger("go2"),
			Guard: rejectingGuard(errSecond)},
		machine.Transition{From: statusStart, To: machine.Status("fs"), Trigger: machine.Trigger("goFS"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				secondFallbackRan = true
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errFirst) {
		t.Fatalf("error does not wrap the first failure's recorded error: %v", err)
	}
	if secondFallbackRan {
		t.Fatal("fallbackSecond ran despite the run aborting on the first failure's handler skip")
	}
	if _, ok := report.Outcome("fallbackSecond"); ok {
		t.Fatal("\"fallbackSecond\" resolved despite never running")
	}
}

// TestRouteSkipLeavesUnrelatedPendingHandlerUntouched proves
// prunePendingOnRoute's inner scan skips a pending entry whose
// handlers set does not name the Route-skipped dependent: an
// unrelated failure's fallback keeps its pending entry and still runs
// to completion after the unrelated Route event. Pins that
// prunePendingOnRoute only ever removes the skipped dependent from
// the entries that actually declare it as a handler.
func TestRouteSkipLeavesUnrelatedPendingHandlerUntouched(t *testing.T) {
	t.Parallel()
	otherErr := errors.New("other boom")
	d, err := flow.New([]flow.Step{
		{ID: "other", To: "o"},
		{ID: "branch", To: "b", Route: keeping("kept")},
		{ID: "otherFallback", Needs: []string{"other"}, When: flow.AdmissionOnFailed, To: "of"},
		{ID: "kept", Needs: []string{"branch"}, To: "k"},
		{ID: "dropped", Needs: []string{"branch"}, To: "d"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("o"), Trigger: machine.Trigger("goO"),
			Guard: rejectingGuard(otherErr)},
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("of"), Trigger: machine.Trigger("goOF")},
		machine.Transition{From: machine.Status("of"), To: machine.Status("k"), Trigger: machine.Trigger("goK")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "other", flow.OutcomeFailed)
	mustOutcome(t, report, "otherFallback", flow.OutcomeSucceeded)
	mustOutcome(t, report, "kept", flow.OutcomeSucceeded)
	mustOutcome(t, report, "dropped", flow.OutcomeSkipped)
}

// TestOneMemberPanelFailureCaughtByFallback proves a step that is the
// sole member of a one-member Panel follows the same catchable-failure
// path as an ordinary singleton step: advanceGroup's scanPanel case,
// for a one-member group, calls resolveCatchable exactly like
// scanSingleton does, and a declared fallback catches the failure the
// same way.
func TestOneMemberPanelFailureCaughtByFallback(t *testing.T) {
	t.Parallel()
	riskyErr := errors.New("risky boom")
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
	}, []flow.Panel{{"risky"}})
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
}
