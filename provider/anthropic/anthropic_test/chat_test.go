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

func TestChatNonStreaming(t *testing.T) {
	var capturedReq map[string]any
	var capturedHeaders http.Header

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)

		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-opus-5",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Hello, how can I assist you today?"},
			},
			"usage": map[string]any{
				"input_tokens":  25,
				"output_tokens": 12,
			},
		})
	})

	req := provider.Request{
		Model: "claude-opus-5",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "Hi"},
		},
	}

	resp, err := fix.client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if capturedHeaders.Get("x-api-key") != "test-key" {
		t.Errorf("x-api-key header = %q, want 'test-key'", capturedHeaders.Get("x-api-key"))
	}
	if capturedHeaders.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version header = %q, want '2023-06-01'", capturedHeaders.Get("anthropic-version"))
	}
	if capturedReq["model"] != "claude-opus-5" {
		t.Errorf("request model = %v, want 'claude-opus-5'", capturedReq["model"])
	}
	if capturedReq["max_tokens"] != float64(anthropic.DefaultMaxTokensNonStreaming) {
		t.Errorf("request max_tokens = %v, want non-streaming default", capturedReq["max_tokens"])
	}
	reqMsgs, ok := capturedReq["messages"].([]any)
	if !ok || len(reqMsgs) != 1 {
		t.Fatalf("request messages = %v, want 1", capturedReq["messages"])
	}
	if m, ok := reqMsgs[0].(map[string]any); !ok || m["role"] != "user" {
		t.Errorf("request messages[0] = %v, want user turn", reqMsgs[0])
	}
	if resp.Message.Content != "Hello, how can I assist you today?" {
		t.Errorf("resp.Message.Content = %q", resp.Message.Content)
	}
	if resp.Usage.PromptTokens != 25 || resp.Usage.CompletionTokens != 12 || resp.Usage.TotalTokens != 37 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want 'end_turn'", resp.FinishReason)
	}
}

func TestChatThinkingBlock(t *testing.T) {
	var capturedBlock provider.ReasoningBlock
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-opus-5",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Let me think about this."},
				{"type": "text", "text": "Result."},
			},
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 10,
			},
		})
	})

	cRedact, err := anthropic.New(anthropic.Options{
		APIKey:          "test-key",
		BaseURL:         fix.baseURL,
		HTTPClient:      fix.httpCli,
		ExposeReasoning: false,
		OnReasoning: func(rb provider.ReasoningBlock) {
			capturedBlock = rb
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := cRedact.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.ReasoningContent != "" {
		t.Errorf("ReasoningContent = %q, want empty when ExposeReasoning is false", resp.Message.ReasoningContent)
	}
	if !capturedBlock.Redacted || capturedBlock.Content != "" {
		t.Errorf("OnReasoning block = %+v, want redacted with empty content", capturedBlock)
	}

	cExpose, err := anthropic.New(anthropic.Options{
		APIKey:          "test-key",
		BaseURL:         fix.baseURL,
		HTTPClient:      fix.httpCli,
		ExposeReasoning: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	respExpose, err := cExpose.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if respExpose.Message.ReasoningContent != "Let me think about this." {
		t.Errorf("ReasoningContent = %q, want 'Let me think about this.'", respExpose.Message.ReasoningContent)
	}
}

func TestChatPromptCaching(t *testing.T) {
	var capturedReq map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)

		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-opus-5",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "Cached response"},
			},
			"usage": map[string]any{
				"input_tokens":                100,
				"output_tokens":               15,
				"cache_creation_input_tokens": 50,
				"cache_read_input_tokens":     40,
			},
		})
	})

	req := provider.Request{
		ReasoningDialect: provider.ReasoningDialect(provider.CacheStyleExplicit),
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "System prompt 1"},
			{Role: provider.RoleSystem, Content: "System prompt 2"},
			{Role: provider.RoleUser, Content: "User message"},
		},
		Tools: []provider.ToolDefinition{
			{Name: "tool1", Description: "desc1"},
			{Name: "tool2", Description: "desc2"},
		},
	}

	resp, err := fix.client.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	sys, ok := capturedReq["system"].([]any)
	if !ok || len(sys) != 2 {
		t.Fatalf("system blocks = %v, want 2", capturedReq["system"])
	}
	lastSys := sys[1].(map[string]any)
	if cc, ok := lastSys["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Errorf("last system cache_control = %v, want ephemeral", lastSys["cache_control"])
	}

	tools, ok := capturedReq["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v, want 2", capturedReq["tools"])
	}
	lastTool := tools[1].(map[string]any)
	if cc, ok := lastTool["cache_control"].(map[string]any); !ok || cc["type"] != "ephemeral" {
		t.Errorf("last tool cache_control = %v, want ephemeral", lastTool["cache_control"])
	}

	if !resp.CacheUsage.Reported {
		t.Errorf("CacheUsage.Reported = false, want true")
	}
	if resp.CacheUsage.CachedInputTokens != 40 || resp.CacheUsage.CacheWriteTokens != 50 {
		t.Errorf("CacheUsage = %+v", resp.CacheUsage)
	}
	if resp.Usage.CachedTokens != 40 {
		t.Errorf("Usage.CachedTokens = %d, want 40", resp.Usage.CachedTokens)
	}
}

func TestChatRefusal(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_refused",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-opus-5",
			"stop_reason": "refusal",
			"stop_details": map[string]any{
				"category":    "safety_policy",
				"explanation": "Cannot comply with request.",
			},
			"content": []map[string]any{},
			"usage": map[string]any{
				"input_tokens":  5,
				"output_tokens": 0,
			},
		})
	})

	_, err := fix.client.Chat(context.Background(), provider.Request{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, anthropic.ErrRefused) {
		t.Fatalf("err = %v, want errors.Is ErrRefused", err)
	}
	if err.Error() != "anthropic: model refused request: safety_policy" {
		t.Errorf("error message = %q, want 'anthropic: model refused request: safety_policy'", err.Error())
	}
}

func TestChatRateLimitRetry(t *testing.T) {
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att == 1 {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error": map[string]any{
					"type":    "rate_limit_error",
					"message": "Number of request tokens has exceeded your per-minute rate limit",
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_success",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage":       map[string]any{"input_tokens": 5, "output_tokens": 2},
		})
	})

	resp, err := fix.client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if resp.Message.Content != "ok" {
		t.Errorf("content = %q, want 'ok'", resp.Message.Content)
	}
}

func TestChatServerRetrySuccess(t *testing.T) {
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att <= 2 {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]any{
					"type":    "api_error",
					"message": fmt.Sprintf("internal server error %d", att),
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_recovered",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "recovered"}},
			"usage":       map[string]any{"input_tokens": 5, "output_tokens": 2},
		})
	})

	resp, err := fix.client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if resp.Message.Content != "recovered" {
		t.Errorf("content = %q, want 'recovered'", resp.Message.Content)
	}
}

func TestChatBadRequestNoRetry(t *testing.T) {
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "max_tokens too large",
			},
		})
	})

	_, err := fix.client.Chat(context.Background(), provider.Request{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, anthropic.ErrBadRequest) {
		t.Fatalf("err = %v, want errors.Is ErrBadRequest", err)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("attempts = %d, want 1 (no retry)", attempts)
	}
}

func TestRunTurnAdapter(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "msg_runturn",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "turn completed"}},
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
	})

	resp, err := provider.RunTurn(context.Background(), fix.client, provider.Request{
		Stream: false,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "do turn"},
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if resp.Message.Content != "turn completed" {
		t.Errorf("content = %q, want 'turn completed'", resp.Message.Content)
	}
}
