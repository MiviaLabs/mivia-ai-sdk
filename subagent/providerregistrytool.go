// ProviderRegistryTool runs one model turn over a fallback order.

package subagent

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// ProviderRegistryTool returns a tool bound to a named-provider
// registry. Each run routes one turn through the caller's order and
// falls through to the next name only when retryable approves the
// failure; the result is the answering provider's reply content.
// Wrap the registered completers with usage.WrapCompleter to gain
// per-session totals under this seam.
func ProviderRegistryTool(
	name string, reg *providerregistry.Registry, order []string,
	retryable providerregistry.Retryable,
) tools.Tool {
	return &providerRegistryTool{name: name, reg: reg, order: order, retryable: retryable}
}

// providerRegistryTool adapts a registry route to the tools.Tool
// interface.
type providerRegistryTool struct {
	name      string
	reg       *providerregistry.Registry
	order     []string
	retryable providerregistry.Retryable
}

// Name returns the registry name.
func (t *providerRegistryTool) Name() string { return t.name }

// Run routes one turn over the prompt through the fallback order.
func (t *providerRegistryTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	req := provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: stringValue(in)}},
	}
	resp, err := t.reg.Route(ctx, req, t.order, t.retryable)
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: resp.Message.Content}, nil
}
