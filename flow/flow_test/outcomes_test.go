package flow_test

// Red step: before phase 21, flow.Report and flow.Outcome were
// undefined symbols. `go test ./flow/...` failed with
// "undefined: flow.Report" and "undefined: flow.Outcome". Outcome and
// Report landed in flow/outcome.go; Run's signature change landed in
// flow/runner.go. The cases below then passed.

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// TestReportLinearThreeStepsAllSucceed proves a linear three-step run
// reports every step OutcomeSucceeded, and Status and Record match
// the values the pre-phase-21 signature returned.
func TestReportLinearThreeStepsAllSucceed(t *testing.T) {
	t.Parallel()
	const (
		s1 = machine.Status("s1")
		s2 = machine.Status("s2")
		s3 = machine.Status("s3")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(s1)},
		{ID: "b", Needs: []string{"a"}, To: string(s2)},
		{ID: "c", Needs: []string{"b"}, To: string(s3)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: s1, Trigger: triggerGo},
		machine.Transition{From: s1, To: s2, Trigger: triggerGo},
		machine.Transition{From: s2, To: s3, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status() != s3 {
		t.Fatalf("Status() = %q, want %q", report.Status(), s3)
	}
	for _, id := range []string{"a", "b", "c"} {
		outcome, ok := report.Outcome(id)
		if !ok || outcome != flow.OutcomeSucceeded {
			t.Fatalf("Outcome(%q) = %v, %v, want %v, true", id, outcome, ok, flow.OutcomeSucceeded)
		}
	}
}

// TestReportMidGraphFireFailureMarksFailed proves a mid-graph Fire
// failure reports earlier steps OutcomeSucceeded, the failing step
// OutcomeFailed, and leaves later steps unresolved.
func TestReportMidGraphFireFailureMarksFailed(t *testing.T) {
	t.Parallel()
	const (
		s1 = machine.Status("s1")
		s2 = machine.Status("s2")
	)
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(s1)},
		{ID: "b", Needs: []string{"a"}, To: string(s2)},
		{ID: "c", Needs: []string{"b"}, To: "unreachable"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: s1, Trigger: triggerGo},
		machine.Transition{From: s1, To: s2, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if outcome, ok := report.Outcome("a"); !ok || outcome != flow.OutcomeSucceeded {
		t.Fatalf("Outcome(a) = %v, %v, want %v, true", outcome, ok, flow.OutcomeSucceeded)
	}
	if outcome, ok := report.Outcome("b"); !ok || outcome != flow.OutcomeSucceeded {
		t.Fatalf("Outcome(b) = %v, %v, want %v, true", outcome, ok, flow.OutcomeSucceeded)
	}
	if outcome, ok := report.Outcome("c"); !ok || outcome != flow.OutcomeFailed {
		t.Fatalf("Outcome(c) = %v, %v, want %v, true", outcome, ok, flow.OutcomeFailed)
	}
}

// TestReportConfirmRejectionMarksFailed proves a Confirm rejection
// marks the rejected step OutcomeFailed.
func TestReportConfirmRejectionMarksFailed(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	rejectErr := errors.New("ack rejected")
	confirm := func(ctx context.Context, step flow.Step) error { return rejectErr }
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if outcome, ok := report.Outcome("a"); !ok || outcome != flow.OutcomeFailed {
		t.Fatalf("Outcome(a) = %v, %v, want %v, true", outcome, ok, flow.OutcomeFailed)
	}
}

// TestReportNilDReturnsCallerRecord proves a nil d returns the pinned
// error and a Report holding the zero Status and the caller's in,
// not a zero-value InOut.
func TestReportNilDReturnsCallerRecord(t *testing.T) {
	t.Parallel()
	m := singleTransitionMachine(t)
	in := machine.InOut{Input: "caller-value"}
	report, err := flow.Run(context.Background(), nil, m, in, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if report.Status() != machine.Status("") {
		t.Fatalf("Status() = %q, want the zero Status", report.Status())
	}
	if report.Record() != in {
		t.Fatalf("Record() = %+v, want the caller's in %+v", report.Record(), in)
	}
}

// TestReportNilMReturnsCallerRecord proves a nil m returns the pinned
// error and a Report holding the zero Status and the caller's in.
func TestReportNilMReturnsCallerRecord(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	in := machine.InOut{Input: "caller-value"}
	report, err := flow.Run(context.Background(), d, nil, in, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if report.Status() != machine.Status("") {
		t.Fatalf("Status() = %q, want the zero Status", report.Status())
	}
	if report.Record() != in {
		t.Fatalf("Record() = %+v, want the caller's in %+v", report.Record(), in)
	}
}

// TestReportNilDAndMKeepsDError proves the d-nil error wins when both
// d and m are nil, and Run never panics.
func TestReportNilDAndMKeepsDError(t *testing.T) {
	t.Parallel()
	in := machine.InOut{Input: "caller-value"}
	report, err := flow.Run(context.Background(), nil, nil, in, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if report.Record() != in {
		t.Fatalf("Record() = %+v, want the caller's in %+v", report.Record(), in)
	}
}

// TestReportNilConfirmReturnsInitialStatus proves a nil confirm returns
// the pinned error and a Report holding the initial status and the
// incoming record.
func TestReportNilConfirmReturnsInitialStatus(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	in := machine.InOut{Input: "caller-value"}
	report, err := flow.Run(context.Background(), d, m, in, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if report.Status() != statusStart {
		t.Fatalf("Status() = %q, want the initial status %q", report.Status(), statusStart)
	}
	if report.Record() != in {
		t.Fatalf("Record() = %+v, want the caller's in %+v", report.Record(), in)
	}
}

// TestReportOutcomesMapMutationIsolated proves mutating the map
// Outcomes returns never changes a later Outcome call on the same
// Report.
func TestReportOutcomesMapMutationIsolated(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	snapshot := report.Outcomes()
	snapshot["a"] = flow.OutcomeFailed
	snapshot["injected"] = flow.OutcomeFailed
	if outcome, ok := report.Outcome("a"); !ok || outcome != flow.OutcomeSucceeded {
		t.Fatalf("Outcome(a) = %v, %v, want %v, true (unaffected by map mutation)", outcome, ok, flow.OutcomeSucceeded)
	}
	if _, ok := report.Outcome("injected"); ok {
		t.Fatal("Outcome(injected) resolved true, want false: report must not see the mutation")
	}
}

// TestReportChainedStepChildFailureMarksParentOnly proves a chained
// step whose child workflow fails reports the chain step's own ID
// OutcomeFailed, and the parent's outcomes map holds no entry for any
// step inside the child.
func TestReportChainedStepChildFailureMarksParentOnly(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "bad", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: child},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerGo,
			Guard: func(ctx context.Context) (bool, error) { return false, nil }},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if outcome, ok := report.Outcome("parent"); !ok || outcome != flow.OutcomeFailed {
		t.Fatalf("Outcome(parent) = %v, %v, want %v, true", outcome, ok, flow.OutcomeFailed)
	}
	if _, ok := report.Outcome("bad"); ok {
		t.Fatal("Outcome(bad) resolved true, want false: the child step must not appear in the parent's outcomes")
	}
}
