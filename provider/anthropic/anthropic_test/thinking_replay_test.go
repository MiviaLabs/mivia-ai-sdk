package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

// assistantWireTurn finds the assistant message in one captured wire
// request. It fails the test when no assistant turn exists.
func assistantWireTurn(t *testing.T, wireReq map[string]any) map[string]any {
	t.Helper()
	msgs, ok := wireReq["messages"].([]any)
	if !ok {
		t.Fatalf("wire request has no messages array: %v", wireReq)
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if mm["role"] == "assistant" {
			return mm
		}
	}
	t.Fatalf("no assistant turn in wire messages: %v", wireReq["messages"])
	return nil
}

// assertNoThinkingPart fails the test when any content part in the
// captured wire request carries the thinking type.
func assertNoThinkingPart(t *testing.T, wireReq map[string]any) {
	t.Helper()
	msgs, ok := wireReq["messages"].([]any)
	if !ok {
		t.Fatalf("wire request has no messages array: %v", wireReq)
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		content, ok := mm["content"].([]any)
		if !ok {
			continue
		}
		for _, p := range content {
			part, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if part["type"] == "thinking" {
				t.Fatalf("wire request carries a thinking part: %v", part)
			}
		}
	}
}

// exposeClient builds a client on the fixture server with
// ExposeReasoning on, so tests can also read the thinking text.
func exposeClient(t *testing.T, fix *testClientFixture) *anthropic.Client {
	t.Helper()
	client, err := anthropic.New(anthropic.Options{
		APIKey:          "test-key",
		BaseURL:         fix.baseURL,
		HTTPClient:      fix.httpCli,
		ExposeReasoning: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// captureThinkingToolLoopServer serves a thinking-plus-text-plus
// tool_use first turn and a plain text second turn, capturing every
// wire request.
func captureThinkingToolLoopServer(t *testing.T, captured *[]map[string]any) *testClientFixture {
	t.Helper()
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var wireReq map[string]any
		_ = json.Unmarshal(body, &wireReq)
		*captured = append(*captured, wireReq)
		if atomic.AddInt32(&attempts, 1) == 1 {
			writeJSON(w, http.StatusOK, map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant",
				"model": "claude-opus-5", "stop_reason": "tool_use",
				"content": []map[string]any{
					{"type": "thinking", "thinking": "Let me think about this.", "signature": "sig-abc"},
					{"type": "text", "text": "Calling the tool."},
					{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{"q": "cats"}},
				},
				"usage": map[string]any{"input_tokens": 10, "output_tokens": 20},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_2", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": "done"}},
			"usage":   map[string]any{"input_tokens": 30, "output_tokens": 5},
		})
	})
	return fix
}

// TestChatReplaysThinkingSignatureBeforeToolUse pins the full
// tool-loop replay: the assistant turn echoes its own thinking block,
// with the exact captured signature, ahead of the text and tool_use
// parts. Without the echo the Messages API rejects the follow-up
// request.
func TestChatReplaysThinkingSignatureBeforeToolUse(t *testing.T) {
	var captured []map[string]any
	fix := captureThinkingToolLoopServer(t, &captured)
	client := exposeClient(t, fix)

	first, err := client.Chat(context.Background(), provider.Request{
		ReasoningEffort: provider.ReasoningEffortHigh,
		Messages:        []provider.Message{{Role: provider.RoleUser, Content: "list files"}},
	})
	if err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	wantBlocks := []provider.ReasoningBlock{{Content: "Let me think about this.", Signature: "sig-abc"}}
	if !reflect.DeepEqual(first.Message.ReasoningBlocks, wantBlocks) {
		t.Fatalf("decode ReasoningBlocks = %+v, want the signed thinking block", first.Message.ReasoningBlocks)
	}

	history := []provider.Message{
		{Role: provider.RoleUser, Content: "list files"},
		first.Message,
		{Role: provider.RoleTool, ToolCallID: "toolu_1", Content: `{"files":["notes.txt"]}`},
	}
	_, err = client.Chat(context.Background(), provider.Request{
		ReasoningEffort: provider.ReasoningEffortHigh,
		Messages:        history,
	})
	if err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("captured requests = %d, want 2", len(captured))
	}

	assistant := assistantWireTurn(t, captured[1])
	parts, ok := assistant["content"].([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("assistant content = %v, want thinking, text, tool_use", assistant["content"])
	}
	thinking, ok := parts[0].(map[string]any)
	if !ok || thinking["type"] != "thinking" {
		t.Fatalf("first assistant part = %v, want the thinking block first", parts[0])
	}
	if thinking["thinking"] != "Let me think about this." {
		t.Errorf("thinking text = %v, want exact replay", thinking["thinking"])
	}
	if thinking["signature"] != "sig-abc" {
		t.Errorf("signature = %v, want exact sig-abc", thinking["signature"])
	}
	if parts[1].(map[string]any)["type"] != "text" {
		t.Errorf("second assistant part = %v, want text after thinking", parts[1])
	}
	if parts[2].(map[string]any)["type"] != "tool_use" {
		t.Errorf("third assistant part = %v, want tool_use after thinking", parts[2])
	}
}

