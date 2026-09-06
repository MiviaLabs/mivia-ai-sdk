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

// fullOptionsRequest builds a request touching every request-level option.
func fullOptionsRequest() provider.Request {
	temp := 0.7
	maxTok := 500
	return provider.Request{
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
}

func TestRequestBodyOptions(t *testing.T) {
	var capturedReq map[string]any

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_cov", "type": "message", "role": "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 10},
		})
	})

	_, err := fix.client.Chat(context.Background(), fullOptionsRequest())
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if tc, ok := capturedReq["tool_choice"].(map[string]any); !ok || tc["type"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", capturedReq["tool_choice"])
	}
	if capturedReq["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", capturedReq["temperature"])
	}
	if capturedReq["max_tokens"] != float64(500) {
		t.Errorf("max_tokens = %v, want 500", capturedReq["max_tokens"])
	}
	if capturedReq["thinking"] == nil {
		t.Errorf("thinking missing, want adaptive block for high effort")
	}
	if out, ok := capturedReq["output_config"].(map[string]any); !ok || out["effort"] != "high" {
		t.Errorf("output_config = %v, want effort high", capturedReq["output_config"])
	}

	tools, ok := capturedReq["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v, want 2", capturedReq["tools"])
	}
	fn2, ok := tools[1].(map[string]any)
	if !ok {
		t.Fatalf("tools[1] = %v", tools[1])
	}
	schema, ok := fn2["input_schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Errorf("fn2 input_schema = %v, want default object schema", fn2["input_schema"])
	}
}

func TestRequestBodyTurnShape(t *testing.T) {
	var capturedReq map[string]any

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_cov", "type": "message", "role": "assistant",
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{"type": "text", "text": "using tool"},
				{"type": "tool_use", "id": "t1", "name": "fn1", "input": map[string]any{"a": 1}},
			},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 10},
		})
	})

	resp, err := fix.client.Chat(context.Background(), fullOptionsRequest())
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "t1" {
		t.Fatalf("resp.ToolCalls = %+v, want one call t1", resp.ToolCalls)
	}

	msgs, ok := capturedReq["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %v, want array", capturedReq["messages"])
	}
	// user, assistant(tool_use x2), user(tool_result x2 merged)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3", len(msgs))
	}
	asst, ok := msgs[1].(map[string]any)
	if !ok || asst["role"] != "assistant" {
		t.Fatalf("messages[1] = %v, want assistant turn", msgs[1])
	}
	parts, ok := asst["content"].([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("assistant content = %v, want 3 parts (text + 2 tool_use)", asst["content"])
	}
	toolUse2, ok := parts[2].(map[string]any)
	if !ok {
		t.Fatalf("assistant part[2] = %v, want tool_use", parts[2])
	}
	input, ok := toolUse2["input"].(map[string]any)
	if !ok || len(input) != 0 {
		t.Errorf("empty-arguments tool_use input = %v, want {}", toolUse2["input"])
	}
	merged, ok := msgs[2].(map[string]any)
	if !ok || merged["role"] != "user" {
		t.Fatalf("messages[2] = %v, want merged user turn", msgs[2])
	}
	mergedParts, ok := merged["content"].([]any)
	if !ok || len(mergedParts) != 2 {
		t.Fatalf("merged user content = %v, want 2 tool_result parts", merged["content"])
	}
	resErr, ok := mergedParts[1].(map[string]any)
	if !ok || resErr["is_error"] != true {
		t.Errorf("tool_result[1] = %v, want is_error true", mergedParts[1])
	}
}

func TestRequestBodyDefaults(t *testing.T) {
	var capturedReq map[string]any

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_cov", "type": "message", "role": "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})

	reqNone := provider.Request{
		ToolChoice: provider.ToolChoiceNone,
		Messages: []provider.Message{
			{Role: provider.RoleTool, ToolCallID: "standalone", Content: "standalone result"},
		},
	}
	if _, err := fix.client.Chat(context.Background(), reqNone); err != nil {
		t.Fatalf("Chat reqNone: %v", err)
	}
	if tc, ok := capturedReq["tool_choice"].(map[string]any); !ok || tc["type"] != "none" {
		t.Errorf("tool_choice = %v, want none", capturedReq["tool_choice"])
	}
	if capturedReq["max_tokens"] != float64(anthropic.DefaultMaxTokensNonStreaming) {
		t.Errorf("max_tokens = %v, want non-streaming default", capturedReq["max_tokens"])
	}
	standaloneMsgs, ok := capturedReq["messages"].([]any)
	if !ok || len(standaloneMsgs) != 1 {
		t.Fatalf("standalone messages = %v, want 1", capturedReq["messages"])
	}
	if standalone, ok := standaloneMsgs[0].(map[string]any); !ok || standalone["role"] != "user" {
		t.Errorf("standalone tool message = %v, want user turn", standaloneMsgs[0])
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
