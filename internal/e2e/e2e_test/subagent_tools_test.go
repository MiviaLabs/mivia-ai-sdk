package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// subagentToolCommand marshals one internal-tool command to the
// string payload a step carries.
func subagentToolCommand(t *testing.T, cmd any) string {
	t.Helper()
	b, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	return string(b)
}

// internalToolsFixture builds the blocks the subagent drives: a
// ledger, a memory store, and the sub plan and machine wiring one
// step per internal tool.
func internalToolsFixture(t *testing.T, ledgerCmd, memoryCmd string) (
	*ledger.Ledger, *memory.Store, *flow.Definition, *machine.Definition, *tools.Registry, *agentrun.Artifacts,
) {
	t.Helper()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	store, err := memory.New(512)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	childPlan, err := flow.New([]flow.Step{
		{ID: "draft", To: "drafted", Payload: "a"},
		{ID: "send", To: "sent", Needs: []string{"draft"}, Payload: "b"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New child: %v", err)
	}
	childMachine, err := machine.New("queued",
		machine.Transition{From: "queued", To: "drafted", Trigger: "t1"},
		machine.Transition{From: "drafted", To: "sent", Trigger: "t2"},
	)
	if err != nil {
		t.Fatalf("machine.New child: %v", err)
	}
	plan, err := flow.New([]flow.Step{
		{ID: "childflow", To: "flowed", Payload: "seed"},
		{ID: "ledgerstep", To: "recorded", Needs: []string{"childflow"}, Payload: ledgerCmd},
		{ID: "memstep", To: "stored", Needs: []string{"ledgerstep"}, Payload: memoryCmd},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "flowed", Trigger: "t1"},
		machine.Transition{From: "flowed", To: "recorded", Trigger: "t2"},
		machine.Transition{From: "recorded", To: "stored", Trigger: "t3"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	addTools(t, reg,
		subagent.FlowTool("childflow", childPlan, childMachine, nil),
		subagent.LedgerTool("ledgerstep", l, "sub-actor", time.Minute),
		subagent.MemoryTool("memstep", store),
	)
	return l, store, plan, m, reg, &agentrun.Artifacts{}
}

// TestSubagentDrivesInternalTools proves one spawned subagent runs a
// child flow, records ledger work, and stores a memory ref, and the
// orchestrator reads the ledger artifact across the boundary.
func TestSubagentDrivesInternalTools(t *testing.T) {
	ctx := context.Background()
	ledgerCmd := subagentToolCommand(t, subagent.LedgerCommand{
		Op: subagent.OpRun, Key: "sub-job", Seq: 1, Description: "work",
	})
	memoryCmd := subagentToolCommand(t, subagent.MemoryCommand{
		Op: subagent.OpPut, Data: "subagent-was-here",
	})
	l, store, plan, m, reg, subArtifacts :=
		internalToolsFixture(t, ledgerCmd, memoryCmd)

	subRunner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "worker", plan), Machine: m,
		Tools: reg, Artifacts: subArtifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New sub: %v", err)
	}

	orchPlan, err := flow.New([]flow.Step{
		{ID: "delegate", To: "delegated", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New orch: %v", err)
	}
	orchMachine, err := machine.New("queued",
		machine.Transition{From: "queued", To: "delegated", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New orch: %v", err)
	}
	orchArtifacts := &agentrun.Artifacts{}
	orchReg := tools.New()
	addTools(t, orchReg, subagent.AsTool("delegate", subRunner, subagent.ToolOptions{
		Artifact: "ledgerstep", Artifacts: subArtifacts,
	}))
	orch, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "orchestrator", orchPlan), Machine: orchMachine,
		Tools: orchReg, Artifacts: orchArtifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New orch: %v", err)
	}

	status, _, err := orch.Run(ctx, "thread-subtools", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "delegated" {
		t.Fatalf("status = %q, want delegated", status)
	}
	if v, ok := orchArtifacts.Get("delegate"); !ok || v != "completed" {
		t.Errorf("delegate artifact = %q,%v, want the completed ledger result", v, ok)
	}
	if v, ok := subArtifacts.Get("childflow"); !ok || v != "sent" {
		t.Errorf("childflow artifact = %q,%v, want the child flow's sent status", v, ok)
	}
	ref, ok := subArtifacts.Get("memstep")
	if !ok || ref == "" {
		t.Fatalf("memstep artifact = %q,%v, want a ref", ref, ok)
	}
	blob, err := store.Get(ref)
	if err != nil || string(blob) != "subagent-was-here" {
		t.Errorf("stored blob = %q,%v, want the subagent payload", blob, err)
	}
	st, found, err := l.State(ctx, "sub-job")
	if err != nil || !found || st.Status != ledger.StatusCompleted {
		t.Errorf("ledger sub-job = %q,%v,%v, want completed", st.Status, found, err)
	}
}
