package agentrun_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// oneStepPlanMachine returns a one-step plan and machine the hook and
// tracer tests share.
func oneStepPlanMachine(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return plan, m
}

// hookRun wires one runner over the shared plan with the given hooks
// registry and a counting tool.
func hookRun(t *testing.T, reg *hooks.Registry) (*agentrun.Runner, *int) {
	t.Helper()
	plan, m := oneStepPlanMachine(t)
	calls := 0
	tools_ := tools.New()
	addTools(t, tools_, runCounterTool{calls: &calls})
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m, Tools: tools_, Hooks: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return runner, &calls
}

// TestHooksVetoFailsStep proves a PointPreTool veto fails the step
// before the tool runs, and the error names the step and the hook.
func TestHooksVetoFailsStep(t *testing.T) {
	reg := hooks.New()
	if err := reg.Add(hooks.PointPreTool, "policy", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	runner, calls := hookRun(t, reg)
	_, _, err := runner.Run(context.Background(), "thread-veto", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the veto failure")
	}
	if !strings.Contains(err.Error(), "work") || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("Run error %q lacks the step and the hook name", err)
	}
	if *calls != 0 {
		t.Fatalf("tool calls = %d, want 0 under a veto", *calls)
	}
}

// TestHooksFireInOrder proves the three points fire around one tool
// call in order: pre-tool, post-tool, then stop with the final
// status.
func TestHooksFireInOrder(t *testing.T) {
	reg := hooks.New()
	var order []string
	record := func(name string) hooks.Handler {
		return func(ctx context.Context, payload any) (bool, error) {
			order = append(order, name)
			return true, nil
		}
	}
	for point, name := range map[hooks.Point]string{
		hooks.PointPreTool: "pre", hooks.PointPostTool: "post", hooks.PointStop: "stop",
	} {
		if err := reg.Add(point, name, record(name)); err != nil {
			t.Fatalf("hooks.Add(%s): %v", name, err)
		}
	}
	runner, calls := hookRun(t, reg)
	status, _, err := runner.Run(context.Background(), "thread-order", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" || *calls != 1 {
		t.Fatalf("status = %q, calls = %d, want done and 1", status, *calls)
	}
	want := []string{"pre", "post", "stop"}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
}

// TestTracerSpansNest proves a wired tracer opens one root span for
// the run and one child span per tool call, the child's parent is the
// root, and both end.
func TestTracerSpansNest(t *testing.T) {
	plan, m := oneStepPlanMachine(t)
	calls := 0
	reg := tools.New()
	addTools(t, reg, runCounterTool{calls: &calls})
	tr := trace.New()
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m, Tools: reg, Tracer: tr,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	if _, _, err := runner.Run(context.Background(), "thread-span", machine.InOut{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spans := tr.Spans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2 (root and tool)", len(spans))
	}
	var root, child *trace.Span
	for _, s := range spans {
		switch s.Name {
		case "agentrun.run":
			root = s
		case "agentrun.tool":
			child = s
		}
	}
	if root == nil || child == nil {
		t.Fatalf("span names missing: %+v", spans)
	}
	if child.ParentID != root.ID {
		t.Fatalf("tool span parent = %v, want the root %v", child.ParentID, root.ID)
	}
	if root.EndTime().IsZero() || child.EndTime().IsZero() {
		t.Fatal("both spans must end")
	}
	if attr, ok := root.Attributes()["thread"]; !ok || attr != "thread-span" {
		t.Fatalf("root thread attribute = %q,%v", attr, ok)
	}
	if attr, ok := child.Attributes()["step"]; !ok || attr != "work" {
		t.Fatalf("tool step attribute = %q,%v", attr, ok)
	}
}
