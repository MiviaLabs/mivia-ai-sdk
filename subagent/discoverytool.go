// DiscoveryTool matches capability cards from a tool call.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// DiscoveryCommand is the JSON wire form of one discovery tool call.
type DiscoveryCommand struct {
	Op   string `json:"op"`
	Card string `json:"card"`
	Need string `json:"need"`
}

// Discovery operation constants.
const (
	// OpMatch parses Card and reports the capability Need matches.
	OpMatch = "match"
)

// noCapability reports a card with no matching capability.
const noCapability = "none"

// DiscoveryTool returns a stateless tool that parses one capability
// card per call and matches it against one need. Routing a match to
// a transport choice stays caller code.
func DiscoveryTool(name string) tools.Tool {
	return &discoveryTool{name: name}
}

// discoveryTool matches cards against needs.
type discoveryTool struct {
	name string
}

// Name returns the registry name.
func (t *discoveryTool) Name() string { return t.name }

// Run executes one decoded command.
func (t *discoveryTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	var cmd DiscoveryCommand
	if err := decodeCommand(stringValue(in), &cmd); err != nil {
		return tools.Out{}, badCommand(t.name)
	}
	if cmd.Op != OpMatch {
		return tools.Out{}, badCommand(t.name)
	}
	card, err := discovery.Parse([]byte(cmd.Card))
	if err != nil {
		return tools.Out{}, err
	}
	if cap, ok := card.Match(cmd.Need); ok {
		return tools.Out{Value: cap}, nil
	}
	return tools.Out{Value: noCapability}, nil
}
