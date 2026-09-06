package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/provider/anthropic"
)

// TestChatReplaysBothThinkingBlocksInOrder pins the slice carrier
// decode and the replay order: two thinking blocks with two different
// signatures land in the carrier in arrival order, and the next
// request replays both, in order, ahead of the text part.
func TestChatReplaysBothThinkingBlocksInOrder(t *testing.T) {
	var captured map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_2sig", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "First thought.", "signature": "sig-first"},
				{"type": "thinking", "thinking": "Second thought.", "signature": "sig-second"},
				{"type": "text", "text": "Answer."},
			},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	})

	client := exposeClient(t, fix)
	resp, err := client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	want := []provider.ReasoningBlock{
		{Content: "First thought.", Signature: "sig-first"},
		{Content: "Second thought.", Signature: "sig-second"},
	}
	if !reflect.DeepEqual(resp.Message.ReasoningBlocks, want) {
		t.Fatalf("ReasoningBlocks = %+v, want both blocks in arrival order", resp.Message.ReasoningBlocks)
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
	assistant := assistantWireTurn(t, captured)
	parts, ok := assistant["content"].([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("assistant content = %v, want thinking, thinking, text", assistant["content"])
	}
	first, _ := parts[0].(map[string]any)
	second, _ := parts[1].(map[string]any)
	if first["type"] != "thinking" || first["thinking"] != "First thought." || first["signature"] != "sig-first" {
		t.Errorf("first replayed part = %v, want exact sig-first block", first)
	}
	if second["type"] != "thinking" || second["thinking"] != "Second thought." || second["signature"] != "sig-second" {
		t.Errorf("second replayed part = %v, want exact sig-second block", second)
	}
	if parts[2].(map[string]any)["type"] != "text" {
		t.Errorf("third replayed part = %v, want text after both thinking blocks", parts[2])
	}
}

// TestChatReplaysEmptyTextThinkingBlock pins that a thinking block
// whose text is empty still carries its signature and still replays;
// the Messages API requires the echo even then. The thinking-only
// turn must survive the empty-turn rule.
func TestChatReplaysEmptyTextThinkingBlock(t *testing.T) {
	var captured map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_empty", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "", "signature": "sig-empty"},
			},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	})

	client := exposeClient(t, fix)
	resp, err := client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	want := []provider.ReasoningBlock{{Content: "", Signature: "sig-empty"}}
	if !reflect.DeepEqual(resp.Message.ReasoningBlocks, want) {
		t.Fatalf("ReasoningBlocks = %+v, want the empty-text signed block", resp.Message.ReasoningBlocks)
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
	assistant := assistantWireTurn(t, captured)
	parts, ok := assistant["content"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("assistant content = %v, want one thinking part", assistant["content"])
	}
	thinking, _ := parts[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["signature"] != "sig-empty" {
		t.Errorf("replayed part = %v, want the empty-text block replayed with sig-empty", thinking)
	}
}

// TestChatReplaysRedactedThinkingRoundTrip pins the redacted_thinking
// round-trip: the adapter decodes a redacted block with its opaque
// data payload into the carrier, and the next request replays it in
// its wire form.
func TestChatReplaysRedactedThinkingRoundTrip(t *testing.T) {
	const wantData = "ErBCSkYPGkvblKQZEhJbCA=="
	var captured map[string]any
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_redacted", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "redacted_thinking", "data": wantData},
				{"type": "text", "text": "Answer."},
			},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	})

	client := exposeClient(t, fix)
	resp, err := client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	want := []provider.ReasoningBlock{{Redacted: true, Data: wantData}}
	if !reflect.DeepEqual(resp.Message.ReasoningBlocks, want) {
		t.Fatalf("ReasoningBlocks = %+v, want the redacted block with its data", resp.Message.ReasoningBlocks)
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
	assistant := assistantWireTurn(t, captured)
	parts, ok := assistant["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("assistant content = %v, want redacted_thinking then text", assistant["content"])
	}
	redacted, _ := parts[0].(map[string]any)
	if redacted["type"] != "redacted_thinking" || redacted["data"] != wantData {
		t.Errorf("replayed part = %v, want the redacted block byte-for-byte", redacted)
	}
}

