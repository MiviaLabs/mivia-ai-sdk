package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

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
	if !strings.Contains(err.Error(), "unexpected HTTP status 404") {
		t.Errorf("err = %v, want unexpected HTTP status 404", err)
	}

	// 404 error on stream (non-retryable)
	_, err = fix404.client.ChatStream(context.Background(), provider.Request{})
	if err == nil {
		t.Fatal("expected 404 error on stream, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected HTTP status 404") {
		t.Errorf("stream err = %v, want unexpected HTTP status 404", err)
	}
}

func TestTransportErrorRetry(t *testing.T) {
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			// Kill the connection: a transport error, not an HTTP status.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_retry", "type": "message", "role": "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "recovered"}},
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})

	resp, err := fix.client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if resp.Message.Content != "recovered" {
		t.Errorf("content = %q, want 'recovered'", resp.Message.Content)
	}
}

func TestStreamingMaxTokensDefaults(t *testing.T) {
	var capturedReq map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: message_stop\ndata: {}\n\n")
			flusher.Flush()
		}
	})

	// Default streaming limit when neither side sets one.
	ch, err := fix.client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	for range ch {
	}
	if capturedReq["max_tokens"] != float64(anthropic.DefaultMaxTokensStreaming) {
		t.Errorf("max_tokens = %v, want streaming default", capturedReq["max_tokens"])
	}

	// Options override wins over the default.
	cOverride, err := anthropic.New(anthropic.Options{
		APIKey:             "test-key",
		BaseURL:            fix.baseURL,
		HTTPClient:         fix.httpCli,
		MaxTokensStreaming: 777,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	chOverride, err := cOverride.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream override: %v", err)
	}
	for range chOverride {
	}
	if capturedReq["max_tokens"] != float64(777) {
		t.Errorf("max_tokens = %v, want 777", capturedReq["max_tokens"])
	}

	// Request-level MaxTokens wins over everything.
	maxTok := 42
	chReq, err := fix.client.ChatStream(context.Background(), provider.Request{MaxTokens: &maxTok})
	if err != nil {
		t.Fatalf("ChatStream request tokens: %v", err)
	}
	for range chReq {
	}
	if capturedReq["max_tokens"] != float64(42) {
		t.Errorf("max_tokens = %v, want 42", capturedReq["max_tokens"])
	}
}
