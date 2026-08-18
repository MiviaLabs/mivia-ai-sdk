package provider_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// twoTurnCompleter simulates a tool-call turn followed by a plain text
// turn, distinguishing the two by whether Request.Messages already
// carries a RoleTool reply.
type twoTurnCompleter struct{}

func (twoTurnCompleter) Name() string { return "two-turn" }

func (twoTurnCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			return provider.Response{
				Model:        "test-model",
				Message:      provider.Message{Role: provider.RoleAssistant, Content: "the weather is sunny"},
				FinishReason: "stop",
			}, nil
		}
	}
	return provider.Response{
		Model: "test-model",
		Message: provider.Message{
			Role: provider.RoleAssistant,
		},
		ToolCalls: []provider.ToolCall{
			{Index: 0, ID: "call-weather-1", Name: "get_weather", Arguments: []byte(`{"city":"paris"}`)},
		},
		FinishReason: "tool_calls",
	}, nil
}

func (twoTurnCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	panic("not used by this integration test")
}

func TestRunTurnComposesAcrossTwoTurns(t *testing.T) {
	c := twoTurnCompleter{}
	req := provider.Request{
		Model:    "test-model",
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "what's the weather in paris?"}},
	}

	first, err := provider.RunTurn(context.Background(), c, req)
	if err != nil {
		t.Fatalf("first RunTurn() error = %v, want nil", err)
	}
	if len(first.ToolCalls) != 1 {
		t.Fatalf("first Response.ToolCalls len = %d, want 1", len(first.ToolCalls))
	}
	call := first.ToolCalls[0]
	if call.Name != "get_weather" {
		t.Fatalf("ToolCalls[0].Name = %q, want %q", call.Name, "get_weather")
	}

	req.Messages = append(req.Messages, provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: call.ID,
		Content:    `{"forecast":"sunny"}`,
	})

	second, err := provider.RunTurn(context.Background(), c, req)
	if err != nil {
		t.Fatalf("second RunTurn() error = %v, want nil", err)
	}
	if second.FinishReason != "stop" {
		t.Fatalf("second Response.FinishReason = %q, want %q", second.FinishReason, "stop")
	}
	if len(second.ToolCalls) != 0 {
		t.Fatalf("second Response.ToolCalls len = %d, want 0", len(second.ToolCalls))
	}
	if second.Message.Content != "the weather is sunny" {
		t.Fatalf("second Response.Message.Content = %q, want %q", second.Message.Content, "the weather is sunny")
	}
}
