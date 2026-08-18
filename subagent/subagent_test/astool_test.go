package subagent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestAsToolReturnsFinalStatus proves a wrapped runner reports its
// final status as the tool result when no artifact is named.
func TestAsToolReturnsFinalStatus(t *testing.T) {
	ctx := context.Background()
	tool := subagent.AsTool("sub", prefixRunner(t, "ran:", &agentrun.Artifacts{}), subagent.ToolOptions{})
	out, err := tool.Run(ctx, tools.InOut{Value: "go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "done" {
		t.Fatalf("result = %v, want the final status %q", out.Value, "done")
	}
}

// TestAsToolReturnsNamedArtifact proves the named artifact crosses
// the tool boundary when the caller shares the artifacts bag.
func TestAsToolReturnsNamedArtifact(t *testing.T) {
	ctx := context.Background()
	artifacts := &agentrun.Artifacts{}
	tool := subagent.AsTool("sub", prefixRunner(t, "ran:", artifacts),
		subagent.ToolOptions{Artifact: "work", Artifacts: artifacts})
	out, err := tool.Run(ctx, tools.InOut{Value: "go"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "ran:go" {
		t.Fatalf("result = %v, want the work artifact %q", out.Value, "ran:go")
	}
}

// TestAsToolFailurePropagates proves a failing wrapped runner fails
// the tool call with the runner's own error.
func TestAsToolFailurePropagates(t *testing.T) {
	ctx := context.Background()
	tool := subagent.AsTool("sub", failingRunner(t, "subagent broke"), subagent.ToolOptions{})
	_, err := tool.Run(ctx, tools.InOut{Value: "go"})
	if err == nil {
		t.Fatal("Run succeeded, want the runner's failure")
	}
	if want := "subagent broke"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q lacks %q", err, want)
	}
}

// TestAsToolRepeatRunsUseFreshThreads proves two invocations both
// complete: each spawn owns a fresh thread.
func TestAsToolRepeatRunsUseFreshThreads(t *testing.T) {
	ctx := context.Background()
	artifacts := &agentrun.Artifacts{}
	tool := subagent.AsTool("sub", prefixRunner(t, "ran:", artifacts), subagent.ToolOptions{})
	for i := 0; i < 2; i++ {
		out, err := tool.Run(ctx, tools.InOut{Value: "go"})
		if err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		if out.Value != "done" {
			t.Fatalf("run %d result = %v, want done", i+1, out.Value)
		}
	}
}
