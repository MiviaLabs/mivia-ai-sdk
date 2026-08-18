// Package agent_test also holds the checkpoint pause and resume
// integration test. agent.Run passes a nil onCheckpoint to flow.Run,
// so no checkpoint fires through it; this file drives flow.Run and
// flow.Resume directly, with the same signed-and-chained Confirm shape
// agent.confirmStep builds. See
// docs/plans/agents/phase46_system_integration_suite.md.
package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// checkpointPlan builds a graph whose outcomes cover all three
// Checkpoint lists at once. The root's Route keeps only the failing
// branch, so the other dependent resolves OutcomeSkipped. The failing
// step's transition always rejects, and the fallback step catches it
// through AdmissionOnFailed.
func checkpointPlan(t testing.TB) *flow.Definition {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{
			ID: "open", To: "opened", Payload: "open the review",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				return []string{"failing"}, nil
			},
		},
		{ID: "skipped-branch", Needs: []string{"open"}, To: "sidelined", Payload: "the route drops this branch"},
		{ID: "failing", Needs: []string{"open"}, To: "published", Payload: "publish the review"},
		{
			ID: "fallback", Needs: []string{"failing"}, To: "recovered",
			When: flow.AdmissionOnFailed, Payload: "fall back after the failed publish",
		},
		{ID: "final", Needs: []string{"fallback"}, To: "closed", Payload: "close the review"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}
	return plan
}

// checkpointMachine builds the status model checkpointPlan targets.
// The publish transition always rejects, so the fallback has a real
// failure to catch.
func checkpointMachine(t testing.TB) *machine.Definition {
	t.Helper()
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "opened", Trigger: "go-open"},
		machine.Transition{From: "opened", To: "sidelined", Trigger: "go-side"},
		machine.Transition{
			From: "opened", To: "published", Trigger: "go-publish",
			Guard: func(ctx context.Context) (bool, error) {
				return false, errors.New("publish: downstream rejected the artifact")
			},
		},
		machine.Transition{From: "opened", To: "recovered", Trigger: "go-recover"},
		machine.Transition{From: "recovered", To: "closed", Trigger: "go-close"},
	)
	if err != nil {
		t.Fatalf("machine.New() unexpected error: %v", err)
	}
	return m
}

// signingConfirm builds the flow.Confirm closure this file uses. It
// mirrors agent.confirmStep: build an envelope, chain it to the
// previous message with PrevHash, sign it with a real identity, and
// verify the signature. built accumulates every signed message so the
// caller can verify the thread.
func signingConfirm(t testing.TB, id *identity.Identity, threadID string, built *[]envelope.Message) flow.Confirm {
	t.Helper()
	return func(ctx context.Context, step flow.Step) error {
		msg := envelope.Message{
			Version:   envelope.Version,
			ID:        step.ID,
			ThreadID:  threadID,
			Intent:    envelope.IntentRequest,
			Epistemic: envelope.EpistemicAssumed,
			Payload:   step.Payload,
		}
		if n := len(*built); n > 0 {
			msg.PrevHash = (*built)[n-1].Hash()
		}
		signed, err := id.Sign(msg)
		if err != nil {
			return err
		}
		if err := signed.VerifySignature(); err != nil {
			return err
		}
		*built = append(*built, signed)
		return nil
	}
}

// checkpointBus builds a bus carrying the one name flow.Run emits.
func checkpointBus(t testing.TB) *events.Bus {
	t.Helper()
	bus := events.New()
	noop := func(ctx context.Context, e events.Event) error { return nil }
	if err := bus.Subscribe(flow.StepCompletedEvent, noop); err != nil {
		t.Fatalf("Subscribe(%q) unexpected error: %v", flow.StepCompletedEvent, err)
	}
	return bus
}

