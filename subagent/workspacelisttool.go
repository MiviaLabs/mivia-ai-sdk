// WorkspaceListTool lists one directory in a caller-bound Workspace.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// WorkspaceListArgs is the decoded argument struct for
// WorkspaceListTool. Path is relative to the bound Workspace's root;
// a blank Path lists the root itself.
type WorkspaceListArgs struct {
	Path string `json:"path"`
}

// WorkspaceListTool returns a tool that lists one directory inside
// ft's bound workspace, relative to its root. Not privileged.
func WorkspaceListTool(name string, ft *FileTools) tools.Tool {
	return &workspaceListTool{name: name, ft: ft}
}

// workspaceListTool adapts one workspace list to tools.Tool,
// tools.SchemaTool, and tools.ProfiledTool.
type workspaceListTool struct {
	name string
	ft   *FileTools
}

// Name returns the registry name.
func (t *workspaceListTool) Name() string { return t.name }

// ParameterSchema returns the flat, string-only argument schema.
func (t *workspaceListTool) ParameterSchema() []byte {
	return flatStringSchema("path")
}

// DecodeArguments parses raw into WorkspaceListArgs.
func (t *workspaceListTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	args, err := decodeArgs[WorkspaceListArgs](t.name, raw)
	if err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args}, nil
}

// ExecutionProfile publishes ExecutionClassRead.
func (t *workspaceListTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassRead}
}

// Run lists the directory at args.Path, relative to t.ft's bound
// root, sorted the way ws.List's underlying os.ReadDir already
// sorts. The result is a JSON-encoded string of []WorkspaceEntry, so
// agentrun's chain accepts it as a tool result.
func (t *workspaceListTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(WorkspaceListArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	entries, err := t.ft.ws.List(args.Path)
	if err != nil {
		return tools.Out{}, err
	}
	out := make([]WorkspaceEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, WorkspaceEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	encoded, err := encodeToolResult(out)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: encoded}, nil
}
