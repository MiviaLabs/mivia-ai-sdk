package flow_test

// Red step: before Run existed, this file did not compile because
// flow.Run and flow.Confirm were undefined symbols. `go test ./flow/...`
// failed with "undefined: flow.Run" and "undefined: flow.Confirm".
// Run and Confirm landed in flow/runner.go; the cases below then passed.

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

const (
	statusStart = machine.Status("start")
	statusDone  = machine.Status("done")
	triggerGo   = machine.Trigger("go")
)

// noopConfirm always confirms the ack.
func noopConfirm(ctx context.Context, step flow.Step) error { return nil }

// singleStepGraph builds a one-step Definition targeting statusDone.
func singleStepGraph(t *testing.T) *flow.Definition {
	t.Helper()
	d, err := flow.New([]flow.Step{{ID: "a", To: string(statusDone)}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return d
}

// singleTransitionMachine builds a one-transition Definition.
func singleTransitionMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New(statusStart, machine.Transition{
		From: statusStart, To: statusDone, Trigger: triggerGo,
	})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestRunOrderRule proves Run walks a two-step chain in order and
// reaches the final status.
func TestRunOrderRule(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusMid)},
		{ID: "b", Needs: []string{"a"}, To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var order []string
	confirm := func(ctx context.Context, step flow.Step) error {
		order = append(order, step.ID)
		return nil
	}
	status, _, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
	want := []string{"a", "b"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// TestRunTransitionPick proves Run picks the transition row whose To
// matches the step's target status.
func TestRunTransitionPick(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	status, _, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
}

// TestRunNilConfirmRejected proves Run rejects a nil confirm before
// it touches the graph.
func TestRunNilConfirmRejected(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m := singleTransitionMachine(t)
	status, _, err := flow.Run(context.Background(), d, m, machine.InOut{}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: confirm must not be nil") {
		t.Fatalf("error %q should contain the confirm-nil message", err.Error())
	}
	if status != statusStart {
		t.Fatalf("status = %q, want the initial status %q", status, statusStart)
	}
}

// TestRunNilDRejected proves Run rejects a nil Definition before it
// touches m, and never panics.
func TestRunNilDRejected(t *testing.T) {
	t.Parallel()
	m := singleTransitionMachine(t)
	status, _, err := flow.Run(context.Background(), nil, m, machine.InOut{}, noopConfirm)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: d must not be nil") {
		t.Fatalf("error %q should contain the d-nil message", err.Error())
	}
	if status != machine.Status("") {
		t.Fatalf("status = %q, want the zero Status", status)
	}
}

// TestRunNilMRejected proves Run rejects a nil machine Definition
// before it touches the graph.
func TestRunNilMRejected(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	status, _, err := flow.Run(context.Background(), d, nil, machine.InOut{}, noopConfirm)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: m must not be nil") {
		t.Fatalf("error %q should contain the m-nil message", err.Error())
	}
	if status != machine.Status("") {
		t.Fatalf("status = %q, want the zero Status", status)
	}
}

// TestRunNilDAndMTogether proves the d-nil error wins when both d and
// m are nil, and Run never panics.
func TestRunNilDAndMTogether(t *testing.T) {
	t.Parallel()
	status, _, err := flow.Run(context.Background(), nil, nil, machine.InOut{}, noopConfirm)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "flow: d must not be nil") {
		t.Fatalf("error %q should contain the d-nil message", err.Error())
	}
	if status != machine.Status("") {
		t.Fatalf("status = %q, want the zero Status", status)
	}
}

// TestRunAmbiguityZeroMatches proves Run fails when no transition
// row targets the step's status.
func TestRunAmbiguityZeroMatches(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{{ID: "a", To: "nowhere"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	_, _, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `no transition to status`) {
		t.Fatalf("error %q should contain the no-transition message", err.Error())
	}
	if !strings.Contains(err.Error(), `step "a"`) {
		t.Fatalf("error %q should name the failing step", err.Error())
	}
}

// TestRunAmbiguityZeroMatchesTerminalState proves Run reports a
// zero-match error when the current status has no outgoing
// transitions at all, not only when a row targets the wrong status.
func TestRunAmbiguityZeroMatchesTerminalState(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusDone)},
		{ID: "b", Needs: []string{"a"}, To: "unreachable"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	_, _, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `no transition to status "unreachable" from ""`) {
		t.Fatalf("error %q should name the empty source status", err.Error())
	}
	if !strings.Contains(err.Error(), `step "b"`) {
		t.Fatalf("error %q should name the failing step", err.Error())
	}
}

// TestRunEmptyGraph proves Run on a zero-step graph returns the
// initial status with no error, and never calls confirm.
func TestRunEmptyGraph(t *testing.T) {
	t.Parallel()
	d, err := flow.New(nil, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	confirm := func(ctx context.Context, step flow.Step) error {
		t.Fatal("confirm ran on an empty step graph")
		return nil
	}
	status, out, err := flow.Run(context.Background(), d, m, machine.InOut{Input: "x"}, confirm)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusStart {
		t.Fatalf("status = %q, want the initial status %q", status, statusStart)
	}
	if out.Input != "x" {
		t.Fatalf("out.Input = %v, want the untouched input record", out.Input)
	}
}

// TestRunAmbiguityManyMatches proves Run fails when more than one
// transition row targets the step's status.
func TestRunAmbiguityManyMatches(t *testing.T) {
	t.Parallel()
	d := singleStepGraph(t)
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: machine.Trigger("go2")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, _, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous transition") {
		t.Fatalf("error %q should contain the ambiguous-transition message", err.Error())
	}
	if !strings.Contains(err.Error(), `step "a"`) {
		t.Fatalf("error %q should name the failing step", err.Error())
	}
}
