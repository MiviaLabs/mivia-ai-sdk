package flow_test

// Red step: before phase 7, Run had no chained-step path, so a step
// with Sub was treated like a normal singleton and looked for a
// transition to the empty To. Adding the child-run and
// fire-from-child helpers in flow/runner.go made the cases below pass.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// childMachine builds a machine with transitions from start to mid
// and from mid to done, both on triggerGo.
func childMachine(t *testing.T) *machine.Definition {
	t.Helper()
	const statusMid = machine.Status("mid")
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestRunChainedStepUsesChildFinalStatus proves Run uses the child
// workflow's final status as the parent step's target status.
func TestRunChainedStepUsesChildFinalStatus(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusDone)},
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
	m := singleTransitionMachine(t)
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
}

// TestRunChainedStepCallsConfirmForChildAndParent proves the same
// confirm closure runs for every child step and again for the
// chained parent step.
func TestRunChainedStepCallsConfirmForChildAndParent(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusMid)},
		{ID: "c2", Needs: []string{"c1"}, To: string(statusDone)},
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
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: machine.Trigger("shortcut")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	var order []string
	confirm := func(ctx context.Context, step flow.Step) error {
		order = append(order, step.ID)
		return nil
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"c1", "c2", "parent"}
	if len(order) != len(want) {
		t.Fatalf("confirm order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("confirm order = %v, want %v", order, want)
		}
	}
}

// TestRunChainedStepNoParentTransition proves Run fails when the
// child final status has no matching parent transition.
func TestRunChainedStepNoParentTransition(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "before", To: string(statusMid)},
		{ID: "parent", Needs: []string{"before"}, Sub: child},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: machine.Trigger("child-go")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no transition to status") {
		t.Fatalf("error %q should contain no-transition message", err.Error())
	}
	if !strings.Contains(err.Error(), `step "parent"`) {
		t.Fatalf("error %q should name the parent step", err.Error())
	}
}

// TestRunChainedStepAmbiguousParentTransition proves Run fails when
// the child final status matches more than one parent transition.
func TestRunChainedStepAmbiguousParentTransition(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "before", To: string(statusMid)},
		{ID: "parent", Needs: []string{"before"}, Sub: child},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: machine.Trigger("child-go")},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusDone, Trigger: machine.Trigger("go2")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous transition") {
		t.Fatalf("error %q should contain ambiguous-transition message", err.Error())
	}
	if !strings.Contains(err.Error(), `step "parent"`) {
		t.Fatalf("error %q should name the parent step", err.Error())
	}
}

// TestRunChainedStepReturnsChildFinalStatus proves Run returns the
// child final status as the parent status when the transition matches.
func TestRunChainedStepReturnsChildFinalStatus(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusMid)},
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
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusMid {
		t.Fatalf("status = %q, want %q", status, statusMid)
	}
}

// TestRunChainedStepReturnsChildErrorUnchanged proves a failing child
// step returns an error that names the failing child step. The child
// Run already wraps the error, so the parent returns it unchanged.
func TestRunChainedStepReturnsChildErrorUnchanged(t *testing.T) {
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
	_, err = flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `step "bad"`) {
		t.Fatalf("error %q should name the failing child step", err.Error())
	}
}

// TestRunChainedStepParentConfirmErrorWrap proves Run wraps a
// confirm error for the chained parent step and returns the status
// and record at the point of failure.
func TestRunChainedStepParentConfirmErrorWrap(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusDone)},
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
	m := singleTransitionMachine(t)
	boom := errors.New("confirm failed")
	confirm := func(ctx context.Context, step flow.Step) error {
		if step.ID == "parent" {
			return boom
		}
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := `flow: step "parent": ack not confirmed: confirm failed`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
	if outcome, ok := report.Outcome("parent"); outcome != flow.OutcomeFailed || !ok {
		t.Fatalf("Outcome(parent) = %v, %v, want %v, true", outcome, ok, flow.OutcomeFailed)
	}
}

// TestRunChainedStepInOneMemberPanelCallsConfirm proves a chained
// step that is the sole member of a one-member panel runs through
// runSingleton and calls confirm for the parent step.
func TestRunChainedStepInOneMemberPanelCallsConfirm(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	d, err := flow.New([]flow.Step{
		{ID: "parent", Sub: child},
	}, []flow.Panel{{"parent"}})
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m := singleTransitionMachine(t)
	var confirmed []string
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmed = append(confirmed, step.ID)
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
	if len(confirmed) != 2 || confirmed[0] != "c1" || confirmed[1] != "parent" {
		t.Fatalf("confirmed = %v, want [c1 parent]", confirmed)
	}
}

