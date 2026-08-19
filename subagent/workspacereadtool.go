// WorkspaceReadTool reads one file from a caller-bound Workspace.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// WorkspaceReadArgs is the decoded argument struct for
// WorkspaceReadTool. Path is relative to the bound Workspace's root.
type WorkspaceReadArgs struct {
	Path string `json:"path"`
}

// WorkspaceReadTool returns a tool that reads one file inside ft's
// bound workspace, relative to its root. maxResultBytes, when
// positive, publishes tools.ResultBudgetTool so agentloop's render
// truncates an oversized read instead of flooding the model's
// context; a non-positive value publishes no budget. Not privileged.
func WorkspaceReadTool(name string, ft *FileTools, maxResultBytes int) tools.Tool {
	base := &workspaceReadTool{name: name, ft: ft}
	if maxResultBytes > 0 {
		return &workspaceReadToolBudgeted{workspaceReadTool: base, maxResultBytes: maxResultBytes}
	}
	return base
}

// workspaceReadTool adapts one workspace read to tools.Tool,
// tools.SchemaTool, and tools.ProfiledTool.
type workspaceReadTool struct {
	name string
	ft   *FileTools
}

// Name returns the registry name.
func (t *workspaceReadTool) Name() string { return t.name }

// ParameterSchema returns the flat, string-only argument schema.
func (t *workspaceReadTool) ParameterSchema() []byte {
	return flatStringSchema("path")
}

// DecodeArguments parses raw into WorkspaceReadArgs.
func (t *workspaceReadTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	args, err := decodeArgs[WorkspaceReadArgs](t.name, raw)
	if err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args}, nil
}

// ExecutionProfile publishes ExecutionClassRead.
func (t *workspaceReadTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassRead}
}

// Run reads the file at args.Path, relative to t.ft's bound root.
func (t *workspaceReadTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(WorkspaceReadArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	data, err := t.ft.ws.ReadFile(args.Path)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: string(data)}, nil
}

// workspaceReadToolBudgeted adds tools.ResultBudgetTool to
// workspaceReadTool. A separate concrete type, not a runtime branch
// on one type, keeps tools.ResultBudgetOf reporting false when
// maxResultBytes is non-positive; see spool/tool.go's
// conditional-capability-composition pattern.
type workspaceReadToolBudgeted struct {
	*workspaceReadTool
	maxResultBytes int
}

// MaxResultBytes returns the configured cap.
func (t *workspaceReadToolBudgeted) MaxResultBytes() int { return t.maxResultBytes }
