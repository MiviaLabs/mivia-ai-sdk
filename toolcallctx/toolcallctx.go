package toolcallctx

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

type toolCallKey struct{}

// WithToolCall attaches a provider.ToolCall to ctx.
func WithToolCall(ctx context.Context, call provider.ToolCall) context.Context {
	return context.WithValue(ctx, toolCallKey{}, call)
}

// ToolCallFromContext extracts the provider.ToolCall from ctx.
func ToolCallFromContext(ctx context.Context) (provider.ToolCall, bool) {
	if ctx == nil {
		return provider.ToolCall{}, false
	}
	val, ok := ctx.Value(toolCallKey{}).(provider.ToolCall)
	return val, ok
}
