package e2e_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// namedIdentity builds a fresh identity and its agent name.
func namedIdentity(t *testing.T, name string) *identity.Identity {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New(%s): %v", name, err)
	}
	return id
}

// messagingSubRunner builds the worker that drains its inbox and
// replies into the orchestrator's mailbox.
func messagingSubRunner(t *testing.T, subID *identity.Identity, subBox, parentBox *subagent.Mailbox) (*agentrun.Runner, *agentrun.Artifacts) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "inbox", To: "received", Payload: "drain"},
		{ID: "reply", To: "replied", Needs: []string{"inbox"}, Payload: "from-sub"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New sub: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "received", Trigger: "t1"},
		machine.Transition{From: "received", To: "replied", Trigger: "t2"},
	)
	if err != nil {
		t.Fatalf("machine.New sub: %v", err)
	}
	reg := tools.New()
	addTools(t, reg,
		subagent.InboxTool("inbox", subBox),
		subagent.SendTool("reply", parentBox, subID),
	)
	artifacts := &agentrun.Artifacts{}
	a, err := agent.New(subID, discovery.Card{Name: "worker", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("agent.New sub: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: a, Machine: m, Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New sub: %v", err)
	}
	return runner, artifacts
}

// messagingOrchestrator builds the parent that greets, admits the
// subagent's signer, delegates, and collects the reply.
func messagingOrchestrator(t *testing.T, parentID *identity.Identity, subID *identity.Identity, subRunner *agentrun.Runner, subArtifacts *agentrun.Artifacts, subBox, parentBox *subagent.Mailbox, r *room.Room) (*agentrun.Runner, *agentrun.Artifacts) {
	t.Helper()
	admitCmd := subagentToolCommand(t, subagent.RoomCommand{
		Op: subagent.OpAdmit, ID: subID.Signer(),
	})
	plan, err := flow.New([]flow.Step{
		{ID: "greet", To: "greeted", Payload: "from-parent"},
		{ID: "admitsub", To: "joined", Needs: []string{"greet"}, Payload: admitCmd},
		{ID: "delegate", To: "delegated", Needs: []string{"admitsub"}, Payload: "go"},
		{ID: "collect", To: "collected", Needs: []string{"delegate"}, Payload: "drain"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New orch: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "greeted", Trigger: "t1"},
		machine.Transition{From: "greeted", To: "joined", Trigger: "t2"},
		machine.Transition{From: "joined", To: "delegated", Trigger: "t3"},
		machine.Transition{From: "delegated", To: "collected", Trigger: "t4"},
	)
	if err != nil {
		t.Fatalf("machine.New orch: %v", err)
	}
	reg := tools.New()
	addTools(t, reg,
		subagent.SendTool("greet", subBox, parentID),
		subagent.RoomTool("admitsub", r, "founder"),
		subagent.AsTool("delegate", subRunner, subagent.ToolOptions{
			Artifact: "inbox", Artifacts: subArtifacts,
		}),
		subagent.InboxTool("collect", parentBox),
	)
	artifacts := &agentrun.Artifacts{}
	a, err := agent.New(parentID, discovery.Card{Name: "orchestrator", Capabilities: []string{"c"}}, plan)
	if err != nil {
		t.Fatalf("agent.New orch: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: a, Machine: m, Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New orch: %v", err)
	}
	return runner, artifacts
}

// TestAgentsAndHumansMessageTheSubagent proves both directions of
// the message plane: the orchestrator and a human send into the
// subagent's mailbox, the subagent drains both and replies into the
// orchestrator's, and the orchestrator admits the subagent into a
// room on the way.
func TestAgentsAndHumansMessageTheSubagent(t *testing.T) {
	ctx := context.Background()
	parentID := namedIdentity(t, "orchestrator")
	subID := namedIdentity(t, "worker")
	subBox, err := subagent.NewMailbox(4)
	if err != nil {
		t.Fatalf("NewMailbox sub: %v", err)
	}
	parentBox, err := subagent.NewMailbox(4)
	if err != nil {
		t.Fatalf("NewMailbox parent: %v", err)
	}
	r, err := room.New("ops", "founder")
	if err != nil {
		t.Fatalf("room.New: %v", err)
	}
	subRunner, subArtifacts :=
		messagingSubRunner(t, subID, subBox, parentBox)
	orch, orchArtifacts := messagingOrchestrator(
		t, parentID, subID, subRunner, subArtifacts, subBox, parentBox, r)

	// A human drops a signed message into the subagent's mailbox
	// before the run: the subagent drains it alongside the parent's.
	human := namedIdentity(t, "human")
	humanMsg, err := human.Sign(e2eMessage("from-human"))
	if err != nil {
		t.Fatalf("human Sign: %v", err)
	}
	if err := subBox.Deliver(humanMsg); err != nil {
		t.Fatalf("human Deliver: %v", err)
	}

	status, _, err := orch.Run(ctx, "thread-messaging", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "collected" {
		t.Fatalf("status = %q, want collected", status)
	}
	assertMessagingResults(t, subArtifacts, orchArtifacts, r, subID)
}

// assertMessagingResults checks the drained inbox, the collected
// reply, and the room admission.
func assertMessagingResults(t *testing.T, subArtifacts, orchArtifacts *agentrun.Artifacts, r *room.Room, subID *identity.Identity) {
	t.Helper()
	inbox, ok := subArtifacts.Get("inbox")
	if !ok {
		t.Fatal("subagent recorded no inbox artifact")
	}
	if !strings.Contains(inbox, "from-human") || !strings.Contains(inbox, "from-parent") {
		t.Fatalf("subagent drained %q, want the human and parent payloads", inbox)
	}
	collect, ok := orchArtifacts.Get("collect")
	if !ok || !strings.Contains(collect, "from-sub") {
		t.Fatalf("collect artifact = %q,%v, want the subagent reply", collect, ok)
	}
	if !r.IsMember(subID.Signer()) {
		t.Fatal("subagent signer is not a room member after admission")
	}
}

// e2eMessage builds one valid message carrying payload.
func e2eMessage(payload string) envelope.Message {
	return envelope.Message{
		Version:   envelope.Version,
		ID:        "m-" + payload,
		ThreadID:  "thread-messaging",
		Intent:    envelope.IntentRequest,
		Epistemic: envelope.EpistemicAssumed,
		Payload:   payload,
	}
}
