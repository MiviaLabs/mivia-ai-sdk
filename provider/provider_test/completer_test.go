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
