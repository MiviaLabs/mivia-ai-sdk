// DiffTool previews a write: diffs on-disk content against
// caller-model-supplied proposed content.

package subagent

import (
	"context"
	"errors"
	"io/fs"

	"github.com/MiviaLabs/mivia-ai-sdk/diff"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// DiffArgs is the decoded argument struct for DiffTool. Path is
// relative to the bound Workspace's root; Content is the proposed
// replacement content.
type DiffArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// DiffTool returns a tool that diffs the on-disk content at a path
// inside ft's bound workspace, relative to its root, against proposed
// content, through diff.Unified. A path that does not yet exist diffs
// against empty content. maxLines passes straight to diff.Unified.
// Not privileged: it reads two pieces of content and writes nothing.
func DiffTool(name string, ft *FileTools, maxLines int) tools.Tool {
	return &diffTool{name: name, ft: ft, maxLines: maxLines}
}

// diffTool adapts one workspace-bound diff to tools.Tool,
// tools.SchemaTool, and tools.ProfiledTool.
type diffTool struct {
	name     string
	ft       *FileTools
	maxLines int
}

// Name returns the registry name.
func (t *diffTool) Name() string { return t.name }

// ParameterSchema returns the flat, string-only argument schema.
func (t *diffTool) ParameterSchema() []byte {
	return flatStringSchema("path", "content")
}

// DecodeArguments parses raw into DiffArgs.
func (t *diffTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	args, err := decodeArgs[DiffArgs](t.name, raw)
	if err != nil {
		return tools.InOut{}, err
	}
	return tools.InOut{Value: args}, nil
}

// ExecutionProfile publishes ExecutionClassRead.
func (t *diffTool) ExecutionProfile() tools.ExecutionProfile {
	return tools.ExecutionProfile{Class: tools.ExecutionClassRead}
}

// Run diffs the on-disk content at args.Path against args.Content. A
// not-yet-existing path (ws.ReadFile returns a wrapped
// fs.ErrNotExist) diffs against empty content; any other ws.ReadFile
// error, including workspace.ErrEscape and workspace.ErrSecretPath,
// propagates unchanged. A diff over t.maxLines returns
// diff.ErrTooLarge unchanged.
func (t *diffTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	args, ok := in.Value.(DiffArgs)
	if !ok {
		return tools.Out{}, badArguments(t.name)
	}
	current, err := t.ft.ws.ReadFile(args.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return tools.Out{}, err
		}
		current = nil
	}
	rendered, err := diff.Unified(args.Path, current, []byte(args.Content), t.maxLines)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: rendered}, nil
}
