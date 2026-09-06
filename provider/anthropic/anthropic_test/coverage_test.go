package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

func TestClientInterfacesAndDefaults(t *testing.T) {
	c, err := anthropic.New(anthropic.Options{
		APIKey:        "test-key",
		ContextWindow: 200000,
		DefaultEffort: provider.ReasoningEffortMedium,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if cw := c.ContextWindow(); cw != 200000 {
		t.Errorf("ContextWindow = %d, want 200000", cw)
	}
	if re := c.ReasoningEffort(); re != "medium" {
		t.Errorf("ReasoningEffort = %q, want 'medium'", re)
	}

	var _ provider.Completer = c
	var _ provider.ContextAccountant = c
	var _ provider.ReasoningPolicy = c
}

func TestRequestBodyOptions(t *testing.T) {
	var capturedReq map[string]any

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_cov",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{"type": "text", "text": "using tool"},
				{"type": "tool_use", "id": "t1", "name": "fn1", "input": map[string]any{"a": 1}},
			},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 10},
		})
	})

	temp := 0.7
	maxTok := 500
	req := provider.Request{
		Temperature:     &temp,
		MaxTokens:       &maxTok,
		ToolChoice:      provider.ToolChoiceAuto,
		ReasoningEffort: provider.ReasoningEffortHigh,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "user prompt"},
			{
				Role:    provider.RoleAssistant,
				Content: "calling tool",
				ToolCalls: []provider.ToolCall{
					{ID: "prev_t1", Name: "prev_fn", Arguments: []byte(`{"prev": true}`)},
					{ID: "prev_t2", Name: "prev_fn2"}, // empty args branch
				},
			},
			{Role: provider.RoleTool, ToolCallID: "prev_t1", Content: "tool output"},
			{Role: provider.RoleTool, ToolCallID: "prev_t2", Content: "[tool-error] failed"},
		},
		Tools: []provider.ToolDefinition{
			{Name: "fn1", Description: "function 1", Schema: []byte(`{"type":"object"}`)},
			{Name: "fn2"}, // empty schema branch
		},
	}

	resp, err := fix.client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("resp.ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "t1" {
		t.Errorf("resp.ToolCalls[0].ID = %q, want 't1'", resp.ToolCalls[0].ID)
	}

	// Also test ToolChoiceNone and default MaxTokens
	reqNone := provider.Request{
		ToolChoice: provider.ToolChoiceNone,
		Messages: []provider.Message{
			{Role: provider.RoleTool, ToolCallID: "standalone", Content: "standalone result"},
		},
	}
	_, err = fix.client.Chat(context.Background(), reqNone)
	if err != nil {
		t.Fatalf("Chat reqNone: %v", err)
	}
}

func TestStreamRetryAndErrors(t *testing.T) {
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att == 1 {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error": map[string]any{"type": "rate_limit_error", "message": "rate limit"},
			})
			return
		}
		if att == 2 {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]any{"type": "api_error", "message": "server error"},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"s1\",\"usage\":{\"cache_creation_input_tokens\":10,\"cache_read_input_tokens\":5,\"input_tokens\":20}}}\n\n")
			fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			flusher.Flush()
		}
	})

	c, err := anthropic.New(anthropic.Options{
		APIKey:     "test-key",
		BaseURL:    fix.baseURL,
		HTTPClient: fix.httpCli,
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := c.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var lastChunk provider.Chunk
	for chunk := range ch {
		if chunk.Done {
			lastChunk = chunk
		}
	}
	if !lastChunk.Done {
		t.Errorf("expected terminal Done chunk")
	}
	if !lastChunk.CacheUsage.Reported {
		t.Errorf("expected reported CacheUsage in stream terminal chunk")
	}
}

func TestStreamErrorEventsAndRefusal(t *testing.T) {
	// 1. Stream error event with anthropic error
	_, fixErr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: error\ndata: {\"error\":{\"message\":\"overloaded\"}}\n\n")
			flusher.Flush()
		}
	})

	ch, err := fixErr.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var errChunk *provider.Chunk
	for chunk := range ch {
		if chunk.Err != nil {
			errChunk = &chunk
		}
	}
	if errChunk == nil || errChunk.Err.Error() != "anthropic: overloaded" {
		t.Fatalf("expected overloaded stream error, got %v", errChunk)
	}

	// 2. Stream refusal
	_, fixRefusal := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"refusal\",\"stop_details\":{\"category\":\"hate_speech\"}}}\n\n")
			fmt.Fprintf(w, "event: message_stop\ndata: {}\n\n")
			flusher.Flush()
		}
	})

	chRef, err := fixRefusal.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var refChunk *provider.Chunk
	for chunk := range chRef {
		if chunk.Err != nil {
			refChunk = &chunk
		}
	}
	if refChunk == nil || !errors.Is(refChunk.Err, anthropic.ErrRefused) {
		t.Fatalf("expected ErrRefused, got %v", refChunk)
	}

	// 3. Raw stream error event
	_, fixRawErr := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: error\ndata: raw failure\n\n")
			flusher.Flush()
		}
	})
	chRaw, _ := fixRawErr.client.ChatStream(context.Background(), provider.Request{})
	for chunk := range chRaw {
		if chunk.Err == nil {
			t.Errorf("expected chunk error on raw failure event")
		}
	}
}

func TestHTTPErrorStatusMapping(t *testing.T) {
	testCases := []struct {
		status  int
		wantErr error
	}{
		{http.StatusUnauthorized, anthropic.ErrAuth},
		{http.StatusForbidden, anthropic.ErrAuth},
		{http.StatusRequestTimeout, anthropic.ErrServer},
		{http.StatusConflict, anthropic.ErrServer},
		{http.StatusBadGateway, anthropic.ErrServer},
	}

	for _, tc := range testCases {
		_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, tc.status, map[string]any{
				"error": map[string]any{"message": "err"},
			})
		})

		client, _ := anthropic.New(anthropic.Options{
			APIKey:     "test-key",
			BaseURL:    fix.baseURL,
			HTTPClient: fix.httpCli,
			MaxRetries: 0,
		})

		_, err := client.Chat(context.Background(), provider.Request{})
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("status %d: error %v, want %v", tc.status, err, tc.wantErr)
		}
	}
}

func TestStreamThinkingBlockRedacted(t *testing.T) {
	var capturedBlock provider.ReasoningBlock
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"streamed thoughts\"}}\n\n")
			fmt.Fprintf(w, "event: message_stop\ndata: {}\n\n")
			flusher.Flush()
		}
	})

	client, err := anthropic.New(anthropic.Options{
		APIKey:          "test-key",
		BaseURL:         fix.baseURL,
		HTTPClient:      fix.httpCli,
		ExposeReasoning: false,
		OnReasoning: func(b provider.ReasoningBlock) {
			capturedBlock = b
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	for range ch {
	}
	if !capturedBlock.Redacted {
		t.Errorf("expected captured reasoning block to be redacted")
	}
}

func TestPostWithRetryErrors(t *testing.T) {
	// 404 error (non-retryable)
	_, fix404 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]any{"message": "not found"},
		})
	})

	_, err := fix404.client.Chat(context.Background(), provider.Request{})
	if err == nil {
		t.Fatal("expected 404 error, got nil")
	}

	// 404 error on stream (non-retryable)
	_, err = fix404.client.ChatStream(context.Background(), provider.Request{})
	if err == nil {
		t.Fatal("expected 404 error on stream, got nil")
	}
}
