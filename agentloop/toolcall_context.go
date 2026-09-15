package agentloop

import (
	"context"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

type toolCallKey struct{}

// ToolCallKey returns the canonical lookup key for one tool call: its
// ID, or its Name when ID is empty. It is the canonical key for per-call
// outcome recording and correlation across providers, including streams
// that deliver the tool-call name delta before or without an ID delta.
func ToolCallKey(call provider.ToolCall) string {
	if call.ID != "" {
		return call.ID
	}
	return call.Name
}

// WithToolCall attaches a provider.ToolCall to ctx. Exported so a Tool
// wrapped by an external caller's own tools.Tool implementation can
// recover the in-flight call's identity from the ctx this package
// passes into Tool.Run during tool dispatch (toolcall.go
// decodeAndRun).
func WithToolCall(ctx context.Context, call provider.ToolCall) context.Context {
	return context.WithValue(ctx, toolCallKey{}, call)
}

// ToolCallFromContext extracts the provider.ToolCall attached by
// WithToolCall. Returns provider.ToolCall{}, false for a nil ctx or
// when absent.
func ToolCallFromContext(ctx context.Context) (provider.ToolCall, bool) {
	if ctx == nil {
		return provider.ToolCall{}, false
	}
	val, ok := ctx.Value(toolCallKey{}).(provider.ToolCall)
	return val, ok
}
