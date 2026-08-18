package flow_test

// Red step: before phase 38, flow.LoopPolicy, Step.Loop, flow.LoopState,
// and flow.LoopStateFrom did not exist. This file did not compile:
// `go build ./flow/...` failed with "undefined: flow.LoopPolicy".
// LoopPolicy, LoopState, LoopStateFrom, runLoopChild, and
// runLoopedChild landed in flow/loop.go; the cases below then passed.
//
// machine rejects a self-loop transition (From == To), so a looped
// step's child cannot always return the same final status: the
// outer parent transition would need to fire from that status back
// to itself on every iteration after the first. loopChild works
// around this with a branch step whose Route alternates the child's
// final status between statusA and statusB, keyed off a counter
// shared across every call; loopMachine wires the matching ping-pong
// rows: statusStart to statusA, statusA to statusB, and statusB back
// to statusA. Each outer-facing row's OnEntry increments an int
// counter carried forward through rec.Output, so a test can observe
// the iteration count through LoopStateFrom.
//
// The LoopPolicy.Validate and New-validation cases live in
// loop_new_test.go, and the Run-level integration cases live in
// loop_integration_test.go, to keep this file at or below the
// 500-line structure cap.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

const (
	statusMid = machine.Status("mid")
	statusA   = machine.Status("a")
	statusB   = machine.Status("b")
)

// loopMachine builds the ping-pong machine every loop test in this
// file shares. statusStart to statusMid to statusA/statusB serves the
// child's own internal walk; statusStart to statusA, statusA to
// statusB, and statusB to statusA serve the parent's own repeated
// fireFromChild calls, alternating with loopChild's Route decision so
// no row ever fires From equal to To.
func loopMachine(t testing.TB) *machine.Definition {
	t.Helper()
	onEntry := func(ctx context.Context, rec *machine.InOut) error {
		n, _ := rec.Output.(int)
		rec.Output = n + 1
		return nil
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusA, Trigger: machine.Trigger("toA")},
		machine.Transition{From: statusMid, To: statusB, Trigger: machine.Trigger("toB")},
		machine.Transition{From: statusStart, To: statusA, Trigger: machine.Trigger("outerA"), OnEntry: onEntry},
		machine.Transition{From: statusA, To: statusB, Trigger: machine.Trigger("outerB"), OnEntry: onEntry},
		machine.Transition{From: statusB, To: statusA, Trigger: machine.Trigger("outerA"), OnEntry: onEntry},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// loopChild builds a child Definition whose final status alternates
// between statusA and statusB: a branch step routes to "toA" on an
// odd call of the shared parity counter and to "toB" on an even
// call, so the parent's repeated fireFromChild call never needs a
// self-loop row.
func loopChild(t testing.TB, parity *int32) *flow.Definition {
	t.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: string(statusMid), Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
			if atomic.AddInt32(parity, 1)%2 == 1 {
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
	return d
}

// countingGuard returns a machine.Guard-shaped closure that returns
// true until it has been called n times, then returns false, and a
// pointer to its call count.
func countingGuard(n int32) (func(context.Context) (bool, error), *int32) {
	var calls int32
	return func(ctx context.Context) (bool, error) {
		c := atomic.AddInt32(&calls, 1)
		return c < n, nil
	}, &calls
}

// TestLoopGuardFalseOnSecondCallRunsChildTwice proves a loop step
// whose Guard returns false on its second call runs its child
// workflow exactly twice and ends OutcomeSucceeded. It also asserts
// LoopStateFrom inside the second Guard call reports Iteration one
// and that call's record.
func TestLoopGuardFalseOnSecondCallRunsChildTwice(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	var gotIteration int
	var gotOutput any
	guard := func(ctx context.Context) (bool, error) {
		n := atomic.AddInt32(&guardCalls, 1)
		if n == 2 {
			state, ok := flow.LoopStateFrom(ctx)
			if !ok {
				t.Fatal("LoopStateFrom reported false inside Guard")
			}
			gotIteration = state.Iteration
			gotOutput = state.Record.Output
		}
		return n < 2, nil
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
	if guardCalls != 2 {
		t.Fatalf("guard calls = %d, want 2", guardCalls)
	}
	if gotIteration != 1 {
		t.Fatalf("Iteration at second Guard call = %d, want 1", gotIteration)
	}
	if gotOutput != 2 {
		t.Fatalf("Record.Output at second Guard call = %v, want 2", gotOutput)
	}
}

// TestLoopMaxStopsBeforeThirdGuardCall proves a loop step with Max
// two and a Guard that always returns true stops after two
// iterations, without a third Guard call: the Max cap decides before
// the loop would otherwise ask Guard again.
func TestLoopMaxStopsBeforeThirdGuardCall(t *testing.T) {
	t.Parallel()
	var guardCalls int32
	guard := func(ctx context.Context) (bool, error) {
		atomic.AddInt32(&guardCalls, 1)
		return true, nil
	}
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard, Max: 2}},
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
	if report.Record().Output != 2 {
		t.Fatalf("final counter = %v, want 2 (two iterations)", report.Record().Output)
	}
	// Max stops the loop after the second iteration, before a third
	// Guard call: the Guard runs only once, after the first iteration.
	if guardCalls != 1 {
		t.Fatalf("guard calls = %d, want 1 (the Max cap decides first)", guardCalls)
	}
}

// TestLoopMaxZeroRunsUntilGuardFalse proves a loop step with Max zero
// and a Guard that returns false on its fifth call runs exactly five
// iterations: zero imposes no engine ceiling.
func TestLoopMaxZeroRunsUntilGuardFalse(t *testing.T) {
	t.Parallel()
	guard, calls := countingGuard(5)
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
	if report.Record().Output != 5 {
		t.Fatalf("final counter = %v, want 5", report.Record().Output)
	}
	if atomic.LoadInt32(calls) != 5 {
		t.Fatalf("guard calls = %d, want 5", atomic.LoadInt32(calls))
	}
}

// TestLoopCtxCanceledMidLoopStopsBeforeNextChildRun proves a loop step
// with Max zero and a Guard that always returns true stops at the
// next ctx.Err() check once ctx is canceled, right after the third
// iteration's child workflow completes, before a fourth child run,
// and ends OutcomeFailed wrapping context.Canceled.
func TestLoopCtxCanceledMidLoopStopsBeforeNextChildRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var iterations int32
	guard := func(ctx context.Context) (bool, error) {
		if atomic.AddInt32(&iterations, 1) == 3 {
			cancel()
		}
		return true, nil
	}
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	report, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	mustOutcome(t, report, "parent", flow.OutcomeFailed)
	if report.Record().Output != 3 {
		t.Fatalf("final counter = %v, want 3 (stopped short of a fourth run)", report.Record().Output)
	}
}

