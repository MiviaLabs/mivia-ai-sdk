package toolcallctx_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/toolcallctx"
)

func TestToolCallContextRoundTrip(t *testing.T) {
	call := provider.ToolCall{
		ID:        "call_123",
		Name:      "test_tool",
		Arguments: []byte(`{"param":"value"}`),
	}

	ctx := toolcallctx.WithToolCall(context.Background(), call)
	got, ok := toolcallctx.ToolCallFromContext(ctx)
	if !ok {
		t.Fatal("expected ToolCallFromContext to return true")
	}
	if got.ID != call.ID || got.Name != call.Name || string(got.Arguments) != string(call.Arguments) {
		t.Fatalf("got %+v, want %+v", got, call)
	}

	// Nil context or context without value
	if _, ok := toolcallctx.ToolCallFromContext(nil); ok {
		t.Fatal("expected ToolCallFromContext(nil) to return false")
	}
	if _, ok := toolcallctx.ToolCallFromContext(context.Background()); ok {
		t.Fatal("expected ToolCallFromContext(empty ctx) to return false")
	}
}
