package flow_test

// Edge cases closing coverage gaps in runLoopedChild: a nil Guard
// reading as "always continue", a child workflow's own internal
// failure staying uncatchable at the parent level (matching a
// non-looped chained step's own child-failure rule), and a
// fireFromChild failure after a successful child run. See
// loop_test.go for the shared loopMachine and loopChild fixtures.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestLoopNilGuardAlwaysContinuesUntilMax proves a nil Guard reads as
// "always continue," matching machine.Guard's own nil convention: the
// loop runs until Max stops it, never asking a Guard.
func TestLoopNilGuardAlwaysContinuesUntilMax(t *testing.T) {
	t.Parallel()
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Max: 4}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "parent", flow.OutcomeSucceeded)
	if report.Record().Output != 4 {
		t.Fatalf("final counter = %v, want 4", report.Record().Output)
	}
}

// TestLoopChildOwnFailureUncatchableAtParent proves a loop step's
// child workflow's own internal failure aborts uncatchably at the
// parent level, even with a parent-level fallback declared: the
// stripped *failureError tag never matches resolveCatchable, mirroring
// a non-looped chained step's own child-failure rule.
func TestLoopChildOwnFailureUncatchableAtParent(t *testing.T) {
	t.Parallel()
	childErr := errors.New("child boom")
	badChild, err := flow.New([]flow.Step{
		{ID: "bad", To: string(statusA)},
	}, nil)
	if err != nil {
		t.Fatalf("child flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusA, Trigger: machine.Trigger("toA"),
			Guard: func(ctx context.Context) (bool, error) { return false, childErr }},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: badChild, Loop: &flow.LoopPolicy{}},
		{ID: "fallback", Needs: []string{"parent"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, childErr) {
		t.Fatalf("error does not wrap the child's own error: %v", err)
	}
	if !strings.Contains(err.Error(), `step "bad"`) {
		t.Fatalf("error %q should name the failing child step", err.Error())
	}
	if _, ok := report.Outcome("fallback"); ok {
		t.Fatal("\"fallback\" resolved despite the uncatchable child failure")
	}
}

// TestLoopFireFromChildFailureStopsLoop proves a loop step whose
// parent-level transition, fired after a successful child run, has no
// matching row aborts the loop with a step-scoped error naming the
// parent. This machine omits the statusB-to-statusA row loopMachine
// normally provides, so the child's own internal run keeps succeeding
// (it always starts fresh from statusStart) while the parent-level
// fireFromChild fails on the third iteration, once cur reaches
// statusB and loopChild's parity picks statusA again.
func TestLoopFireFromChildFailureStopsLoop(t *testing.T) {
	t.Parallel()
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusA, Trigger: machine.Trigger("toA")},
		machine.Transition{From: statusMid, To: statusB, Trigger: machine.Trigger("toB")},
		machine.Transition{From: statusStart, To: statusA, Trigger: machine.Trigger("outerA")},
		machine.Transition{From: statusA, To: statusB, Trigger: machine.Trigger("outerB")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no transition to status") {
		t.Fatalf("error %q should contain no-transition message", err.Error())
	}
	if !strings.Contains(err.Error(), `step "parent"`) {
		t.Fatalf("error %q should name the parent step", err.Error())
	}
	mustOutcome(t, report, "parent", flow.OutcomeFailed)
}
