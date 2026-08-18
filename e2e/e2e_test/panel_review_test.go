package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// panelTool mirrors the workflow engine's agent_panel step: one
// orchestrator step whose tool runs every panel member as a
// subagent through RunAll and joins the verdicts. A member failure
// is partial: synthesis proceeds without that member. All members
// failing fails the panel.
type panelTool struct {
	toolName string
	specs    []subagent.Spec
}

// Name returns the registry name.
func (p *panelTool) Name() string { return p.toolName }

// Run spawns every member at once and joins the verdicts.
func (p *panelTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	results := subagent.RunAll(ctx, p.specs)
	parts := make([]string, 0, len(results))
	failed := make([]string, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, r.Name)
			continue
		}
		parts = append(parts, r.Name+"="+string(r.Status))
	}
	if len(parts) == 0 {
		return tools.Out{}, fmt.Errorf("panel failed: every member failed (%s)", strings.Join(failed, ","))
	}
	return tools.Out{Value: fmt.Sprintf("panel-approved:%s,failed=%s", strings.Join(parts, ","), strings.Join(failed, ","))}, nil
}

// panelMemberRunner builds one one-step subagent runner whose member
// verdict is its final status. A failing member ends at an error
// instead.
func panelMemberRunner(t *testing.T, name string, fail bool) *agentrun.Runner {
	t.Helper()
	final := "ok-" + name
	plan, err := flow.New([]flow.Step{{ID: "work", To: final, Payload: "review"}}, nil)
	if err != nil {
		t.Fatalf("flow.New member %s: %v", name, err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: machine.Status(final), Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New member %s: %v", name, err)
	}
	reg := tools.New()
	if err := reg.Add(memberTool{name: name, fail: fail}); err != nil {
		t.Fatalf("registry.Add member %s: %v", name, err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "panel-"+name, plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New member %s: %v", name, err)
	}
	return runner
}

// memberTool is one panel member's work tool.
type memberTool struct {
	name string
	fail bool
}

// Name returns the registry name.
func (m memberTool) Name() string { return "work" }

// Run succeeds or fails this member.
func (m memberTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if m.fail {
		return tools.Out{}, fmt.Errorf("member %s failed", m.name)
	}
	return tools.Out{Value: "ok"}, nil
}

// reviewPanelPlan wires the panel step, a synthesis step chained to
// its output, a review gate routing on the panel verdict, and both
// gate branches.
func reviewPanelPlan(t *testing.T, artifacts *agentrun.Artifacts) *flow.Definition {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "review_panel", To: "paneled", Payload: "review-the-change"},
		{
			ID: "synthesis", To: "synthesized", Needs: []string{"review_panel"},
			PayloadFrom: agentrun.PayloadOf("review_panel", artifacts),
		},
		{
			ID: "review_gate", To: "gated", Needs: []string{"synthesis"}, Payload: "route-verdict",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				verdict, _ := artifacts.Get("review_panel")
				if strings.Contains(verdict, "panel-approved") {
					return []string{"merge"}, nil
				}
				return []string{"rework"}, nil
			},
		},
		{ID: "merge", To: "merged", Needs: []string{"review_gate"}, Payload: "merge-it",
			When: flow.AdmissionOnSucceeded},
		{ID: "rework", To: "reworked", Needs: []string{"review_gate"}, Payload: "rework-it",
			When: flow.AdmissionOnSucceeded},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New panel plan: %v", err)
	}
	return plan
}

// reviewPanelMachine carries the panel chain and both gate branches,
// plus the sibling-chaining row the validator's walk demands.
func reviewPanelMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "paneled", Trigger: "r01"},
		machine.Transition{From: "paneled", To: "synthesized", Trigger: "r02"},
		machine.Transition{From: "synthesized", To: "gated", Trigger: "r03"},
		machine.Transition{From: "gated", To: "merged", Trigger: "r04"},
		machine.Transition{From: "gated", To: "reworked", Trigger: "r05"},
		machine.Transition{From: "merged", To: "reworked", Trigger: "r06"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestReviewPanelPartialFailureStillApproves pins the engine's
// allow_partial policy: one of three members fails, synthesis
// proceeds over the two survivors, and the gate still approves the
// merge, naming the failed member in the artifact.
func TestReviewPanelPartialFailureStillApproves(t *testing.T) {
	artifacts := &agentrun.Artifacts{}
	plan := reviewPanelPlan(t, artifacts)
	m := reviewPanelMachine(t)
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}
	reg := tools.New()
	addTools(t, reg,
		&panelTool{toolName: "review_panel", specs: []subagent.Spec{
			{Name: "correctness", Runner: panelMemberRunner(t, "correctness", false)},
			{Name: "security", Runner: panelMemberRunner(t, "security", true)},
			{Name: "integration", Runner: panelMemberRunner(t, "integration", false)},
		}},
		e2e.PrefixTool{ToolName: "synthesis", Prefix: "synthesis:"},
		e2e.PrefixTool{ToolName: "review_gate", Prefix: "gate:"},
		e2e.PrefixTool{ToolName: "merge", Prefix: "merged:"},
		e2e.PrefixTool{ToolName: "rework", Prefix: "reworked:"},
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "panel-orchestrator", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-panel", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "merged" {
		t.Fatalf("status = %q, want %q", status, "merged")
	}
	want := "panel-approved:correctness=ok-correctness,integration=ok-integration,failed=security"
	if got, _ := artifacts.Get("review_panel"); got != want {
		t.Errorf("panel artifact = %q, want %q", got, want)
	}
	if got, _ := artifacts.Get("synthesis"); got != "synthesis:"+want {
		t.Errorf("synthesis artifact = %q, want the chained panel output", got)
	}
	if _, ok := artifacts.Get("rework"); ok {
		t.Errorf("rework ran; the route must exclude it on approval")
	}
}

// TestReviewPanelAllMembersFailFailsRun pins the other edge of the
// policy: when every member fails, the panel step itself fails and
// the run error names the panel.
func TestReviewPanelAllMembersFailFailsRun(t *testing.T) {
	artifacts := &agentrun.Artifacts{}
	plan := reviewPanelPlan(t, artifacts)
	m := reviewPanelMachine(t)
	reg := tools.New()
	addTools(t, reg,
		&panelTool{toolName: "review_panel", specs: []subagent.Spec{
			{Name: "correctness", Runner: panelMemberRunner(t, "correctness", true)},
			{Name: "security", Runner: panelMemberRunner(t, "security", true)},
		}},
		e2e.PrefixTool{ToolName: "synthesis", Prefix: "synthesis:"},
		e2e.PrefixTool{ToolName: "review_gate", Prefix: "gate:"},
		e2e.PrefixTool{ToolName: "merge", Prefix: "merged:"},
		e2e.PrefixTool{ToolName: "rework", Prefix: "reworked:"},
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "panel-orchestrator", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	_, _, err = runner.Run(context.Background(), "thread-panel-dead", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the panel failure")
	}
	if !strings.Contains(err.Error(), "panel") || !strings.Contains(err.Error(), "security") {
		t.Fatalf("Run error %q lacks the panel and a member name", err)
	}
}
