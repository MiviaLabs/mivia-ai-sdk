// WorkspaceWriteTool writes one file into a caller-bound Workspace.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// WorkspaceWriteArgs is the decoded argument struct for
// WorkspaceWriteTool. Path is relative to the bound Workspace's root;
// Content replaces the file's whole content.
type WorkspaceWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WorkspaceWriteTool returns a tool that writes one file inside ft's
// bound workspace, relative to its root, with the fixed mode
// workspace.WriteFile applies. No os.FileMode argument reaches the
// model. Implements tools.PrivilegedTool: tools.Scope.Allowed denies
// it unless a caller's ScopeOptions.Allowlist names it explicitly.
func WorkspaceWriteTool(name string, ft *FileTools) tools.Tool {
	return &workspaceWriteTool{name: name, ft: ft}
}

// workspaceWriteTool adapts one workspace write to tools.Tool,
// tools.SchemaTool, tools.ProfiledTool, and tools.PrivilegedTool.
type workspaceWriteTool struct {
	name string
	ft   *FileTools
}

// Name returns the registry name.
func (t *workspaceWriteTool) Name() string { return t.name }

// ParameterSchema returns the flat, string-only argument schema.
func (t *workspaceWriteTool) ParameterSchema() []byte {
	return flatStringSchema("path", "content")
}

// DecodeArguments parses raw into WorkspaceWriteArgs.
func (t *workspaceWriteTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	args, err := decodeArgs[WorkspaceWriteArgs](t.name, raw)
	if err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args}, nil
}

// ExecutionProfile publishes ExecutionClassWrite.
func (t *workspaceWriteTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassWrite}
}

// Privileged reports true: WorkspaceWriteTool mutates the filesystem
// inside ws's root, so tools.Scope.Allowed denies it by default.
func (t *workspaceWriteTool) Privileged() bool { return true }

// Run writes args.Content to args.Path, relative to t.ft's bound
// root. workspace.WriteFile owns the create mode.
func (t *workspaceWriteTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(WorkspaceWriteArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	if err := t.ft.ws.WriteFile(args.Path, []byte(args.Content)); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: "ok"}, nil
}
