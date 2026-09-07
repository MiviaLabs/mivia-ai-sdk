package e2e_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestOrchestratorDepthBoundStopsRecursion proves a self-spawning
// chain of subagents stops at the configured bound through a real
// orchestrator run, with the sentinel naming the failure.
func TestOrchestratorDepthBoundStopsRecursion(t *testing.T) {
	ctx := context.Background()
	plan, err := flow.New([]flow.Step{
		{ID: "child", To: "done", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	// One registry shared by every level: the innermost runner's own
	// tool is the subagent tool that spawns it again.
	reg := tools.New()
	addTools(t, reg, okTool{name: "child"})
	inner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "worker", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New inner: %v", err)
	}
	if !reg.Remove("child") {
		t.Fatal("Remove placeholder: name not held")
	}
	addTools(t, reg, subagent.AsTool("child", inner, subagent.ToolOptions{Depth: 2}))

	orchPlan, err := flow.New([]flow.Step{
		{ID: "child", To: "done", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New orch: %v", err)
	}
	orch, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "orchestrator", orchPlan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New orch: %v", err)
	}

	_, _, err = orch.Run(ctx, "thread-depth", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the depth bound")
	}
	if !strings.Contains(err.Error(), subagent.ErrMaxDepth.Error()) {
		t.Fatalf("error %v lacks the depth sentinel", err)
	}
}
