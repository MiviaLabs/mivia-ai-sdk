package e2e_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// wiredEdgePlan returns a one-step dispatch plan and machine the edge
// cases share.
func wiredEdgePlan(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "dispatch", To: "dispatched", Payload: "delegate"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "dispatched", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return plan, m
}

// TestWiredHookVetoFailsRun proves an orchestrator-level pre-tool
// veto fails the run before the spawn: the error names the step and
// the hook, and the subagent never runs.
func TestWiredHookVetoFailsRun(t *testing.T) {
	plan, m := wiredEdgePlan(t)
	acc := usage.New()
	primaryCalls := 0
	reg := tools.New()
	addTools(t, reg, subagent.AsTool("dispatch",
		wiredSubRunner(t, nil, acc, "session-veto", &primaryCalls),
		subagent.ToolOptions{}))
	hookReg := hooks.New()
	if err := hookReg.Add(hooks.PointPreTool, "gatekeeper", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "veto-orchestrator", plan), Machine: m,
		Tools: reg, Hooks: hookReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	_, _, err = runner.Run(context.Background(), "thread-veto", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the veto failure")
	}
	if !strings.Contains(err.Error(), "dispatch") || !strings.Contains(err.Error(), "gatekeeper") {
		t.Fatalf("Run error %q lacks the step and the hook", err)
	}
	if primaryCalls != 0 {
		t.Fatalf("primary calls = %d, want 0; the veto must precede the spawn", primaryCalls)
	}
}

// TestWiredAllProvidersFailedSurfaces proves an exhausted registry
// order fails the subagent's step, the spawn, and the orchestrator
// run, with ErrAllFailed visible at the top.
func TestWiredAllProvidersFailedSurfaces(t *testing.T) {
	plan, m := wiredEdgePlan(t)
	reg := providerregistry.New()
	for _, name := range []string{"a", "b"} {
		if err := reg.Register(name, &wiredCompleter{name: name, fail: true}); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
	}
	toolReg := tools.New()
	addTools(t, toolReg, subagent.ProviderRegistryTool("work", reg,
		[]string{"a", "b"}, func(error) bool { return true }))
	subPlan, err := flow.New([]flow.Step{{ID: "work", To: "done", Payload: "ask"}}, nil)
	if err != nil {
		t.Fatalf("flow.New sub: %v", err)
	}
	subM, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New sub: %v", err)
	}
	sub, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "dead-sub", subPlan), Machine: subM, Tools: toolReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New sub: %v", err)
	}
	top := tools.New()
	addTools(t, top, subagent.AsTool("dispatch", sub, subagent.ToolOptions{}))
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "dead-orchestrator", plan), Machine: m, Tools: top,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	_, _, err = runner.Run(context.Background(), "thread-dead", machine.InOut{})
	if !errors.Is(err, providerregistry.ErrAllFailed) {
		t.Fatalf("Run error = %v, want ErrAllFailed at the top", err)
	}
}

// TestWiredUsageIsolatedPerSession proves two spawns over one
// accumulator keep separate session totals: each thread's counts
// answer only its own turns.
func TestWiredUsageIsolatedPerSession(t *testing.T) {
	plan, m := wiredEdgePlan(t)
	acc := usage.New()
	primary := 0
	reg := tools.New()
	addTools(t, reg, subagent.AsTool("dispatch",
		wiredSubRunner(t, nil, acc, "session-a", &primary),
		subagent.ToolOptions{}))
	second := tools.New()
	addTools(t, second, subagent.AsTool("dispatch",
		wiredSubRunner(t, nil, acc, "session-b", &primary),
		subagent.ToolOptions{}))
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "usage-orchestrator", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	if _, _, err := runner.Run(context.Background(), "thread-usage-a", machine.InOut{}); err != nil {
		t.Fatalf("Run a: %v", err)
	}
	runnerB, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "usage-orchestrator-b", plan), Machine: m, Tools: second,
	})
	if err != nil {
		t.Fatalf("agentrun.New b: %v", err)
	}
	if _, _, err := runnerB.Run(context.Background(), "thread-usage-b", machine.InOut{}); err != nil {
		t.Fatalf("Run b: %v", err)
	}
	for _, session := range []string{"session-a", "session-b"} {
		total, ok := acc.Total(session)
		if !ok || total.TotalTokens != 12 {
			t.Errorf("Total(%s) = %+v,%v, want one backup turn", session, total, ok)
		}
	}
}

// TestWiredStopHookVetoFailsAfterWalk proves a PointStop veto fails
// the run after the walk completes, with the final status still
// reported alongside the error.
func TestWiredStopHookVetoFailsAfterWalk(t *testing.T) {
	plan, m := wiredEdgePlan(t)
	reg := tools.New()
	addTools(t, reg, e2e.PrefixTool{ToolName: "dispatch", Prefix: "done:"})
	hookReg := hooks.New()
	if err := hookReg.Add(hooks.PointStop, "auditor", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "stop-orchestrator", plan), Machine: m,
		Tools: reg, Hooks: hookReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-stop", machine.InOut{})
	if err == nil || !strings.Contains(err.Error(), "stop hook") {
		t.Fatalf("Run error = %v, want the stop-hook veto", err)
	}
	if status != "dispatched" {
		t.Fatalf("status = %q, want the walk's final status alongside the error", status)
	}
}