// TestSystemCheckpointCarriesAllThreeOutcomeLists proves a real run
// produces a Checkpoint whose Done, Skipped, and Failed are all
// non-empty at once, and that Validate's one-list-per-step-ID rule
// holds on it. flow's own unit tests build such a Checkpoint by hand;
// this test earns one from a real graph.
func TestSystemCheckpointCarriesAllThreeOutcomeLists(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	var built []envelope.Message
	var seen []flow.Checkpoint
	report, err := flow.Run(context.Background(), checkpointPlan(t), checkpointMachine(t),
		machine.InOut{}, signingConfirm(t, id, "checkpoint-thread", &built), checkpointBus(t),
		func(c flow.Checkpoint) { seen = append(seen, c) })
	if err != nil {
		t.Fatalf("flow.Run() unexpected error: %v", err)
	}
	if report.Status() != machine.Status("closed") {
		t.Fatalf("Run() status = %q, want %q", report.Status(), "closed")
	}
	if err := envelope.VerifyThread(built); err != nil {
		t.Fatalf("envelope.VerifyThread() unexpected error: %v", err)
	}

	full := findFullCheckpoint(t, seen)
	if err := full.Validate(); err != nil {
		t.Fatalf("Checkpoint.Validate() unexpected error: %v", err)
	}
	if !contains(full.Skipped, "skipped-branch") {
		t.Fatalf("Checkpoint.Skipped = %v, want it to name skipped-branch", full.Skipped)
	}
	if !contains(full.Failed, "failing") {
		t.Fatalf("Checkpoint.Failed = %v, want it to name failing", full.Failed)
	}

	encoded, err := full.Encode()
	if err != nil {
		t.Fatalf("Checkpoint.Encode() unexpected error: %v", err)
	}
	decoded, err := flow.Decode(encoded)
	if err != nil {
		t.Fatalf("flow.Decode() unexpected error: %v", err)
	}
	if !sameCheckpoint(full, decoded) {
		t.Fatalf("decoded checkpoint = %+v, want %+v", decoded, full)
	}
}

// TestSystemCheckpointResumeMatchesUninterruptedRun proves Resume from
// a captured mid-run Checkpoint reaches the same outcomes an
// uninterrupted run reaches, and re-runs no step already done.
func TestSystemCheckpointResumeMatchesUninterruptedRun(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	var baselineBuilt []envelope.Message
	baseline, err := flow.Run(context.Background(), checkpointPlan(t), checkpointMachine(t),
		machine.InOut{}, signingConfirm(t, id, "baseline-thread", &baselineBuilt), checkpointBus(t), nil)
	if err != nil {
		t.Fatalf("baseline flow.Run() unexpected error: %v", err)
	}

	var pausedBuilt []envelope.Message
	var seen []flow.Checkpoint
	_, err = flow.Run(context.Background(), checkpointPlan(t), checkpointMachine(t),
		machine.InOut{}, signingConfirm(t, id, "paused-thread", &pausedBuilt), checkpointBus(t),
		func(c flow.Checkpoint) { seen = append(seen, c) })
	if err != nil {
		t.Fatalf("paused flow.Run() unexpected error: %v", err)
	}
	full := findFullCheckpoint(t, seen)

	var resumedBuilt []envelope.Message
	resumed, err := flow.Resume(context.Background(), checkpointPlan(t), checkpointMachine(t),
		full, signingConfirm(t, id, "resumed-thread", &resumedBuilt), checkpointBus(t), nil)
	if err != nil {
		t.Fatalf("flow.Resume() unexpected error: %v", err)
	}

	if resumed.Status() != baseline.Status() {
		t.Fatalf("resumed status = %q, want the baseline status %q", resumed.Status(), baseline.Status())
	}
	for id, want := range baseline.Outcomes() {
		got, ok := resumed.Outcome(id)
		if !ok {
			t.Fatalf("resumed report has no outcome for step %q", id)
		}
		if got != want {
			t.Fatalf("resumed outcome for %q = %v, want %v", id, got, want)
		}
	}
	for _, msg := range resumedBuilt {
		if contains(full.Done, msg.ID) {
			t.Fatalf("Resume re-ran step %q, which the checkpoint already lists in Done", msg.ID)
		}
	}
}

// findFullCheckpoint returns the first checkpoint whose Done, Skipped,
// and Failed are all non-empty. It fails the test when no captured
// checkpoint carries all three.
func findFullCheckpoint(t *testing.T, seen []flow.Checkpoint) flow.Checkpoint {
	t.Helper()
	for _, c := range seen {
		if len(c.Done) > 0 && len(c.Skipped) > 0 && len(c.Failed) > 0 {
			return c
		}
	}
	t.Fatalf("no captured checkpoint carried all three outcome lists; captured %d: %+v", len(seen), seen)
	return flow.Checkpoint{}
}

// contains reports whether ids holds want.
func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// sameCheckpoint compares two checkpoints by status and by their three
// sorted step-ID lists.
func sameCheckpoint(a, b flow.Checkpoint) bool {
	if a.Status != b.Status {
		return false
	}
	return equalIDs(a.Done, b.Done) && equalIDs(a.Skipped, b.Skipped) && equalIDs(a.Failed, b.Failed)
}

// equalIDs reports whether two step-ID lists match element for
// element. Checkpoint sorts each list, so order is stable.
func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
