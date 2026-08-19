package agentrun_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
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
// registry, a counting tool, and an order log the tool appends to.
func hookRun(t *testing.T, reg *hooks.Registry, order *[]string) (*agentrun.Runner, *int) {
	t.Helper()
	plan, m := oneStepPlanMachine(t)
	calls := 0
	toolReg := tools.New()
	addTools(t, toolReg, markerTool{calls: &calls, order: order})
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m, Tools: toolReg, Hooks: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return runner, &calls
}

// markerTool counts its calls and appends its marker to the shared
// order log, so a test can pin the tool between the hook points.
type markerTool struct {
	calls *int
	order *[]string
}

// Name returns the registry name.
func (markerTool) Name() string { return "work" }

// Run counts the call and marks the order log.
func (m markerTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*m.calls++
	*m.order = append(*m.order, "tool")
	return tools.Out{Value: "ran"}, nil
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
	runner, calls := hookRun(t, reg, &[]string{})
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

// TestHooksFireInOrder proves the points fire around one tool call
// in order — pre-tool, tool, post-tool, stop — that the stop payload
// is the final status, and the post payload is the confirmed ack.
func TestHooksFireInOrder(t *testing.T) {
	reg := hooks.New()
	order := []string{}
	var stopPayload any
	var postPayload any
	handlers := []struct {
		point hooks.Point
		name  string
	}{
		{hooks.PointPreTool, "pre"},
		{hooks.PointPostTool, "post"},
		{hooks.PointStop, "stop"},
	}
	for _, h := range handlers {
		h := h
		if err := reg.Add(h.point, h.name, func(ctx context.Context, payload any) (bool, error) {
			order = append(order, h.name)
			switch h.point {
			case hooks.PointStop:
				stopPayload = payload
			case hooks.PointPostTool:
				postPayload = payload
			}
			return true, nil
		}); err != nil {
			t.Fatalf("hooks.Add(%s): %v", h.name, err)
		}
	}
	runner, calls := hookRun(t, reg, &order)
	status, _, err := runner.Run(context.Background(), "thread-order", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" || *calls != 1 {
		t.Fatalf("status = %q, calls = %d, want done and 1", status, *calls)
	}
	want := []string{"pre", "tool", "post", "stop"}
	if len(order) != 4 || order[0] != want[0] || order[1] != want[1] ||
		order[2] != want[2] || order[3] != want[3] {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
	if got, ok := stopPayload.(machine.Status); !ok || got != "done" {
		t.Fatalf("stop payload = %#v, want the final status done", stopPayload)
	}
	if ack, ok := postPayload.(envelope.Ack); !ok || ack.Status != envelope.AckConfirmed {
		t.Fatalf("post payload = %#v, want the confirmed ack", postPayload)
	}
}

// TestHooksPostVetoFailsAfterTool proves a PointPostTool veto fails
// the step after the tool ran, with the error naming the step and the
// hook.
func TestHooksPostVetoFailsAfterTool(t *testing.T) {
	reg := hooks.New()
	if err := reg.Add(hooks.PointPostTool, "auditor", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	order := []string{}
	runner, calls := hookRun(t, reg, &order)
	_, _, err := runner.Run(context.Background(), "thread-post-veto", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the post-tool veto failure")
	}
	if !strings.Contains(err.Error(), "work") || !strings.Contains(err.Error(), "auditor") {
		t.Fatalf("Run error %q lacks the step and the hook name", err)
	}
	if *calls != 1 {
		t.Fatalf("tool calls = %d, want 1; the veto must follow the tool", *calls)
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
