package flow_test

// routing_integration_test.go runs an if/else graph end to end: a
// branch step, two alternatives, and a join. It exercises the full
// path New, Run, machine.Fire, and Confirm walk together, not just
// one function in isolation.

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// ifElseMachine builds the machine for the if/else integration graph:
// start -> b (the branch), b -> yes, b -> no, yes -> j, no -> j.
func ifElseMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: machine.Trigger("goB")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("yes"), Trigger: machine.Trigger("goYes")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("no"), Trigger: machine.Trigger("goNo")},
		machine.Transition{From: machine.Status("yes"), To: machine.Status("j"), Trigger: machine.Trigger("goJ")},
		machine.Transition{From: machine.Status("no"), To: machine.Status("j"), Trigger: machine.Trigger("goJ")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// pickYes is a Route that always keeps the "yes" alternative.
func pickYes(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
	return []string{"yes"}, nil
}

// confirmTracker returns a Confirm that records every step ID it
// confirms and never fails.
func confirmTracker(seen *[]string) flow.Confirm {
	return func(ctx context.Context, step flow.Step) error {
		*seen = append(*seen, step.ID)
		return nil
	}
}

// TestRoutingIntegrationDefaultJoinAdmitsChosenAlternative runs an
// if/else graph with a default-admission join. It proves the chosen
// alternative succeeds, the other alternative skips, the join still
// succeeds, Confirm never runs for the skipped alternative, and the
// final status equals the chosen branch's target status.
func TestRoutingIntegrationDefaultJoinAdmitsChosenAlternative(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: pickYes},
		{ID: "yes", Needs: []string{"branch"}, To: "yes"},
		{ID: "no", Needs: []string{"branch"}, To: "no"},
		{ID: "join", Needs: []string{"yes", "no"}, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var confirmed []string
	report, err := flow.Run(context.Background(), d, ifElseMachine(t), machine.InOut{}, confirmTracker(&confirmed), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "yes", flow.OutcomeSucceeded)
	mustOutcome(t, report, "no", flow.OutcomeSkipped)
	mustOutcome(t, report, "join", flow.OutcomeSucceeded)
	if report.Status() != machine.Status("j") {
		t.Fatalf("final status = %q, want %q (the join, reached through the chosen alternative)", report.Status(), "j")
	}
	for _, id := range confirmed {
		if id == "no" {
			t.Fatal("Confirm ran for the skipped alternative \"no\"")
		}
	}
}

// TestRoutingIntegrationStrictJoinSkipsOnUnchosenAlternative runs the
// same if/else graph, with the join needing both alternatives under
// AdmissionOnSucceeded. It proves the strict join skips, because the
// unchosen alternative never succeeds, and Confirm never runs for
// either the skipped alternative or the skipped join.
func TestRoutingIntegrationStrictJoinSkipsOnUnchosenAlternative(t *testing.T) {
	t.Parallel()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: pickYes},
		{ID: "yes", Needs: []string{"branch"}, To: "yes"},
		{ID: "no", Needs: []string{"branch"}, To: "no"},
		{ID: "join", Needs: []string{"yes", "no"}, When: flow.AdmissionOnSucceeded, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	var confirmed []string
	report, err := flow.Run(context.Background(), d, ifElseMachine(t), machine.InOut{}, confirmTracker(&confirmed), nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustOutcome(t, report, "yes", flow.OutcomeSucceeded)
	mustOutcome(t, report, "no", flow.OutcomeSkipped)
	mustOutcome(t, report, "join", flow.OutcomeSkipped)
	if report.Status() != machine.Status("yes") {
		t.Fatalf("final status = %q, want %q (the last step Run actually fired)", report.Status(), "yes")
	}
	for _, id := range confirmed {
		if id == "no" || id == "join" {
			t.Fatalf("Confirm ran for the skipped step %q", id)
		}
	}
}

// TestRoutingIntegrationConfirmRejectionBlocksRoute runs the if/else
// graph with a Confirm that rejects the branch step's own ack. It
// proves Run aborts before it calls Route: Route never runs, neither
// alternative resolves, and the branch step is marked OutcomeFailed.
func TestRoutingIntegrationConfirmRejectionBlocksRoute(t *testing.T) {
	t.Parallel()
	routeCalled := false
	route := func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
		routeCalled = true
		return []string{"yes"}, nil
	}
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: route},
		{ID: "yes", Needs: []string{"branch"}, To: "yes"},
		{ID: "no", Needs: []string{"branch"}, To: "no"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	rejectErr := errors.New("ack rejected")
	confirm := func(ctx context.Context, step flow.Step) error {
		if step.ID == "branch" {
			return rejectErr
		}
		return nil
	}
	report, err := flow.Run(context.Background(), d, ifElseMachine(t), machine.InOut{}, confirm, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, rejectErr) {
		t.Fatalf("error does not wrap the confirm rejection: %v", err)
	}
	if routeCalled {
		t.Fatal("Route ran despite the branch step's ack rejection")
	}
	mustOutcome(t, report, "branch", flow.OutcomeFailed)
	if _, ok := report.Outcome("yes"); ok {
		t.Fatal("\"yes\" resolved despite Run aborting before Route ran")
	}
	if _, ok := report.Outcome("no"); ok {
		t.Fatal("\"no\" resolved despite Run aborting before Route ran")
	}
}
