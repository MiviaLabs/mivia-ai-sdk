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

	ctx := withToolCall(context.Background(), call)
	got, ok := toolCallFromContext(ctx)
	if !ok {
		t.Fatal("expected toolCallFromContext to return true")
	}
	if got.ID != call.ID || got.Name != call.Name || string(got.Arguments) != string(call.Arguments) {
		t.Fatalf("got %+v, want %+v", got, call)
	}

	// Nil context or context without value
	if _, ok := toolCallFromContext(nil); ok {
		t.Fatal("expected toolCallFromContext(nil) to return false")
	}
	if _, ok := toolCallFromContext(context.Background()); ok {
		t.Fatal("expected toolCallFromContext(empty ctx) to return false")
	}
}