// captureThinkingOnlyServer serves one thinking-only turn carrying the
// given signature and captures every wire request.
func captureThinkingOnlyServer(t *testing.T, signature string, captured *map[string]any) *testClientFixture {
	t.Helper()
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, captured)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_t", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Deep thought.", "signature": signature},
			},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	})
	return fix
}

// TestChatReplayKeepsThinkingOnlyAssistantTurn pins the changed
// empty-turn rule: a thinking-only assistant turn survives the drop
// once it carries the replay carrier, and the same turn without a
// signature still drops. The signature is the gate.
func TestChatReplayKeepsThinkingOnlyAssistantTurn(t *testing.T) {
	cases := []struct {
		name          string
		signature     string
		wantAssistant bool
	}{
		{name: "with a signature the turn survives", signature: "sig-xyz", wantAssistant: true},
		{name: "without a signature the turn drops", signature: "", wantAssistant: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			fix := captureThinkingOnlyServer(t, tc.signature, &captured)
			client := exposeClient(t, fix)
			resp, err := client.Chat(context.Background(), provider.Request{
				ReasoningEffort: provider.ReasoningEffortHigh,
			})
			if err != nil {
				t.Fatalf("first Chat: %v", err)
			}
			if resp.Message.Content != "" {
				t.Fatalf("Content = %q, want a thinking-only response", resp.Message.Content)
			}
			if len(resp.Message.ReasoningBlocks) != 1 || resp.Message.ReasoningBlocks[0].Content != "Deep thought." {
				t.Fatalf("ReasoningBlocks = %+v, want the thinking block", resp.Message.ReasoningBlocks)
			}

			_, err = client.Chat(context.Background(), provider.Request{
				ReasoningEffort: provider.ReasoningEffortHigh,
				Messages: []provider.Message{
					{Role: provider.RoleUser, Content: "go"},
					resp.Message,
				},
			})
			if err != nil {
				t.Fatalf("second Chat: %v", err)
			}

			msgs, ok := captured["messages"].([]any)
			if !ok {
				t.Fatalf("wire request has no messages array: %v", captured)
			}
			var assistant map[string]any
			for _, m := range msgs {
				if mm, ok := m.(map[string]any); ok && mm["role"] == "assistant" {
					assistant = mm
				}
			}
			if !tc.wantAssistant {
				if assistant != nil {
					t.Fatalf("messages = %v, want the signature-less turn dropped", msgs)
				}
				return
			}
			if assistant == nil {
				t.Fatalf("messages = %v, want the thinking-only assistant turn kept", msgs)
			}
			parts, ok := assistant["content"].([]any)
			if !ok || len(parts) != 1 {
				t.Fatalf("assistant content = %v, want one thinking part", assistant["content"])
			}
			thinking, ok := parts[0].(map[string]any)
			if !ok || thinking["type"] != "thinking" ||
				thinking["thinking"] != "Deep thought." || thinking["signature"] != "sig-xyz" {
				t.Fatalf("thinking part = %v, want exact replay of the carrier", parts[0])
			}
		})
	}
}

// TestChatReplayPlainHistoryHasNoThinkingPart pins that a plain
// text-only response replayed as history carries no thinking part.
func TestChatReplayPlainHistoryHasNoThinkingPart(t *testing.T) {
	var captured map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_plain", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": "plain answer"}},
			"usage":   map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	})

	resp, err := fix.client.Chat(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if len(resp.Message.ReasoningBlocks) != 0 {
		t.Fatalf("ReasoningBlocks = %+v, want empty for a plain response", resp.Message.ReasoningBlocks)
	}

	_, err = fix.client.Chat(context.Background(), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hi"},
			resp.Message,
		},
	})
	if err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	assertNoThinkingPart(t, captured)
}

// TestChatReplaySkippedWhenReasoningDisabled pins the
// reasoning-enabled guard: history carries both carrier fields, but
// the request sets no effort and the client sets no DefaultEffort, so
// the wire carries no thinking part anywhere.
func TestChatReplaySkippedWhenReasoningDisabled(t *testing.T) {
	var captured map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_off", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})

	_, err := fix.client.Chat(context.Background(), provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "go"},
			{Role: provider.RoleAssistant, Content: "answered",
				ReasoningBlocks: []provider.ReasoningBlock{{Content: "Deep thought.", Signature: "sig-abc"}}},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, hasThinking := captured["thinking"]; hasThinking {
		t.Fatalf("thinking config = %v, want absent when no effort is set", captured["thinking"])
	}
	assertNoThinkingPart(t, captured)
}
