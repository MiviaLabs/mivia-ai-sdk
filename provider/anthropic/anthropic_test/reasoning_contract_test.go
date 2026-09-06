package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

func TestReasoningEffortValidation(t *testing.T) {
	valid := []provider.ReasoningEffort{
		provider.ReasoningEffortLow, provider.ReasoningEffortMedium,
		provider.ReasoningEffortHigh, provider.ReasoningEffortXHigh, provider.ReasoningEffortMax,
	}
	for _, effort := range valid {
		var capturedReq map[string]any
		_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &capturedReq)
			writeJSON(w, http.StatusOK, map[string]any{
				"id": "msg_e", "type": "message", "role": "assistant",
				"stop_reason": "end_turn",
				"content":     []map[string]any{{"type": "text", "text": "ok"}},
				"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
			})
		})
		_, err := fix.client.Chat(context.Background(), provider.Request{ReasoningEffort: effort})
		if err != nil {
			t.Fatalf("Chat effort %q: %v", effort, err)
		}
		out, ok := capturedReq["output_config"].(map[string]any)
		if !ok || out["effort"] != string(effort) {
			t.Errorf("effort %q: output_config = %v", effort, capturedReq["output_config"])
		}
	}

	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for an invalid effort")
	})
	_, err := fix.client.Chat(context.Background(), provider.Request{ReasoningEffort: "bogus"})
	if !errors.Is(err, anthropic.ErrInvalidOptions) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidOptions", err)
	}
}

// TestThinkingDisplaySummarized pins that ExposeReasoning and
// OnReasoning both ask the API for the readable "summarized" thinking
// form; without it the default "omitted" display leaves thinking text
// empty even though a client asked to read it.
func TestThinkingDisplaySummarized(t *testing.T) {
	var capturedReq map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_d", "type": "message", "role": "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})

	client, err := anthropic.New(anthropic.Options{
		APIKey:          "test-key",
		BaseURL:         fix.baseURL,
		HTTPClient:      fix.httpCli,
		ExposeReasoning: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = client.Chat(context.Background(), provider.Request{ReasoningEffort: provider.ReasoningEffortHigh})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	thinking, ok := capturedReq["thinking"].(map[string]any)
	if !ok || thinking["display"] != "summarized" {
		t.Errorf("thinking = %v, want display summarized", capturedReq["thinking"])
	}

	// Without ExposeReasoning or OnReasoning, no display field is sent.
	var capturedReq2 map[string]any
	_, fix2 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq2)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_d2", "type": "message", "role": "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})
	_, err = fix2.client.Chat(context.Background(), provider.Request{ReasoningEffort: provider.ReasoningEffortHigh})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	thinking2, ok := capturedReq2["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %v, want an adaptive block", capturedReq2["thinking"])
	}
	if _, hasDisplay := thinking2["display"]; hasDisplay {
		t.Errorf("thinking = %v, want no display field", thinking2)
	}
}
