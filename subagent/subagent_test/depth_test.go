package subagent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestDepthGuardStopsSelfSpawn proves a runner whose tool is itself
// stops at the spawn bound with ErrMaxDepth, not a stack overflow.
func TestDepthGuardStopsSelfSpawn(t *testing.T) {
	ctx := context.Background()
	plan, err := flow.New([]flow.Step{{ID: "child", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	// agentrun.New validates the registry up front, so a placeholder
	// tool holds the step's name until the real, self-referencing
	// tool exists to replace it. One tracer counts the spawn spans.
	tr := trace.New()
	reg := tools.New()
	if err := reg.Add(okTool{name: "child"}); err != nil {
		t.Fatalf("Add placeholder: %v", err)
	}
	inner := runnerOver(t, plan, m, reg)
	if !reg.Remove("child") {
		t.Fatal("Remove placeholder: name not held")
	}
	if err := reg.Add(subagent.AsTool("child", inner, subagent.ToolOptions{Tracer: tr})); err != nil {
		t.Fatalf("Add tool: %v", err)
	}
	outer := runnerOver(t, plan, m, reg)

	tool := subagent.AsTool("child", outer, subagent.ToolOptions{Tracer: tr})
	_, err = tool.Run(ctx, tools.InOut{Value: "go"})
	if err == nil {
		t.Fatal("Run succeeded, want the depth bound")
	}
	// The default bound is three: three spawns pass the guard
	// before the fourth refuses.
	if spawns := len(tr.Spans()); spawns != 3 {
		t.Fatalf("spawn spans = %d, want exactly 3 before the default bound", spawns)
	}
	if !strings.Contains(err.Error(), subagent.ErrMaxDepth.Error()) {
		t.Fatalf("error %v lacks the depth sentinel", err)
	}
	if !errors.Is(err, subagent.ErrMaxDepth) {
		t.Fatalf("errors.Is(err, subagent.ErrMaxDepth) = false, want true")
	}
}

// TestDepthBoundIsConfigurable proves Depth 1 stops the very first
// nested spawn, counting the spawned executions so the boundary
// itself is pinned, not only the eventual error.
func TestDepthBoundIsConfigurable(t *testing.T) {
	ctx := context.Background()
	plan, err := flow.New([]flow.Step{{ID: "child", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	tr := trace.New()
	reg := tools.New()
	if err := reg.Add(okTool{name: "child"}); err != nil {
		t.Fatalf("Add placeholder: %v", err)
	}
	inner := runnerOver(t, plan, m, reg)
	if !reg.Remove("child") {
		t.Fatal("Remove placeholder: name not held")
	}
	if err := reg.Add(subagent.AsTool("child", inner, subagent.ToolOptions{Depth: 1, Tracer: tr})); err != nil {
		t.Fatalf("Add tool: %v", err)
	}
	outer := runnerOver(t, plan, m, reg)

	tool := subagent.AsTool("child", outer, subagent.ToolOptions{Depth: 1, Tracer: tr})
	_, err = tool.Run(ctx, tools.InOut{Value: "go"})
	if err == nil {
		t.Fatal("Run succeeded, want the depth bound at one")
	}
	if spawns := len(tr.Spans()); spawns != 1 {
		t.Fatalf("spawn spans = %d, want exactly 1 before the bound", spawns)
	}
	if !strings.Contains(err.Error(), subagent.ErrMaxDepth.Error()) {
		t.Fatalf("error %v lacks the depth sentinel", err)
	}
}

// okTool holds a step name until the real tool replaces it.
type okTool struct{ name string }

// Name returns the registry name.
func (o okTool) Name() string { return o.name }

// Run succeeds without doing anything.
func (o okTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "ok"}, nil
}
