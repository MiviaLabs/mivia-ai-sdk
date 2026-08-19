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

// ChatStream mirrors Chat's branching over a Chunk channel: a tool-call
// turn streams as one ToolCallDelta fragment, a follow-up turn streams
// as plain-text deltas. Both terminate with a Done chunk, so a caller
// composing history through the streamed path exercises the same
// two-turn branch Chat does.
func (twoTurnCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 4)
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			ch <- provider.Chunk{Delta: "the weather is sunny"}
			ch <- provider.Chunk{Done: true, FinishReason: "stop"}
			close(ch)
			return ch, nil
		}
	}
	ch <- provider.Chunk{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-weather-1", Name: "get_weather", Arguments: []byte(`{"city":"paris"}`)}}
	ch <- provider.Chunk{Done: true, FinishReason: "tool_calls"}
	close(ch)
	return ch, nil
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

// streamedTwoTurnCompleter simulates a tool-call turn followed by a
// plain text turn over the streamed path. It distinguishes the two by
// whether Request.Messages already carries a RoleTool reply, the same
// way twoTurnCompleter does, but it drives RunTurn's streamed
// aggregation so resp.Message carries the merged tool calls.
type streamedTwoTurnCompleter struct{}

func (streamedTwoTurnCompleter) Name() string { return "streamed-two-turn" }

func (streamedTwoTurnCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	panic("not used by this integration test")
}

func (streamedTwoTurnCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		for _, m := range req.Messages {
			if m.Role == provider.RoleTool {
				ch <- provider.Chunk{Delta: "the weather is sunny"}
				ch <- provider.Chunk{Done: true, FinishReason: "stop"}
				return
			}
		}
		ch <- provider.Chunk{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-weather-1", Name: "get_weather", Arguments: []byte(`{"city":"paris"}`)}}
		ch <- provider.Chunk{Done: true, FinishReason: "tool_calls"}
	}()
	return ch, nil
}

// TestRunTurnComposesFullHistoryAcrossStreamedTurns appends the first
// streamed Response's Message directly to Request.Messages, replies
// with one RoleTool message per call, and runs a second turn over the
// full history. It proves the idiomatic append forms a legal history
// RunTurn accepts with no error.
func TestRunTurnComposesFullHistoryAcrossStreamedTurns(t *testing.T) {
	c := streamedTwoTurnCompleter{}
	req := provider.Request{
		Model:    "test-model",
		Stream:   true,
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
	if call.ID == "" {
		t.Fatal("first Response.ToolCalls[0].ID is empty, want a call id")
	}
	if len(first.Message.ToolCalls) != len(first.ToolCalls) {
		t.Fatalf("first Message.ToolCalls len = %d, want %d", len(first.Message.ToolCalls), len(first.ToolCalls))
	}

	req.Messages = append(req.Messages, first.Message)
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
	if second.Message.Content != "the weather is sunny" {
		t.Fatalf("second Response.Message.Content = %q, want %q", second.Message.Content, "the weather is sunny")
	}
}

// TestRunTurnComposesHistoryAppendedFromResponseMessageStreamed is the
// streamed counterpart to
// TestRunTurnComposesHistoryAppendedFromResponseMessage: it drives the
// same two-turn history-append composition through Request.Stream ==
// true, proving the streamed path's aggregated Message is equally a
// directly-appendable history entry against a real, branching
// Completer, not just a fake channel of canned chunks.
func TestRunTurnComposesHistoryAppendedFromResponseMessageStreamed(t *testing.T) {
	c := twoTurnCompleter{}
	history := []provider.Message{{Role: provider.RoleUser, Content: "what's the weather in paris?"}}
	req := provider.Request{Model: "test-model", Stream: true, Messages: history}

	first, err := provider.RunTurn(context.Background(), c, req)
	if err != nil {
		t.Fatalf("first RunTurn() error = %v, want nil", err)
	}
	history = append(history, first.Message)
	if len(first.Message.ToolCalls) != 1 {
		t.Fatalf("first Response.Message.ToolCalls len = %d, want 1", len(first.Message.ToolCalls))
	}
	call := first.Message.ToolCalls[0]

	history = append(history, provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: call.ID,
		Content:    `{"forecast":"sunny"}`,
	})

	req.Messages = history
	second, err := provider.RunTurn(context.Background(), c, req)
	if err != nil {
		t.Fatalf("second RunTurn() error = %v, want nil", err)
	}
	if second.Message.Content != "the weather is sunny" {
		t.Fatalf("second Response.Message.Content = %q, want %q", second.Message.Content, "the weather is sunny")
	}
}
