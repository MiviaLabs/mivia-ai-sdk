package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestExecutionProfileIntegration registers a read-class tool and a
// write-class tool implementing ProfiledTool in one Registry. It
// builds a Scope that allowlists only the read tool and proves
// RunScoped runs the read tool and denies the write tool with
// ErrScopeDenied. It also proves Registry.Run, unscoped, still runs
// both, showing the phase 14 path is unchanged.
func TestExecutionProfileIntegration(t *testing.T) {
	readTool := &profiledTool{
		stubTool: stubTool{name: "read-file", result: "contents"},
		profile:  tools.ExecutionProfile{Class: tools.ExecutionClassRead},
	}
	writeTool := &profiledTool{
		stubTool: stubTool{name: "write-file", result: "written"},
		profile:  tools.ExecutionProfile{Class: tools.ExecutionClassWrite},
	}

	r := tools.New()
	if err := r.Add(readTool); err != nil {
		t.Fatalf("Add(read-file) error = %v, want nil", err)
	}
	if err := r.Add(writeTool); err != nil {
		t.Fatalf("Add(write-file) error = %v, want nil", err)
	}

	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"read-file"}})

	out, err := r.RunScoped(context.Background(), "read-file", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(read-file) error = %v, want nil", err)
	}
	if out.Value != "contents" {
		t.Fatalf("RunScoped(read-file).Value = %v, want contents", out.Value)
	}

	_, err = r.RunScoped(context.Background(), "write-file", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrScopeDenied) {
		t.Fatalf("RunScoped(write-file) error = %v, want ErrScopeDenied", err)
	}

	// Registry.Run, unscoped, still runs both.
	out, err = r.Run(context.Background(), "read-file", tools.InOut{})
	if err != nil {
		t.Fatalf("Run(read-file) error = %v, want nil", err)
	}
	if out.Value != "contents" {
		t.Fatalf("Run(read-file).Value = %v, want contents", out.Value)
	}
	out, err = r.Run(context.Background(), "write-file", tools.InOut{})
	if err != nil {
		t.Fatalf("Run(write-file) error = %v, want nil", err)
	}
	if out.Value != "written" {
		t.Fatalf("Run(write-file).Value = %v, want written", out.Value)
	}
}
