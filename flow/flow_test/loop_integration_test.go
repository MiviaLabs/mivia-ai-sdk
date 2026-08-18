package flow_test

// Integration cases for the loop driver, run end to end through
// flow.Run. See loop_test.go for the shared loopMachine and loopChild
// fixtures, and for why the second child step's target alternates
// between statusA and statusB instead of self-looping.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestLoopIntegrationCounterStopsAtThree runs a looped step end to
// end. The child workflow's parent-level transition moves a counter
// in Record.Output by one per iteration; Guard reads LoopStateFrom to
// stop once the counter reaches three. Asserts the final record, the
// final status, and Iteration at each Guard call.
func TestLoopIntegrationCounterStopsAtThree(t *testing.T) {
	t.Parallel()
	var iterationsSeen []int
	guard := func(ctx context.Context) (bool, error) {
		state, ok := flow.LoopStateFrom(ctx)
		if !ok {
			t.Fatal("LoopStateFrom reported false inside Guard")
		}
		iterationsSeen = append(iterationsSeen, state.Iteration)
		n, _ := state.Record.Output.(int)
		return n < 3, nil
	}
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
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
	if report.Record().Output != 3 {
		t.Fatalf("final counter = %v, want 3", report.Record().Output)
	}
	// statusStart -> statusA -> statusB -> statusA: three iterations,
	// alternating parity per loopChild's Route decision.
	if report.Status() != statusA {
		t.Fatalf("final status = %q, want %q", report.Status(), statusA)
	}
	wantIterations := []int{0, 1, 2}
	if len(iterationsSeen) != len(wantIterations) {
		t.Fatalf("iterationsSeen = %v, want %v", iterationsSeen, wantIterations)
	}
	for i := range wantIterations {
		if iterationsSeen[i] != wantIterations[i] {
			t.Fatalf("iterationsSeen = %v, want %v", iterationsSeen, wantIterations)
		}
	}
}

// TestLoopIntegrationDeadlineAbortsWithNoMaxStop runs a looped step
// with Max zero and a short ctx deadline. It asserts the run aborts
// once the deadline passes, with no Max-driven stop: the recorded
// iteration count at abort time never hits an engine ceiling, since
// Max is zero.
func TestLoopIntegrationDeadlineAbortsWithNoMaxStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	guard := func(ctx context.Context) (bool, error) { return true, nil }
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	_, err = flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error does not wrap context.DeadlineExceeded: %v", err)
	}
}

// TestLoopIntegrationDeadlineWithFallbackContinues combines a loop
// step with a phase 23 fallback that catches the deadline failure.
//
// The Guard reports context.DeadlineExceeded directly, rather than
// letting Run's ctx actually expire: Run's between-steps pause check
// (see Run's doc comment) shares the same ctx passed to the loop step,
// and would otherwise pause the whole walk at the next step boundary
// once that ctx genuinely expires, before the fallback ever gets a
// chance to run. Reporting the error through Guard isolates the case
// this test pins: a loop step's deadline-flavored failure is
// catchable the same way any other Guard error is.
func TestLoopIntegrationDeadlineWithFallbackContinues(t *testing.T) {
	t.Parallel()
	var gotErr error
	guard := func(ctx context.Context) (bool, error) { return false, context.DeadlineExceeded }
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusA, Trigger: machine.Trigger("toA")},
		machine.Transition{From: statusMid, To: statusB, Trigger: machine.Trigger("toB")},
		machine.Transition{From: statusStart, To: statusA, Trigger: machine.Trigger("outerA")},
		machine.Transition{From: statusA, To: machine.Status("f"), Trigger: machine.Trigger("goF"),
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
	if !errors.Is(gotErr, context.DeadlineExceeded) {
		t.Fatalf("Failure.Err = %v, does not wrap context.DeadlineExceeded", gotErr)
	}
}
