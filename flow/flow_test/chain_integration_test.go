package flow_test

// Integration test: a parent workflow nests a child workflow. The
// child final status returns to the parent. The Confirm closure records
// an audit thread. envelope.VerifyThread checks the thread. A tampered
// message fails verification.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// chainWorkflows builds the child and parent workflows for the main
// integration test. The child has two steps; the parent has a chained
// step and a dependent normal step.
func chainWorkflows(t *testing.T) (*flow.Definition, *flow.Definition) {
	t.Helper()
	const statusMid = machine.Status("mid")
	child, err := flow.New([]flow.Step{
		{ID: "child-a", To: string(statusMid)},
		{ID: "child-b", Needs: []string{"child-a"}, To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	const statusFinal = machine.Status("final")
	parent, err := flow.New([]flow.Step{
		{ID: "parent-chain", Sub: child},
		{ID: "parent-next", Needs: []string{"parent-chain"}, To: string(statusFinal)},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	return child, parent
}

// chainMachine builds the machine for the main integration test.
// It supports the child path start->mid->done, the chained-parent
// shortcut start->done, and the dependent step done->final.
func chainMachine(t *testing.T) *machine.Definition {
	t.Helper()
	const (
		statusMid   = machine.Status("mid")
		statusFinal = machine.Status("final")
	)
	m, err := machine.New(statusStart,
		machine.Transition{From: statusStart, To: statusMid, Trigger: triggerGo},
		machine.Transition{From: statusMid, To: statusDone, Trigger: triggerGo},
		machine.Transition{From: statusStart, To: statusDone, Trigger: machine.Trigger("shortcut")},
		machine.Transition{From: statusDone, To: statusFinal, Trigger: triggerGo},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// chainConfirm returns a Confirm closure that signs and records each
// step as an envelope message. It also returns the thread slice.
func chainConfirm(t *testing.T, priv ed25519.PrivateKey, room, threadID string) (flow.Confirm, *[]envelope.Message) {
	t.Helper()
	var thread []envelope.Message
	var prev string
	confirm := func(ctx context.Context, step flow.Step) error {
		msg := envelope.Message{
			Version:    envelope.Version,
			ID:         step.ID,
			Room:       room,
			ThreadID:   threadID,
			Intent:     envelope.IntentAssert,
			Epistemic:  envelope.EpistemicAssumed,
			Confidence: 1,
			Payload:    "confirmed " + step.ID,
			PrevHash:   prev,
		}
		msg, err := envelope.Sign(priv, msg)
		if err != nil {
			return err
		}
		thread = append(thread, msg)
		prev = msg.Hash()
		return nil
	}
	return confirm, &thread
}

// assertThreadOrder checks the recorded thread contains the expected
// step IDs in order.
func assertThreadOrder(t *testing.T, thread []envelope.Message, want []string) {
	t.Helper()
	if len(thread) != len(want) {
		t.Fatalf("thread length = %d, want %d", len(thread), len(want))
	}
	for i := range want {
		if thread[i].ID != want[i] {
			t.Fatalf("thread[%d].ID = %q, want %q", i, thread[i].ID, want[i])
		}
	}
}

// TestChainedWorkflowStatusAndAuditThread runs a parent workflow with
// one chained step and one dependent normal step. It records envelope
// messages in Confirm, verifies the thread, then proves tampering
// breaks verification.
func TestChainedWorkflowStatusAndAuditThread(t *testing.T) {
	t.Parallel()
	const (
		auditRoom   = "room://phase07"
		auditThread = "thread://phase07"
	)
	_, parent := chainWorkflows(t)
	m := chainMachine(t)
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	confirm, threadPtr := chainConfirm(t, priv, auditRoom, auditThread)

	report, err := flow.Run(context.Background(), parent, m, machine.InOut{}, confirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	const statusFinal = machine.Status("final")
	if status != statusFinal {
		t.Fatalf("status = %q, want %q", status, statusFinal)
	}

	want := []string{"child-a", "child-b", "parent-chain", "parent-next"}
	assertThreadOrder(t, *threadPtr, want)
	if err := envelope.VerifyThread(*threadPtr); err != nil {
		t.Fatalf("VerifyThread: %v", err)
	}

	// Tamper with one message's payload and prove verification fails.
	tampered := make([]envelope.Message, len(*threadPtr))
	copy(tampered, *threadPtr)
	tampered[1].Payload = strings.Replace(tampered[1].Payload, "child-b", "evil", 1)
	if err := envelope.VerifyThread(tampered); err == nil {
		t.Fatal("VerifyThread should fail on a tampered message")
	}
}

// TestChainedStepStatusReturnedAsParentStatus proves the parent status
// equals the child final status when no normal step follows the chain.
func TestChainedStepStatusReturnedAsParentStatus(t *testing.T) {
	t.Parallel()
	const statusMid = machine.Status("mid")
	child, err := flow.New([]flow.Step{
		{ID: "child-a", To: string(statusMid)},
		{ID: "child-b", Needs: []string{"child-a"}, To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	parent, err := flow.New([]flow.Step{
		{ID: "parent-chain", Sub: child},
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
	report, err := flow.Run(context.Background(), parent, m, machine.InOut{}, noopConfirm, nil)
	status := report.Status()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != statusDone {
		t.Fatalf("status = %q, want %q", status, statusDone)
	}
}

// TestAuditThreadJSONRoundTrip proves the recorded thread survives a
// JSON round trip and still verifies.
func TestAuditThreadJSONRoundTrip(t *testing.T) {
	t.Parallel()
	child, err := flow.New([]flow.Step{
		{ID: "child-a", To: string(statusDone)},
	}, nil)
	if err != nil {
		t.Fatalf("child New: %v", err)
	}
	parent, err := flow.New([]flow.Step{
		{ID: "parent-chain", Sub: child},
	}, nil)
	if err != nil {
		t.Fatalf("parent New: %v", err)
	}
	m := singleTransitionMachine(t)

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	var thread []envelope.Message
	var prev string
	confirm := func(ctx context.Context, step flow.Step) error {
		msg := envelope.Message{
			Version:    envelope.Version,
			ID:         step.ID,
			ThreadID:   "thread://json",
			Intent:     envelope.IntentAssert,
			Epistemic:  envelope.EpistemicAssumed,
			Confidence: 1,
			Payload:    "confirmed " + step.ID,
			PrevHash:   prev,
		}
		msg, err := envelope.Sign(priv, msg)
		if err != nil {
			return err
		}
		thread = append(thread, msg)
		prev = msg.Hash()
		return nil
	}

	_, err = flow.Run(context.Background(), parent, m, machine.InOut{}, confirm, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := json.Marshal(thread)
	if err != nil {
		t.Fatalf("marshal thread: %v", err)
	}
	var back []envelope.Message
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal thread: %v", err)
	}
	if err := envelope.VerifyThread(back); err != nil {
		t.Fatalf("VerifyThread after round trip: %v", err)
	}
}
