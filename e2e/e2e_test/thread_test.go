package e2e_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
)

// threadPlan builds a two-step plan whose second step needs the
// first.
func threadPlan(t *testing.T) *flow.Definition {
	t.Helper()
	p, err := flow.New([]flow.Step{
		{ID: "draft", To: "drafted", Payload: "body"},
		{ID: "send", To: "sent", Needs: []string{"draft"}, Payload: "package"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	return p
}

// threadMachine builds the two rows threadPlan fires.
func threadMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "drafted", Trigger: "run"},
		machine.Transition{From: "drafted", To: "sent", Trigger: "run"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestRunThreadVerifiesAcrossHops drives one run under a capturing
// resolver, then a second run on a fresh thread. Both message chains
// verify, the prev-hash links hold in run order, and both runs end
// on the same status.
func TestRunThreadVerifiesAcrossHops(t *testing.T) {
	ctx := context.Background()
	plan := threadPlan(t)
	m := threadMachine(t)

	run := func(thread string) (machine.Status, []envelope.Message) {
		t.Helper()
		capture := e2e.NewThreadCapture()
		runner, err := agentrun.New(agentrun.Options{
			Agent:   e2eAgent(t, "thread-agent", plan),
			Machine: m,
			Wait:    capture.Wait,
		})
		if err != nil {
			t.Fatalf("agentrun.New: %v", err)
		}
		status, _, err := runner.Run(ctx, thread, machine.InOut{})
		if err != nil {
			t.Fatalf("Run(%s): %v", thread, err)
		}
		return status, capture.Messages()
	}

	statusA, msgsA := run("thread-a")
	statusB, msgsB := run("thread-b")

	if len(msgsA) != 2 || len(msgsB) != 2 {
		t.Fatalf("captured messages = %d and %d, want 2 each", len(msgsA), len(msgsB))
	}
	if err := envelope.VerifyThread(msgsA); err != nil {
		t.Fatalf("VerifyThread(first run): %v", err)
	}
	if err := envelope.VerifyThread(msgsB); err != nil {
		t.Fatalf("VerifyThread(second run): %v", err)
	}
	if msgsA[0].ID != "draft" || msgsA[1].ID != "send" {
		t.Fatalf("message order = %s,%s, want draft then send", msgsA[0].ID, msgsA[1].ID)
	}
	if msgsA[1].PrevHash != msgsA[0].Hash() {
		t.Fatal("second message does not chain to the first message's hash")
	}
	if statusA != "sent" || statusB != "sent" {
		t.Fatalf("statuses = %q and %q, want sent on both runs", statusA, statusB)
	}

	// Metamorphic check: same inputs on a fresh thread reproduce the
	// same payload set, so the pipeline stays deterministic.
	for i := range msgsA {
		if msgsA[i].Payload != msgsB[i].Payload {
			t.Fatalf("payload %d differs across runs: %q vs %q", i, msgsA[i].Payload, msgsB[i].Payload)
		}
	}
}
