package flow_test

// Regression coverage for the checkpoint/skip gap: before the fix,
// Checkpoint.Done recorded only OutcomeSucceeded steps, so a step
// excluded by Route (flow/routing.go's applyRoute) or by admission
// (flow/runner.go's nextReadyGroup) vanished from the checkpoint. A
// Resume built from that checkpoint re-evaluated the excluded step
// from scratch instead of treating the exclusion as final. These
// tests pause mid-graph right after a skip lands in the outcomes map,
// assert the fired Checkpoint's Skipped field names the excluded
// step, and prove Resume reaches the same Report an uninterrupted Run
// reaches.

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// routeLeftOnly is a Route that keeps only the "left" alternative.
func routeLeftOnly(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
	return []string{"left"}, nil
}

// routeSkipFixture builds a three-step graph: branch routes to "left"
// only, excluding "right" as a direct dependent.
func routeSkipFixture(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: routeLeftOnly},
		{ID: "left", Needs: []string{"branch"}, To: "leftDone"},
		{ID: "right", Needs: []string{"branch"}, To: "rightDone"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("b"), To: machine.Status("leftDone"), Trigger: machine.Trigger("goLeft")},
		machine.Transition{From: machine.Status("b"), To: machine.Status("rightDone"), Trigger: machine.Trigger("goRight")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// TestCheckpointSkipResumeRouteExclusionStaysExcluded reproduces the
// route-exclusion gap: pausing right after branch's checkpoint fires,
// resuming, and proving "right" stays excluded through Resume exactly
// as if the run never paused. Before the fix this either errors with
// "no transition to status" (right is re-admitted after left already
// advanced the status) or, on a looser machine, re-runs "right".
func TestCheckpointSkipResumeRouteExclusionStaysExcluded(t *testing.T) {
	t.Parallel()
	d, m := routeSkipFixture(t)
	confirmWant, _ := storingConfirm()
	want, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirmWant, nil, nil)
	if err != nil {
		t.Fatalf("uninterrupted Run: %v", err)
	}
	mustOutcome(t, want, "left", flow.OutcomeSucceeded)
	mustOutcome(t, want, "right", flow.OutcomeSkipped)

	confirm, counts := storingConfirm()
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoint flow.Checkpoint
	var checkpointCalls int
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
		if checkpointCalls == 1 {
			checkpoint = c
			cancel()
		}
	}
	_, err = flow.Run(ctx, d, m, machine.InOut{}, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if len(checkpoint.Done) != 1 || checkpoint.Done[0] != "branch" {
		t.Fatalf("checkpoint.Done = %v, want [branch]", checkpoint.Done)
	}
	if len(checkpoint.Skipped) != 1 || checkpoint.Skipped[0] != "right" {
		t.Fatalf("checkpoint.Skipped = %v, want [right]: right's route exclusion must survive the pause", checkpoint.Skipped)
	}

	resumed, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != want.Status() {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), want.Status())
	}
	mustOutcome(t, resumed, "left", flow.OutcomeSucceeded)
	mustOutcome(t, resumed, "right", flow.OutcomeSkipped)
	if counts("right") != 0 {
		t.Fatalf("right confirm count = %d, want 0: an excluded step must never run", counts("right"))
	}
	if counts("left") != 1 {
		t.Fatalf("left confirm count = %d, want 1", counts("left"))
	}
}

// routeExcludeAll is a Route that keeps no alternative, excluding
// every direct dependent of the branch step.
func routeExcludeAll(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
	return nil, nil
}

// admissionSkipFixture builds a five-step chain where "mid" is
// excluded by branch's Route, and "leaf" then skips through
// admissionVerdict (flow/runner.go), because its AdmissionOnSucceeded
// rule rejects mid's OutcomeSkipped. "mid2" and "tail" use the
// default admission rule and both run to completion after leaf skips.
func admissionSkipFixture(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "branch", To: "b", Route: routeExcludeAll},
		{ID: "mid", Needs: []string{"branch"}, To: "midDone"},
		{ID: "leaf", Needs: []string{"mid"}, When: flow.AdmissionOnSucceeded, To: "leafDone"},
		{ID: "mid2", Needs: []string{"leaf"}, To: "afterMid2"},
		{ID: "tail", Needs: []string{"mid2"}, To: "final"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("b"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("b"), To: machine.Status("afterMid2"), Trigger: triggerGo},
		machine.Transition{From: machine.Status("afterMid2"), To: machine.Status("final"), Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// TestCheckpointSkipResumeAdmissionCascadeStaysExcluded reproduces the
// admission-skip gap: "mid" excludes via Route, "leaf" then skips
// through admissionVerdict alone, with no Route of its own. Pausing
// right after mid2's checkpoint fires and resuming must reach the
// same Report an uninterrupted Run reaches, with both mid and leaf
// still Skipped.
func TestCheckpointSkipResumeAdmissionCascadeStaysExcluded(t *testing.T) {
	t.Parallel()
	d, m := admissionSkipFixture(t)
	confirmWant, _ := storingConfirm()
	want, err := flow.Run(context.Background(), d, m, machine.InOut{}, confirmWant, nil, nil)
	if err != nil {
		t.Fatalf("uninterrupted Run: %v", err)
	}
	mustOutcome(t, want, "mid", flow.OutcomeSkipped)
	mustOutcome(t, want, "leaf", flow.OutcomeSkipped)
	mustOutcome(t, want, "mid2", flow.OutcomeSucceeded)
	mustOutcome(t, want, "tail", flow.OutcomeSucceeded)

	confirm, counts := storingConfirm()
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoint flow.Checkpoint
	var checkpointCalls int
	onCheckpoint := func(c flow.Checkpoint) {
		checkpointCalls++
		if checkpointCalls == 2 {
			checkpoint = c
			cancel()
		}
	}
	_, err = flow.Run(ctx, d, m, machine.InOut{}, confirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if counts("tail") != 0 {
		t.Fatal("tail ran before Resume")
	}
	if len(checkpoint.Skipped) != 2 {
		t.Fatalf("checkpoint.Skipped = %v, want 2 entries (leaf, mid)", checkpoint.Skipped)
	}

	resumed, err := flow.Resume(context.Background(), d, m, checkpoint, confirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != want.Status() {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), want.Status())
	}
	mustOutcome(t, resumed, "mid", flow.OutcomeSkipped)
	mustOutcome(t, resumed, "leaf", flow.OutcomeSkipped)
	mustOutcome(t, resumed, "tail", flow.OutcomeSucceeded)
	if counts("mid") != 0 || counts("leaf") != 0 {
		t.Fatalf("mid confirm = %d, leaf confirm = %d, want 0, 0: an excluded step must never run", counts("mid"), counts("leaf"))
	}
	if counts("tail") != 1 {
		t.Fatalf("tail confirm count = %d, want 1", counts("tail"))
	}
}