// TestLoopGuardErrorOnFirstCallStopsAfterOneIteration proves a loop
// step whose Guard errors on its first call stops after one iteration
// and ends OutcomeFailed wrapping that error.
func TestLoopGuardErrorOnFirstCallStopsAfterOneIteration(t *testing.T) {
	t.Parallel()
	guardErr := errors.New("guard boom")
	guard := func(ctx context.Context) (bool, error) { return false, guardErr }
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, guardErr) {
		t.Fatalf("error does not wrap the guard error: %v", err)
	}
	mustOutcome(t, report, "parent", flow.OutcomeFailed)
	if report.Record().Output != 1 {
		t.Fatalf("final counter = %v, want 1 (exactly one iteration)", report.Record().Output)
	}
}

// TestLoopSecondIterationReceivesFirstIterationsOutput proves the
// loop step's second iteration receives the first iteration's output
// record as its input: the counter climbs by exactly one per
// iteration, never resetting.
func TestLoopSecondIterationReceivesFirstIterationsOutput(t *testing.T) {
	t.Parallel()
	var seen []int
	guard := func(ctx context.Context) (bool, error) {
		state, _ := flow.LoopStateFrom(ctx)
		n, _ := state.Record.Output.(int)
		seen = append(seen, n)
		return len(seen) < 2, nil
	}
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	if _, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []int{1, 2}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("seen = %v, want %v", seen, want)
	}
}

// TestLoopStateFromOutsideLoopStepReturnsFalse proves LoopStateFrom
// returns false and a zero LoopState outside any loop step's Guard
// call.
func TestLoopStateFromOutsideLoopStepReturnsFalse(t *testing.T) {
	t.Parallel()
	state, ok := flow.LoopStateFrom(context.Background())
	if ok {
		t.Fatal("LoopStateFrom reported true outside a loop step")
	}
	if state != (flow.LoopState{}) {
		t.Fatalf("LoopStateFrom returned %+v, want the zero value", state)
	}
}

// TestLoopCtxAlreadyCanceledAtEntryRunsZeroIterations proves a loop
// step whose ctx is already canceled at Run's entry runs zero child
// workflows: Run's own between-steps pause check (see Run's doc
// comment) intercepts before the loop step starts, so "parent" never
// resolves an Outcome at all, and the run aborts with the pinned
// pause error wrapping context.Canceled.
func TestLoopCtxAlreadyCanceledAtEntryRunsZeroIterations(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var guardCalls int32
	guard := func(ctx context.Context) (bool, error) {
		atomic.AddInt32(&guardCalls, 1)
		return true, nil
	}
	var parity int32
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: loopChild(t, &parity), Loop: &flow.LoopPolicy{Guard: guard}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := loopMachine(t)
	report, err := flow.Run(ctx, d, m, machine.InOut{}, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error does not wrap context.Canceled: %v", err)
	}
	if guardCalls != 0 {
		t.Fatalf("guard calls = %d, want 0", guardCalls)
	}
	if _, ok := report.Outcome("parent"); ok {
		t.Fatal("\"parent\" resolved an Outcome despite the pre-canceled ctx")
	}
}

// TestLoopExhaustedCtxWithFallbackContinues proves a loop step that
// exhausts ctx, mid-loop, with a phase 23 fallback declared continues
// the run down the fallback path, and FailureFrom inside the
// fallback returns the context error.
//
// The Guard reports context.Canceled directly, rather than canceling
// Run's own ctx: Run's between-steps pause check (see Run's doc
// comment) shares the same ctx and would otherwise pause the whole
// walk at the next step boundary, before the fallback ever gets a
// chance to run, regardless of whether runLoopedChild's own
// ctx.Err()-triggered failure was caught. Reporting the error through
// Guard, instead of canceling the shared ctx, isolates the case this
// test pins: a loop step's own ctx-flavored failure is catchable the
// same way a Guard error is.
func TestLoopExhaustedCtxWithFallbackContinues(t *testing.T) {
	t.Parallel()
	var gotErr error
	guard := func(ctx context.Context) (bool, error) {
		return false, context.Canceled
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusA, Trigger: machine.Trigger("toA")},
		machine.Transition{From: statusMid, To: statusB, Trigger: machine.Trigger("toB")},
		machine.Transition{From: statusStart, To: statusA, Trigger: machine.Trigger("outerA")},
		// The loop step fires exactly once (parity is always odd on the
		// first call, targeting statusA), then ctx cancels; the
		// fallback's row starts from statusA.
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
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("Failure.Err = %v, does not wrap context.Canceled", gotErr)
	}
}
