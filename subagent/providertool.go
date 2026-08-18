// ProviderTool runs one model turn from a tool call.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// ProviderTool returns a tool bound to one caller-supplied
// Completer. The input string is the prompt; the result is the
// assistant reply's content. No concrete client ships in this SDK;
// the Completer is always caller code.
func ProviderTool(name string, c provider.Completer) tools.Tool {
	return &providerTool{name: name, completer: c}
}

// providerTool adapts one Completer to the tools.Tool interface.
type providerTool struct {
	name      string
	completer provider.Completer
}

// Name returns the registry name.
func (t *providerTool) Name() string { return t.name }

// Run completes one turn over the prompt.
func (t *providerTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	prompt := stringValue(in)
	req := provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: prompt}},
	}
	resp, err := provider.RunTurn(ctx, t.completer, req)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: resp.Message.Content}, nil
}
