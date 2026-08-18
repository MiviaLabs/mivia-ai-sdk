package tools_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunScopedApprovalIntegration registers a read-class tool and a
// write-class tool implementing ProfiledTool in one Registry. It
// builds a Scope with ApprovalThreshold: ExecutionClassWrite and an
// Approve function that denies every call. It proves RunScoped runs
// the read tool with no Approve call and denies the write tool with
// ErrToolDeclined. It also proves Registry.Run, unscoped, still runs
// both tools with no approval check, showing the phase 14 and phase
// 31 paths stay unchanged.
func TestRunScopedApprovalIntegration(t *testing.T) {
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

	approveCalls := 0
	scope := tools.NewScope(tools.ScopeOptions{
		ApprovalThreshold: tools.ExecutionClassWrite,
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			approveCalls++
			return false, nil
		},
	})

	out, err := r.RunScoped(context.Background(), "read-file", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(read-file) error = %v, want nil", err)
	}
	if out.Value != "contents" {
		t.Fatalf("RunScoped(read-file).Value = %v, want contents", out.Value)
	}
	if approveCalls != 0 {
		t.Fatalf("Approve call count after read-file = %d, want 0", approveCalls)
	}

	_, err = r.RunScoped(context.Background(), "write-file", tools.InOut{}, scope)
	if !errors.Is(err, tools.ErrToolDeclined) {
		t.Fatalf("RunScoped(write-file) error = %v, want ErrToolDeclined", err)
	}
	if approveCalls != 1 {
		t.Fatalf("Approve call count after write-file = %d, want 1", approveCalls)
	}

	// Registry.Run, unscoped, still runs both tools with no approval
	// check.
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
	if approveCalls != 1 {
		t.Fatalf("Approve call count after unscoped Run = %d, want 1", approveCalls)
	}
}
