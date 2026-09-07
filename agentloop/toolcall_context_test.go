package agentloop

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestToolCallContextRoundTrip(t *testing.T) {
	call := provider.ToolCall{
		ID:        "call_123",
		Name:      "test_tool",
		Arguments: []byte(`{"param":"value"}`),
	}

	ctx := WithToolCall(context.Background(), call)
	got, ok := ToolCallFromContext(ctx)
	if !ok {
		t.Fatal("expected ToolCallFromContext to return true")
	}
	if got.ID != call.ID || got.Name != call.Name || string(got.Arguments) != string(call.Arguments) {
		t.Fatalf("got %+v, want %+v", got, call)
	}

	// Nil context or context without value
	if _, ok := ToolCallFromContext(nil); ok {
		t.Fatal("expected ToolCallFromContext(nil) to return false")
	}
	if _, ok := ToolCallFromContext(context.Background()); ok {
		t.Fatal("expected ToolCallFromContext(empty ctx) to return false")
	}
}
