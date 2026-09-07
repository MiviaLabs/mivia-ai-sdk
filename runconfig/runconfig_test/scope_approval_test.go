package runconfig_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/runconfig"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// writeClassTool is an internal-Kind tool publishing
// tools.ExecutionClassWrite through tools.ProfiledTool. It pins the
// approval-threshold forwarding fix: a Scope's approve callback only
// fires when RunScoped reads inner's true published class through the
// wrapper Definition.Runner puts around it, not a stripped default.
type writeClassTool struct{ name string }

// Name returns the registry name.
func (t writeClassTool) Name() string { return t.name }

// Run returns a fixed string result.
func (t writeClassTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{Value: "written"}, nil
}

// ExecutionProfile publishes ExecutionClassWrite.
func (t writeClassTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}

// TestScopeApprovalThresholdFiresForInternalKind proves a Scope
// ApprovalThreshold set on Options.Scope actually fires its approve
// callback when the run reaches a step bound to an internal Kind whose
// tool publishes ExecutionClassWrite through tools.ProfiledTool. Before
// the newStepTool forwarding fix, Definition.Runner's stepTool wrapper
// dropped tools.ProfiledTool, so RunScoped always read the zero
// ExecutionProfile and never called approve for an internal-Kind step.
func TestScopeApprovalThresholdFiresForInternalKind(t *testing.T) {
	doc := `{
		"machine": {"initial": "queued", "transitions": [
			{"from": "queued", "to": "done", "trigger": "run"}
		]},
		"plan": {"steps": [{"id": "s1", "to": "done", "payload": "go", "internal": "memory"}]},
		"tools": []
	}`
	d := loadDoc(t, doc)
	blocks := runconfig.NewBlocks()
	blocks.Set(runconfig.MemoryKind, writeClassTool{name: "s1"})
	d.Blocks = blocks
	d.Options.Agent = agentOver(t, d)

	approveCalls := 0
	d.Options.Scope = tools.NewScope(tools.ScopeOptions{
		ApprovalThreshold: tools.ExecutionClassWrite,
		Approve: func(ctx context.Context, call tools.ToolCall) (bool, error) {
			approveCalls++
			if call.Profile.Class != tools.ExecutionClassWrite {
				t.Fatalf("call.Profile.Class = %q, want write", call.Profile.Class)
			}
			return true, nil
		},
	})

	runner, err := d.Runner()
	if err != nil {
		t.Fatalf("Runner: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-scope-approval", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want done", status)
	}
	if approveCalls != 1 {
		t.Fatalf("approveCalls = %d, want 1", approveCalls)
	}
}
