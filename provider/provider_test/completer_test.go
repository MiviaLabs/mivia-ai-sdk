package provider_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestFakeChatReturnsFixedResponse(t *testing.T) {
	want := provider.Response{Model: "test-model", Message: provider.Message{Role: provider.RoleAssistant, Content: "hi"}}
	f := &fakeCompleter{name: "fake", chatResp: want}
	req := provider.Request{Model: "test-model", Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}}

	got, err := f.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Chat() = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(f.lastRequest, req) {
		t.Fatalf("fake recorded request = %+v, want %+v", f.lastRequest, req)
	}
}

func TestFakeChatFails(t *testing.T) {
	f := &fakeCompleter{name: "fake", chatErr: errFakeChat}

	got, err := f.Chat(context.Background(), provider.Request{})
	if !errors.Is(err, errFakeChat) {
		t.Fatalf("Chat() error = %v, want errors.Is errFakeChat", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("Chat() response = %+v, want zero value", got)
	}
}

func TestFakeChatStreamDrainOrder(t *testing.T) {
	wantUsage := provider.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}
	chunks := []provider.Chunk{
		{Delta: "a"},
		{Delta: "b"},
		{Delta: "c", Done: true, Usage: wantUsage, FinishReason: "stop"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}

	ch, err := f.ChatStream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatalf("ChatStream() error = %v, want nil", err)
	}
	var drained []provider.Chunk
	for c := range ch {
		drained = append(drained, c)
	}
	if len(drained) != 3 {
		t.Fatalf("drained %d chunks, want 3", len(drained))
	}
	for i, c := range chunks {
		if drained[i].Delta != c.Delta || drained[i].Done != c.Done {
			t.Fatalf("chunk %d = %+v, want %+v", i, drained[i], c)
		}
	}
	if drained[2].Usage != wantUsage {
		t.Fatalf("final Usage = %+v, want %+v", drained[2].Usage, wantUsage)
	}
}

func TestFakeChatStreamFails(t *testing.T) {
	f := &fakeCompleter{name: "fake", streamErr: errFakeStream}

	ch, err := f.ChatStream(context.Background(), provider.Request{})
	if !errors.Is(err, errFakeStream) {
		t.Fatalf("ChatStream() error = %v, want errors.Is errFakeStream", err)
	}
	if ch != nil {
		t.Fatalf("ChatStream() channel = %v, want nil", ch)
	}
}

// TestRunTurnStreamedMessageCarriesToolCalls drives RunTurn's streamed
// aggregation through buildResponse and proves Message.ToolCalls stays
// in sync with Response.ToolCalls. A hand-built Response would set both
// fields itself and prove nothing, so the case drains a real stream.
func TestRunTurnStreamedMessageCarriesToolCalls(t *testing.T) {
	chunks := []provider.Chunk{
		{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0", Name: "search", Arguments: []byte(`{"q":`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 0, Arguments: []byte(`"cats"}`)}},
		{Done: true, FinishReason: "tool_calls"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true, Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}

	resp, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(resp.Message.ToolCalls, resp.ToolCalls) {
		t.Fatalf("resp.Message.ToolCalls = %+v, want equal to resp.ToolCalls %+v", resp.Message.ToolCalls, resp.ToolCalls)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("resp.Message.ToolCalls len = %d, want 1", len(resp.Message.ToolCalls))
	}
	if err := resp.Message.Validate(); err != nil {
		t.Fatalf("resp.Message.Validate() = %v, want nil", err)
	}
}

func TestRunTurnValidatesToolChoiceBeforeMessages(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	req := provider.Request{
		ToolChoice: provider.ToolChoice("bogus"),
		Messages:   []provider.Message{{Role: provider.Role("also-bogus")}},
	}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrToolChoiceInvalid) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrToolChoiceInvalid", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
	if f.chatCalled || f.streamCalled {
		t.Fatal("RunTurn() dispatched to the Completer despite an invalid ToolChoice")
	}
}

func TestRunTurnValidToolChoiceStillValidatesMessages(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	req := provider.Request{
		ToolChoice: provider.ToolChoiceAuto,
		Messages:   []provider.Message{{Role: provider.RoleUser, ToolCallID: "call-1"}},
	}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrToolCallIDUnexpected) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrToolCallIDUnexpected", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
}

func TestRunTurnStreamedReasoningDeltaConcatenates(t *testing.T) {
	chunks := []provider.Chunk{
		{Delta: "hel", ReasoningDelta: "first "},
		{Delta: "lo", ReasoningDelta: "second"},
		{Done: true, FinishReason: "stop"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true, Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}

	resp, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if resp.Message.Content != "hello" {
		t.Fatalf("resp.Message.Content = %q, want %q", resp.Message.Content, "hello")
	}
	if resp.Message.ReasoningContent != "first second" {
		t.Fatalf("resp.Message.ReasoningContent = %q, want %q", resp.Message.ReasoningContent, "first second")
	}
}

func TestRunTurnStreamedTerminalChunkCarriesCacheAndWebSearch(t *testing.T) {
	wantCacheUsage := provider.CacheUsage{Reported: true, Style: provider.CacheStyleImplicit, InputTokens: 10, CachedInputTokens: 4}
	wantWebSearch := []provider.WebSearchResult{{Title: "t", Link: "l"}}
	chunks := []provider.Chunk{
		{Delta: "a", CacheUsage: provider.CacheUsage{Reported: true, Style: provider.CacheStyleExplicit, InputTokens: 99}, WebSearch: []provider.WebSearchResult{{Title: "discarded"}}},
		{Delta: "b", Done: true, CacheUsage: wantCacheUsage, WebSearch: wantWebSearch},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true, Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}

	resp, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(resp.CacheUsage, wantCacheUsage) {
		t.Fatalf("resp.CacheUsage = %+v, want %+v", resp.CacheUsage, wantCacheUsage)
	}
	if !reflect.DeepEqual(resp.WebSearch, wantWebSearch) {
		t.Fatalf("resp.WebSearch = %+v, want %+v", resp.WebSearch, wantWebSearch)
	}
}

func TestOptionalCapabilityInterfaces(t *testing.T) {
	capable := &capableFake{fakeCompleter: fakeCompleter{name: "capable"}, contextWindow: 128000, reasoningEffort: "high"}
	var c provider.Completer = capable

	ca, ok := c.(provider.ContextAccountant)
	if !ok {
		t.Fatal("capableFake does not satisfy ContextAccountant")
	}
	if ca.ContextWindow() != 128000 {
		t.Fatalf("ContextWindow() = %d, want 128000", ca.ContextWindow())
	}
	rp, ok := c.(provider.ReasoningPolicy)
	if !ok {
		t.Fatal("capableFake does not satisfy ReasoningPolicy")
	}
	if rp.ReasoningEffort() != "high" {
		t.Fatalf("ReasoningEffort() = %q, want %q", rp.ReasoningEffort(), "high")
	}

	plain := &fakeCompleter{name: "plain"}
	var pc provider.Completer = plain
	if _, ok := pc.(provider.ContextAccountant); ok {
		t.Fatal("plain fakeCompleter unexpectedly satisfies ContextAccountant")
	}
	if _, ok := pc.(provider.ReasoningPolicy); ok {
		t.Fatal("plain fakeCompleter unexpectedly satisfies ReasoningPolicy")
	}
}
