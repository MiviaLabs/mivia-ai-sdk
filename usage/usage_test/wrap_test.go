package usage_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// countingCompleter answers one canned turn per call and reports its
// usage. It stands in for a real model client.
type countingCompleter struct {
	turns []provider.Response
	calls int
}

// Name returns the stub's name.
func (c *countingCompleter) Name() string { return "counting" }

// Chat returns the next canned turn.
func (c *countingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	resp := c.turns[c.calls%len(c.turns)]
	c.calls++
	return resp, nil
}

// ChatStream is unused by these tests.
func (c *countingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errStreamingUnused
}

// TestWrapCompleterRecordsEachTurn proves WrapCompleter records every
// completed turn's usage under the wrapped session, keeps the
// response unchanged, and isolates one session's total from another.
func TestWrapCompleterRecordsEachTurn(t *testing.T) {
	acc := usage.New()
	inner := &countingCompleter{turns: []provider.Response{
		{Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
			Message: provider.Message{Role: provider.RoleAssistant, Content: "first"}},
		{Usage: provider.Usage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24},
			Message: provider.Message{Role: provider.RoleAssistant, Content: "second"}},
	}}
	wrapped, err := usage.WrapCompleter("session-1", acc, inner)
	if err != nil {
		t.Fatalf("WrapCompleter: %v", err)
	}

	for i := 0; i < 2; i++ {
		resp, rerr := provider.RunTurn(context.Background(), wrapped, provider.Request{
			Messages: []provider.Message{{Role: provider.RoleUser, Content: "go"}},
		})
		if rerr != nil {
			t.Fatalf("RunTurn %d: %v", i, rerr)
		}
		if want := inner.turns[i].Message.Content; resp.Message.Content != want {
			t.Fatalf("turn %d content = %q, want %q unchanged", i, resp.Message.Content, want)
		}
	}
	total, ok := acc.Total("session-1")
	if !ok {
		t.Fatal("Total(session-1): want recorded, got none")
	}
	want := provider.Usage{PromptTokens: 30, CompletionTokens: 6, TotalTokens: 36}
	if total != want {
		t.Fatalf("Total(session-1) = %+v, want %+v", total, want)
	}
	if _, ok := acc.Total("session-2"); ok {
		t.Fatal("Total(session-2): want isolated, got a total")
	}
}

// TestWrapCompleterBlankSessionFails proves a blank session id fails
// construction rather than silently dropping counts.
func TestWrapCompleterBlankSessionFails(t *testing.T) {
	_, err := usage.WrapCompleter("  ", usage.New(), &countingCompleter{})
	if err == nil {
		t.Fatal("WrapCompleter with a blank session: want an error")
	}
}

// errStreamingUnused backs ChatStream's unused stub.
var errStreamingUnused = &streamingError{}

// streamingError is ChatStream's unused stub error.
type streamingError struct{}

// Error reports the stub.
func (e *streamingError) Error() string { return "usage test: streaming unused" }
