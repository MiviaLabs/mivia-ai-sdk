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
	"sync/atomic"
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

// TestLoopChildStartsFromCarriedRecordNotFreshInOut proves
// runLoopedChild's first child run starts from the loop step's
// incoming record, and every later iteration starts from the
// previous iteration's post-fire record, rather than a fresh
// machine.InOut{} each time. The child's own Route reads
// rec.Output's parity, instead of loopChild's atomic parity counter,
// so the branch decision only lines up with loopMachine's ping-pong
// rows if the incoming record actually reached the child. A build
// that started every iteration from a fresh machine.InOut{} would
// route to statusB on the first iteration too (nil reads as even),
// and loopMachine has no statusStart-to-statusB row, so that bug
// aborts the run with a transition error instead of completing.
func TestLoopChildStartsFromCarriedRecordNotFreshInOut(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "branch", To: string(statusMid), Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
			n, _ := rec.Output.(int)
			if n%2 == 1 {
				return []string{"toA"}, nil
			}
			return []string{"toB"}, nil
		}},
		{ID: "toA", Needs: []string{"branch"}, To: string(statusA)},
		{ID: "toB", Needs: []string{"branch"}, To: string(statusB)},
	}, nil)
	if err != nil {
		t.Fatalf("child flow.New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: child, Loop: &flow.LoopPolicy{Max: 2}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	report, err := flow.Run(context.Background(), d, m, machine.InOut{Output: 1}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "parent", flow.OutcomeSucceeded)
	if report.Status() != statusB {
		t.Fatalf("final status = %q, want %q", report.Status(), statusB)
	}
	if report.Record().Output != 3 {
		t.Fatalf("final counter = %v, want 3", report.Record().Output)
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

// TestLoopFireFromChildFailureWithFallbackContinues proves a loop
// step's fireFromChild failure is catchable by a declared
// AdmissionOnFailed fallback, the same way a Guard error or a
// ctx-exhaustion failure already is: fireFromChild reuses fireStep,
// tagging a rejected transition failureKindFire, identically to the
// straight-line, non-looped path. A missing-row failure, by contrast,
// tags failureKindTransition and stays uncatchable; this test uses a
// Guard rejection on the parent-level row instead, so the row exists
// and m.Fire itself fails.
//
// The loop's first iteration's own child run succeeds (parity picks
// statusA on its first, odd call), but the parent-level
// statusStart-to-statusA row's Guard always rejects, so fireFromChild
// fails before any loop Guard call.
func TestLoopFireFromChildFailureWithFallbackContinues(t *testing.T) {
	t.Parallel()
	var gotErr error
	fireErr := errors.New("parent transition rejected")
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusA, Trigger: machine.Trigger("toA")},
		machine.Transition{From: statusMid, To: statusB, Trigger: machine.Trigger("toB")},
		machine.Transition{From: statusStart, To: statusA, Trigger: machine.Trigger("outerA"),
			Guard: func(ctx context.Context) (bool, error) { return false, fireErr }},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
			OnEntry: func(ctx context.Context, rec *machine.InOut) error {
				fail, _ := flow.FailureFrom(ctx)
				gotErr = fail.Err
				return nil
			}},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var parity int32
	var guardCalls int32
	guard := func(ctx context.Context) (bool, error) {
		atomic.AddInt32(&guardCalls, 1)
		return true, nil
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
		{ID: "fallback", Needs: []string{"parent"}, When: flow.AdmissionOnFailed, To: "f"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "parent", flow.OutcomeFailed)
	mustOutcome(t, report, "fallback", flow.OutcomeSucceeded)
	if guardCalls != 0 {
		t.Fatalf("guard calls = %d, want 0 (fireFromChild fails before any Guard call)", guardCalls)
	}
	if !errors.Is(gotErr, fireErr) {
		t.Fatalf("Failure.Err = %v, does not wrap the rejected transition's error", gotErr)
	}
}
