package flow_test

// These cases pin the interaction between Checkpoint's Failed field
// and a caught failure's fallback: a merge of the checkpoint/resume
// design and the fallback design, landed independently of each other,
// must agree on what a pause after a caught failure preserves.

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// checkpointFallbackFixture builds a three-step graph: risky always
// fails, fallback catches it, and join needs the fallback. It returns
// a fresh Definition and machine.Definition pair; the caller may
// build as many pairs as it needs from one call site.
func checkpointFallbackFixture(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	d, err := flow.New([]flow.Step{
		{ID: "risky", To: "r"},
		{ID: "fallback", Needs: []string{"risky"}, When: flow.AdmissionOnFailed, To: "f"},
		{ID: "join", Needs: []string{"fallback"}, To: "j"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: machine.Status("r"), Trigger: machine.Trigger("goR"),
			Guard: rejectingGuard(errors.New("risky boom"))},
		machine.Transition{From: statusStart, To: machine.Status("f"), Trigger: machine.Trigger("goF")},
		machine.Transition{From: machine.Status("f"), To: machine.Status("j"), Trigger: machine.Trigger("goJ")},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return d, m
}

// TestCheckpointFailedPreservesCaughtFailureAcrossResume proves a
// checkpoint taken after a fallback resolves still lets Resume finish
// the graph. Before Checkpoint.Failed existed, the caught failure's
// OutcomeFailed entry never survived the round trip: Resume treated
// the already-caught step as unresolved, re-selected it, and failed
// to find a transition row from the resumed status back to it, since
// cur had already moved past that point. Failed closes that gap.
func TestCheckpointFailedPreservesCaughtFailureAcrossResume(t *testing.T) {
	t.Parallel()

	dFull, mFull := checkpointFallbackFixture(t)
	want, err := flow.Run(context.Background(), dFull, mFull, machine.InOut{}, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("uninterrupted Run: %v", err)
	}

	dPause, mPause := checkpointFallbackFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var checkpoint flow.Checkpoint
	onCheckpoint := func(c flow.Checkpoint) {
		checkpoint = c
		cancel()
	}
	_, err = flow.Run(ctx, dPause, mPause, machine.InOut{}, noopConfirm, nil, onCheckpoint)
	if err == nil {
		t.Fatal("expected the pause error, got nil")
	}
	if len(checkpoint.Failed) != 1 || checkpoint.Failed[0] != "risky" {
		t.Fatalf("checkpoint.Failed = %v, want [risky]", checkpoint.Failed)
	}
	if len(checkpoint.Done) != 1 || checkpoint.Done[0] != "fallback" {
		t.Fatalf("checkpoint.Done = %v, want [fallback]", checkpoint.Done)
	}

	dResume, mResume := checkpointFallbackFixture(t)
	resumed, err := flow.Resume(context.Background(), dResume, mResume, checkpoint, noopConfirm, nil, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status() != want.Status() {
		t.Fatalf("resumed status = %q, want %q", resumed.Status(), want.Status())
	}
	mustOutcome(t, resumed, "risky", flow.OutcomeFailed)
	mustOutcome(t, resumed, "fallback", flow.OutcomeSucceeded)
	mustOutcome(t, resumed, "join", flow.OutcomeSucceeded)
}

// TestCheckpointValidateRejectsStepInFailedAndDone proves Validate
// rejects a step ID named in both Failed and Done, the same way it
// already rejects Done/Skipped and Skipped/Failed overlaps.
func TestCheckpointValidateRejectsStepInFailedAndDone(t *testing.T) {
	t.Parallel()
	c := flow.Checkpoint{
		Status: machine.Status("s"),
		Done:   []string{"x"},
		Failed: []string{"x"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected an error, got nil")
	}
}

// TestResumeRejectsUnknownFailedStep proves Resume rejects a
// checkpoint.Failed entry naming a step ID absent from d, the same
// way it already rejects an unknown Done or Skipped entry.
func TestResumeRejectsUnknownFailedStep(t *testing.T) {
	t.Parallel()
	d, m := checkpointFallbackFixture(t)
	checkpoint := flow.Checkpoint{
		Status: statusStart,
		Failed: []string{"ghost"},
	}
	_, err := flow.Resume(context.Background(), d, m, checkpoint, noopConfirm, nil, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
