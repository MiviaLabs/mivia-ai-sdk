package e2e_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/spool"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// largeOutputTool always returns a large string result, standing in
// for a log tail or a file read a caller does not want in full.
type largeOutputTool struct {
	toolName string
	result   string
}

// Name returns the registry name.
func (l largeOutputTool) Name() string { return l.toolName }

// Run returns l's fixed, oversized string result.
func (l largeOutputTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: l.result}, nil
}

// TestSpoolToolTruncatesLargeStepResult wires SpoolTool around a
// large-output tool inside an agentrun step, then confirms the
// spooled view names a ref that a follow-up Spool.Load call resolves
// to the tool's full result.
func TestSpoolToolTruncatesLargeStepResult(t *testing.T) {
	ctx := context.Background()
	store, err := memory.New(1 << 20)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}

	full := strings.Repeat("log-line\n", 500)
	inner := largeOutputTool{toolName: "tail", result: full}
	wrapped := spool.SpoolTool("tail", 64, store, inner)

	plan, err := flow.New([]flow.Step{
		{ID: "tail", To: "tailed", Payload: "go"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "tailed", Trigger: "t1"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}

	reg := tools.New()
	addTools(t, reg, wrapped)
	artifacts := &agentrun.Artifacts{}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "tailer", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	runCtx := spool.WithPrincipal(ctx, "tailer")
	status, _, err := runner.Run(runCtx, "thread-spool", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "tailed" {
		t.Fatalf("status = %q, want tailed", status)
	}

	view, ok := artifacts.Get("tail")
	if !ok {
		t.Fatalf("artifacts.Get(tail) = %q,%v, want a recorded view", view, ok)
	}
	if len(view) >= len(full) {
		t.Fatalf("view len = %d, want shorter than the full result len %d", len(view), len(full))
	}

	sp, err := spool.NewSpool(store, 1<<20)
	if err != nil {
		t.Fatalf("spool.NewSpool: %v", err)
	}
	_, ref, err := sp.Spool(runCtx, "tailer", []byte(full))
	if err != nil {
		t.Fatalf("sp.Spool: %v", err)
	}
	if !strings.Contains(view, ref) {
		t.Fatalf("view %q does not name ref %q", view, ref)
	}
	got, err := sp.Load(runCtx, "tailer", ref)
	if err != nil || string(got) != full {
		t.Fatalf("sp.Load = %q,%v, want the full tool result", got, err)
	}
}
