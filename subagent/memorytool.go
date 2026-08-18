// MemoryTool exchanges blobs through the shared context store.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// MemoryCommand is the JSON wire form of one memory tool call.
type MemoryCommand struct {
	Op   string `json:"op"`
	Data string `json:"data"`
	Ref  string `json:"ref"`
}

// Memory operation constants.
const (
	// OpPut stores Data and returns its content-addressed ref.
	OpPut = "put"
	// OpGet returns the bytes stored under Ref.
	OpGet = "get"
)

// MemoryTool returns a tool bound to one store. Put returns the new
// ref string; get returns the stored bytes. Both commands travel as
// a JSON-encoded MemoryCommand in the tool input.
func MemoryTool(name string, s *memory.Store) tools.Tool {
	return &memoryTool{name: name, store: s}
}

// memoryTool adapts one store to the tools.Tool interface.
type memoryTool struct {
	name  string
	store *memory.Store
}

// Name returns the registry name.
func (t *memoryTool) Name() string { return t.name }

// Run executes one decoded command.
func (t *memoryTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	var cmd MemoryCommand
	if err := decodeCommand(stringValue(in), &cmd); err != nil {
		return tools.Out{}, badCommand(t.name)
	}
	switch cmd.Op {
	case OpPut:
		ref, err := t.store.Put([]byte(cmd.Data))
		if err != nil {
			return tools.Out{}, err
		}
		return tools.Out{Value: ref}, nil
	case OpGet:
		blob, err := t.store.Get(cmd.Ref)
		if err != nil {
			return tools.Out{}, err
		}
		return tools.Out{Value: string(blob)}, nil
	default:
		return tools.Out{}, badCommand(t.name)
	}
}
