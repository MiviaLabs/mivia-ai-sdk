package usage_test

import (
	"context"
	"errors"
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
	if wrapped.Name() != "counting" {
		t.Fatalf("wrapped.Name() = %q, want the inner name kept", wrapped.Name())
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

// TestWrapCompleterConstructionRejects proves a blank sessionID, a
// nil Accumulator, and a nil Completer each fail construction with
// their sentinel, rather than panicking at turn time.
func TestWrapCompleterConstructionRejects(t *testing.T) {
	cases := []struct {
		name    string
		session string
		acc     *usage.Accumulator
		inner   provider.Completer
		want    error
	}{
		{name: "blank session", session: "  ", acc: usage.New(), inner: &countingCompleter{}, want: usage.ErrBlankSessionID},
		{name: "nil accumulator", session: "s", acc: nil, inner: &countingCompleter{}, want: usage.ErrNilAccumulator},
		{name: "nil completer", session: "s", acc: usage.New(), inner: nil, want: usage.ErrNilCompleter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := usage.WrapCompleter(tc.session, tc.acc, tc.inner)
			if !errors.Is(err, tc.want) {
				t.Fatalf("WrapCompleter error = %v, want %v", err, tc.want)
			}
		})
	}
}

// streamingCompleter answers one streamed turn of two chunks.
type streamingCompleter struct{}

// Name returns the stub's name.
func (streamingCompleter) Name() string { return "streaming" }

// Chat is unused by the stream test.
func (streamingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{}, nil
}

// ChatStream yields one content chunk and one done chunk carrying
// usage.
func (streamingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Delta: "stre"}
	ch <- provider.Chunk{Delta: "amed", Done: true,
		Usage: provider.Usage{PromptTokens: 5, CompletionTokens: 5, TotalTokens: 10}}
	close(ch)
	return ch, nil
}

// TestWrapCompleterStreamRecordsNothing proves the wrapper passes a
// streamed turn through unchanged and records no usage for it,
// matching the documented passthrough.
func TestWrapCompleterStreamRecordsNothing(t *testing.T) {
	acc := usage.New()
	wrapped, err := usage.WrapCompleter("session-s", acc, streamingCompleter{})
	if err != nil {
		t.Fatalf("WrapCompleter: %v", err)
	}
	resp, err := provider.RunTurn(context.Background(), wrapped, provider.Request{
		Stream:   true,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "go"}},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if resp.Message.Content != "streamed" {
		t.Fatalf("streamed content = %q, want the passthrough", resp.Message.Content)
	}
	if _, ok := acc.Total("session-s"); ok {
		t.Fatal("Total(session-s): want nothing recorded for a streamed turn")
	}
}

// errStreamingUnused backs ChatStream's unused stub.
var errStreamingUnused = &streamingError{}

// streamingError is ChatStream's unused stub error.
type streamingError struct{}

// Error reports the stub.
func (e *streamingError) Error() string { return "usage test: streaming unused" }
