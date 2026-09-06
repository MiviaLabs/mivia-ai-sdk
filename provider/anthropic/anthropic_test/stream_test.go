package anthropic_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestChatStreamingText(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		sendEvent := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		sendEvent("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","usage":{"input_tokens":10}}}`)
		sendEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		sendEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)
		sendEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world!"}}`)
		sendEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)
		sendEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
		sendEvent("message_stop", `{"type":"message_stop"}`)
	})

	ch, err := fix.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var chunks []provider.Chunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 3 {
		t.Fatalf("received %d chunks, want >= 3", len(chunks))
	}

	fullText := ""
	var termChunk *provider.Chunk
	for _, c := range chunks {
		fullText += c.Delta
		if c.Done {
			termChunk = &c
		}
	}

	if fullText != "Hello world!" {
		t.Errorf("fullText = %q, want 'Hello world!'", fullText)
	}
	if termChunk == nil {
		t.Fatal("terminal chunk not found")
	}
	if termChunk.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want 'end_turn'", termChunk.FinishReason)
	}
	if termChunk.Usage.PromptTokens != 10 || termChunk.Usage.CompletionTokens != 5 {
		t.Errorf("termChunk.Usage = %+v", termChunk.Usage)
	}
}

func TestChatStreamingToolCall(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		sendEvent := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		sendEvent("message_start", `{"type":"message_start","message":{"id":"msg_tc","type":"message","role":"assistant","usage":{"input_tokens":15}}}`)
		sendEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_123","name":"get_weather"}}`)
		sendEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`)
		sendEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ation\": \"San "}}`)
		sendEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"Francisco\"}"}}`)
		sendEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)
		sendEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`)
		sendEvent("message_stop", `{"type":"message_stop"}`)
	})

	ch, err := fix.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var tcChunk *provider.Chunk
	for chunk := range ch {
		if chunk.ToolCallDelta != nil {
			tcChunk = &chunk
		}
	}

	if tcChunk == nil {
		t.Fatal("expected tool call chunk, got nil")
	}
	tc := tcChunk.ToolCallDelta
	if tc.ID != "call_123" || tc.Name != "get_weather" {
		t.Errorf("tc ID=%q, Name=%q", tc.ID, tc.Name)
	}
	if string(tc.Arguments) != `{"location": "San Francisco"}` {
		t.Errorf("tc Arguments = %s, want %s", string(tc.Arguments), `{"location": "San Francisco"}`)
	}
}

// TestChatStreamingCumulativeUsage pins the contract that a
// message_delta's usage.output_tokens is a cumulative running total:
// the last event's value is the turn's output-token count, never a
// sum over the events.
func TestChatStreamingCumulativeUsage(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		sendEvent := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			flusher.Flush()
		}

		sendEvent("message_start", `{"type":"message_start","message":{"id":"msg_cum","type":"message","role":"assistant","usage":{"input_tokens":7}}}`)
		sendEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)
		sendEvent("message_delta", `{"type":"message_delta","delta":{},"usage":{"output_tokens":10}}`)
		sendEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":25}}`)
		sendEvent("message_stop", `{"type":"message_stop"}`)
	})

	ch, err := fix.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var termChunk *provider.Chunk
	for chunk := range ch {
		if chunk.Done {
			termChunk = &chunk
		}
	}
	if termChunk == nil {
		t.Fatal("terminal chunk not found")
	}
	if termChunk.Usage.CompletionTokens != 25 {
		t.Errorf("CompletionTokens = %d, want 25 (the last cumulative value)", termChunk.Usage.CompletionTokens)
	}
	if termChunk.Usage.TotalTokens != 32 {
		t.Errorf("TotalTokens = %d, want 32", termChunk.Usage.TotalTokens)
	}
}

func TestChatStreamDisconnect(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: message_start\ndata: {}\n\n")
			f.Flush()
		}
		// Close without sending message_stop or terminal event
	})

	ch, err := fix.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var hasTerminalDone bool
	for c := range ch {
		if c.Done {
			hasTerminalDone = true
		}
	}
	if hasTerminalDone {
		t.Error("unexpected Done terminal chunk on mid-stream disconnect")
	}
}

func TestChatStreamContextCancel(t *testing.T) {
	releaseServer := make(chan struct{})
	serverDone := make(chan struct{})

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: message_start\ndata: {}\n\n")
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
			f.Flush()
		}
		select {
		case <-releaseServer:
		case <-r.Context().Done():
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := fix.client.ChatStream(ctx, provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// Read the text chunk deterministically
	<-ch

	cancel()

	// Wait for channel to close deterministically
	for range ch {
	}

	close(releaseServer)
	<-serverDone
}