// TestOnReasoningFiresBesideExposeReasoning pins the both-fire
// decision: OnReasoning receives every readable thinking block in
// redacted form even when ExposeReasoning is on, and the replay
// carrier stays populated either way.
func TestOnReasoningFiresBesideExposeReasoning(t *testing.T) {
	var fired []provider.ReasoningBlock
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": "msg_both", "type": "message", "role": "assistant",
			"model": "claude-opus-5", "stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "thinking", "thinking": "First thought.", "signature": "sig-first"},
				{"type": "thinking", "thinking": "Second thought.", "signature": "sig-second"},
			},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 5},
		})
	})

	client, err := anthropic.New(anthropic.Options{
		APIKey:          "test-key",
		BaseURL:         fix.baseURL,
		HTTPClient:      fix.httpCli,
		ExposeReasoning: true,
		OnReasoning:     func(rb provider.ReasoningBlock) { fired = append(fired, rb) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := client.Chat(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(fired) != 2 {
		t.Fatalf("OnReasoning fired %d times, want 2", len(fired))
	}
	for i, block := range fired {
		if !block.Redacted || block.Content != "" || block.Signature != "" {
			t.Errorf("OnReasoning block %d = %+v, want redacted with empty content", i, block)
		}
	}
	if len(resp.Message.ReasoningBlocks) != 2 {
		t.Fatalf("ReasoningBlocks = %+v, want the replay carrier populated", resp.Message.ReasoningBlocks)
	}
}

// streamThinkingFixture serves one streamed turn: a thinking block
// with a signature_delta, then a text block. It captures every wire
// request.
func streamThinkingFixture(t *testing.T, captured *map[string]any) *testClientFixture {
	t.Helper()
	var attempts int32
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, captured)
		if atomic.AddInt32(&attempts, 1) > 1 {
			writeJSON(w, http.StatusOK, map[string]any{
				"id": "msg_s2", "type": "message", "role": "assistant",
				"model": "claude-opus-5", "stop_reason": "end_turn",
				"content": []map[string]any{{"type": "text", "text": "done"}},
				"usage":   map[string]any{"input_tokens": 9, "output_tokens": 2},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			fmt.Fprintf(w, "event: message_start\ndata: {\"message\":{\"id\":\"msg_s\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5\",\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n")
			fmt.Fprintf(w, "event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n")
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Streamed thought.\"}}\n\n")
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-stream\"}}\n\n")
			fmt.Fprintf(w, "event: content_block_stop\ndata: {\"index\":0}\n\n")
			fmt.Fprintf(w, "event: content_block_start\ndata: {\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Answer.\"}}\n\n")
			fmt.Fprintf(w, "event: content_block_stop\ndata: {\"index\":1}\n\n")
			fmt.Fprintf(w, "event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":9}}\n\n")
			fmt.Fprintf(w, "event: message_stop\ndata: {}\n\n")
			flusher.Flush()
		}
	})
	return fix
}

// TestChatStreamCapturesSignatureAndReplays pins the streamed path:
// the signature_delta event lands in the carrier, RunTurn's streamed
// aggregation forwards the complete block, and the next Chat call
// replays it like a non-streamed block.
func TestChatStreamCapturesSignatureAndReplays(t *testing.T) {
	var captured map[string]any
	fix := streamThinkingFixture(t, &captured)
	client := exposeClient(t, fix)

	resp, err := client.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	got, err := provider.RunTurn(context.Background(), chanCompleter{ch: resp},
		provider.Request{Stream: true})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	want := []provider.ReasoningBlock{{Content: "Streamed thought.", Signature: "sig-stream"}}
	if !reflect.DeepEqual(got.Message.ReasoningBlocks, want) {
		t.Fatalf("ReasoningBlocks = %+v, want the streamed signed block", got.Message.ReasoningBlocks)
	}

	_, err = client.Chat(context.Background(), provider.Request{
		ReasoningEffort: provider.ReasoningEffortHigh,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "go"},
			got.Message,
		},
	})
	if err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	assistant := assistantWireTurn(t, captured)
	parts, ok := assistant["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("assistant content = %v, want thinking then text", assistant["content"])
	}
	thinking, _ := parts[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["thinking"] != "Streamed thought." || thinking["signature"] != "sig-stream" {
		t.Errorf("replayed part = %v, want the streamed block replayed exactly", thinking)
	}
}

// TestChatReplaySuppressedWhenProviderReplayDisabled pins the
// DisableProviderReplay contract: the request flag suppresses every
// reasoning part on the wire, even when history carries signed
// blocks.
func TestChatReplaySuppressedWhenProviderReplayDisabled(t *testing.T) {
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

	client := exposeClient(t, fix)
	_, err := client.Chat(context.Background(), provider.Request{
		ReasoningEffort:       provider.ReasoningEffortHigh,
		DisableProviderReplay: true,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "go"},
			{Role: provider.RoleAssistant, Content: "answered",
				ReasoningBlocks: []provider.ReasoningBlock{
					{Content: "Deep thought.", Signature: "sig-abc"},
					{Redacted: true, Data: "ErBCSkYPGkvblKQZEhJbCA=="},
				}},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	assertNoThinkingPart(t, captured)
}

// chanCompleter adapts one ChatStream channel to provider.Completer
// so a streamed turn runs through RunTurn's aggregation.
type chanCompleter struct {
	ch <-chan provider.Chunk
}

func (c chanCompleter) Name() string { return "chan" }

func (c chanCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{}, errors.New("chanCompleter: Chat is not supported")
}

func (c chanCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return c.ch, nil
}
