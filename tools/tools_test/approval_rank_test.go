package tools_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestApprovalRankBelowSkipsApprove proves a tool ranked below
// ApprovalThreshold runs with no Approve call.
func TestApprovalRankBelowSkipsApprove(t *testing.T) {
	readTool := &profiledTool{
		stubTool: stubTool{name: "read-file", result: "contents"},
		profile:  tools.ExecutionProfile{Class: tools.ExecutionClassRead},
	}
	r := tools.New()
	if err := r.Add(readTool); err != nil {
		t.Fatalf("Add(read-file) error = %v, want nil", err)
	}

	count := 0
	scope := tools.NewScope(tools.ScopeOptions{
		ApprovalThreshold: tools.ExecutionClassWrite,
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			count++
			return true, nil
		},
	})

	out, err := r.RunScoped(context.Background(), "read-file", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(read-file) error = %v, want nil", err)
	}
	if out.Value != "contents" {
		t.Fatalf("RunScoped(read-file).Value = %v, want contents", out.Value)
	}
	if count != 0 {
		t.Fatalf("Approve call count = %d, want 0", count)
	}
}

// TestApprovalRankAtOrAboveTriggersApprove proves a tool ranked
// at or above ApprovalThreshold triggers exactly one Approve call.
func TestApprovalRankAtOrAboveTriggersApprove(t *testing.T) {
	writeTool := &profiledTool{
		stubTool: stubTool{name: "write-file", result: "written"},
		profile:  tools.ExecutionProfile{Class: tools.ExecutionClassWrite},
	}
	r := tools.New()
	if err := r.Add(writeTool); err != nil {
		t.Fatalf("Add(write-file) error = %v, want nil", err)
	}

	count := 0
	scope := tools.NewScope(tools.ScopeOptions{
		ApprovalThreshold: tools.ExecutionClassWrite,
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			count++
			return true, nil
		},
	})

	out, err := r.RunScoped(context.Background(), "write-file", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(write-file) error = %v, want nil", err)
	}
	if out.Value != "written" {
		t.Fatalf("RunScoped(write-file).Value = %v, want written", out.Value)
	}
	if count != 1 {
		t.Fatalf("Approve call count = %d, want 1", count)
	}
}

// TestApprovalRankOutOfEnumClassIsCautious proves a tool with an
// out-of-enum Class triggers Approve at any threshold at or below
// ExecutionClassExternal, proving the cautious-default rank.
func TestApprovalRankOutOfEnumClassIsCautious(t *testing.T) {
	thresholds := []tools.ExecutionClass{
		tools.ExecutionClassUnclassified,
		tools.ExecutionClassRead,
		tools.ExecutionClassWrite,
		tools.ExecutionClassExternal,
	}
	for _, threshold := range thresholds {
		threshold := threshold
		t.Run(string(threshold)+"-or-empty", func(t *testing.T) {
			weird := &profiledTool{
				stubTool: stubTool{name: "weird", result: "ok"},
				profile:  tools.ExecutionProfile{Class: tools.ExecutionClass("not-a-real-class")},
			}
			r := tools.New()
			if err := r.Add(weird); err != nil {
				t.Fatalf("Add(weird) error = %v, want nil", err)
			}

			count := 0
			scope := tools.NewScope(tools.ScopeOptions{
				ApprovalThreshold: threshold,
				Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
					count++
					return true, nil
				},
			})

			_, err := r.RunScoped(context.Background(), "weird", tools.InOut{}, scope)
			if err != nil {
				t.Fatalf("RunScoped(weird) error = %v, want nil", err)
			}
			if count != 1 {
				t.Fatalf("Approve call count = %d, want 1", count)
			}
		})
	}
}

// TestApprovalRankZeroValueGatesUnclassifiedTool proves a Scope
// built with Approve set and ApprovalThreshold left unset (zero
// value) triggers Approve even for an unclassified tool.
func TestApprovalRankZeroValueGatesUnclassifiedTool(t *testing.T) {
	plain := &stubTool{name: "plain", result: "ok"}
	r := tools.New()
	if err := r.Add(plain); err != nil {
		t.Fatalf("Add(plain) error = %v, want nil", err)
	}

	count := 0
	scope := tools.NewScope(tools.ScopeOptions{
		Approve: func(_ context.Context, _ tools.ToolCall) (bool, error) {
			count++
			return true, nil
		},
	})

	_, err := r.RunScoped(context.Background(), "plain", tools.InOut{}, scope)
	if err != nil {
		t.Fatalf("RunScoped(plain) error = %v, want nil", err)
	}
	if count != 1 {
		t.Fatalf("Approve call count = %d, want 1", count)
	}
}