// TestRunNormalStepInOneMemberPanelCallsConfirm proves a normal step
// that is the sole member of a one-member panel runs through
// runSingleton and calls confirm for that step.
func TestRunNormalStepInOneMemberPanelCallsConfirm(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "solo", To: string(statusDone)},
	}, []flow.Panel{{"solo"}})
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m := singleTransitionMachine(t)
	var confirmed []string
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmed = append(confirmed, step.ID)
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
	if len(confirmed) != 1 || confirmed[0] != "solo" {
		t.Fatalf("confirmed = %v, want [solo]", confirmed)
	}
}

// TestRunChainedStepChildGetsFreshInOut proves a chained step's child
// workflow starts with a fresh machine.InOut, not the parent's record.
func TestRunChainedStepChildGetsFreshInOut(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	const statusChildDone = machine.Status("childDone")
	const triggerChild = machine.Trigger("childGo")
	const triggerParent = machine.Trigger("parentGo")

	var childInput any
	recordChildInput := func(ctx context.Context, rec *machine.InOut) error {
		childInput = rec.Input
		return nil
	}

	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusChildDone, Trigger: triggerChild, OnEntry: recordChildInput},
		machine.Transition{From: statusMid, To: statusChildDone, Trigger: triggerParent},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusChildDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}

	d, err := flow.New([]flow.Step{
		{ID: "before", To: string(statusMid)},
		{ID: "parent", Needs: []string{"before"}, Sub: child},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}

	in := machine.InOut{Input: "parent-input"}
	report, err := flow.Run(context.Background(), d, m, in, noopConfirm, nil)
	status := report.Status()
	out := report.Record()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusChildDone {
		t.Fatalf("status = %q, want %q", status, statusChildDone)
	}
	if childInput != nil {
		t.Fatalf("child saw input = %v, want nil", childInput)
	}
	if out.Input != "parent-input" {
		t.Fatalf("out.Input = %v, want %q", out.Input, "parent-input")
	}
}

// TestRunOneMemberPanelAmongOtherStepsCallsConfirm proves a one-member
// panel scheduled alongside other steps runs through the group-based
// singleton branch in Run's multi-step loop, not just the
// len(d.steps)==1 shortcut. It must still call confirm for the
// panel's sole member.
func TestRunOneMemberPanelAmongOtherStepsCallsConfirm(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	d, err := flow.New([]flow.Step{
		{ID: "a", To: string(statusMid)},
		{ID: "b", Needs: []string{"a"}, To: string(statusDone)},
	}, []flow.Panel{{"a"}})
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
	var confirmed []string
	confirm := func(ctx context.Context, step flow.Step) error {
		confirmed = append(confirmed, step.ID)
		return nil
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
	if len(confirmed) != 2 || confirmed[0] != "a" || confirmed[1] != "b" {
		t.Fatalf("confirmed = %v, want [a b]", confirmed)
	}
}

// TestRunChainedStepParentFireFromChildGuardRejects proves Run returns
// a step-scoped error when the child workflow completes successfully
// but the parent's post-child transition has a Guard that rejects.
// This exercises fireFromChild's Fire-error branch, distinct from a
// guard failure inside the child's own run: the child reaches its
// final status through an unguarded path, and only the parent-level
// row from the shared start status to that final status is guarded.
func TestRunChainedStepParentFireFromChildGuardRejects(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	const triggerParent = machine.Trigger("parentGo")
	child, err := flow.New([]flow.Step{
		{ID: "c1", To: string(statusMid)},
		{ID: "c2", Needs: []string{"c1"}, To: string(statusDone)},
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
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: triggerParent,
			Guard: func(ctx context.Context) (bool, error) { return false, nil }},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	report, err := flow.Run(context.Background(), d, m, machine.InOut{}, noopConfirm, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `step "parent"`) {
		t.Fatalf("error %q should name the parent step", err.Error())
	}
	if strings.Contains(err.Error(), `step "c1"`) || strings.Contains(err.Error(), `step "c2"`) {
		t.Fatalf("error %q should not name a child step", err.Error())
	}
	if outcome, ok := report.Outcome("parent"); outcome != flow.OutcomeFailed || !ok {
		t.Fatalf("Outcome(parent) = %v, %v, want %v, true", outcome, ok, flow.OutcomeFailed)
	}
}
