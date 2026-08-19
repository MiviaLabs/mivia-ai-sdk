// WorkspaceStatTool stats one path in a caller-bound Workspace.

package subagent

import (
	"context"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// WorkspaceStatArgs is the decoded argument struct for
// WorkspaceStatTool. Path is relative to the bound Workspace's root.
type WorkspaceStatArgs struct {
	Path string `json:"path"`
}

// WorkspaceFileInfo is one path's stat result WorkspaceStatTool
// returns.
type WorkspaceFileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}

// WorkspaceStatTool returns a tool that stats one path inside ft's
// bound workspace, relative to its root. Not privileged.
func WorkspaceStatTool(name string, ft *FileTools) tools.Tool {
	return &workspaceStatTool{name: name, ft: ft}
}

// workspaceStatTool adapts one workspace stat to tools.Tool,
// tools.SchemaTool, and tools.ProfiledTool.
type workspaceStatTool struct {
	name string
	ft   *FileTools
}

// Name returns the registry name.
func (t *workspaceStatTool) Name() string { return t.name }

// ParameterSchema returns the flat, string-only argument schema.
func (t *workspaceStatTool) ParameterSchema() []byte {
	return flatStringSchema("path")
}

// DecodeArguments parses raw into WorkspaceStatArgs.
func (t *workspaceStatTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	args, err := decodeArgs[WorkspaceStatArgs](t.name, raw)
	if err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args}, nil
}

// ExecutionProfile publishes ExecutionClassRead.
func (t *workspaceStatTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassRead}
}

// Run stats args.Path, relative to t.ft's bound root.
func (t *workspaceStatTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(WorkspaceStatArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	info, err := t.ft.ws.Stat(args.Path)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: WorkspaceFileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}}, nil
}
