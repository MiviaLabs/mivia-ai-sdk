package provider_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

func TestRunTurnNonStreamCallsChat(t *testing.T) {
	want := provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "hi"}}
	f := &fakeCompleter{name: "fake", chatResp: want}
	req := provider.Request{Stream: false, Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}}

	got, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if !f.chatCalled {
		t.Fatal("RunTurn() with Stream=false did not call Chat")
	}
	if f.streamCalled {
		t.Fatal("RunTurn() with Stream=false called ChatStream")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RunTurn() = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(f.lastRequest, req) {
		t.Fatalf("RunTurn() forwarded request = %+v, want the caller's %+v (unmodified)", f.lastRequest, req)
	}
}

func TestRunTurnStreamAggregatesPlainDeltas(t *testing.T) {
	chunks := []provider.Chunk{
		{Delta: "Hel"},
		{Delta: "lo, "},
		{Delta: "world"},
		{Done: true, FinishReason: "stop"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true, Model: "stream-model", Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}

	got, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if !f.streamCalled {
		t.Fatal("RunTurn() with Stream=true did not call ChatStream")
	}
	if !reflect.DeepEqual(f.lastRequest, req) {
		t.Fatalf("RunTurn() forwarded request = %+v, want the caller's %+v (unmodified)", f.lastRequest, req)
	}
	if got.Message.Content != "Hello, world" {
		t.Fatalf("Message.Content = %q, want %q", got.Message.Content, "Hello, world")
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("ToolCalls len = %d, want 0", len(got.ToolCalls))
	}
	if got.Message.Role != provider.RoleAssistant {
		t.Fatalf("Message.Role = %v, want RoleAssistant", got.Message.Role)
	}
}

func TestRunTurnStreamRejectsInvalidChunk(t *testing.T) {
	chunks := []provider.Chunk{
		{Err: errors.New("mid-stream failure"), Done: true},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrChunkErrDoneConflict) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrChunkErrDoneConflict", err)
	}
	if err != provider.ErrChunkErrDoneConflict {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrChunkErrDoneConflict (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
}

func TestRunTurnStreamMergesConcurrentToolCalls(t *testing.T) {
	chunks := []provider.Chunk{
		{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0", Name: "search", Arguments: []byte(`{"q":`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 1, ID: "call-1", Name: "lookup", Arguments: []byte(`{"id":`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 0, Arguments: []byte(`"cats"}`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 1, Arguments: []byte(`42}`)}},
		{Done: true, FinishReason: "tool_calls"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if len(got.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(got.ToolCalls))
	}
	first, second := got.ToolCalls[0], got.ToolCalls[1]
	if first.Index != 0 || first.ID != "call-0" || first.Name != "search" || string(first.Arguments) != `{"q":"cats"}` {
		t.Fatalf("ToolCalls[0] = %+v", first)
	}
	if second.Index != 1 || second.ID != "call-1" || second.Name != "lookup" || string(second.Arguments) != `{"id":42}` {
		t.Fatalf("ToolCalls[1] = %+v", second)
	}
	if got.Message.Role != provider.RoleAssistant {
		t.Fatalf("Message.Role = %v, want RoleAssistant", got.Message.Role)
	}
	if !reflect.DeepEqual(got.Message.ToolCalls, got.ToolCalls) {
		t.Fatalf("Message.ToolCalls = %+v, want equal to ToolCalls %+v", got.Message.ToolCalls, got.ToolCalls)
	}
}

func TestRunTurnStreamMergesOutOfOrderToolCallIndexes(t *testing.T) {
	chunks := []provider.Chunk{
		{ToolCallDelta: &provider.ToolCall{Index: 1, ID: "call-1", Name: "lookup", Arguments: []byte(`{"id":`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0", Name: "search", Arguments: []byte(`{"q":`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 1, Arguments: []byte(`42}`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 0, Arguments: []byte(`"cats"}`)}},
		{Done: true, FinishReason: "tool_calls"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if len(got.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(got.ToolCalls))
	}
	first, second := got.ToolCalls[0], got.ToolCalls[1]
	if first.Index != 0 || first.ID != "call-0" || first.Name != "search" || string(first.Arguments) != `{"q":"cats"}` {
		t.Fatalf("ToolCalls[0] = %+v, want Index 0 (call-0)", first)
	}
	if second.Index != 1 || second.ID != "call-1" || second.Name != "lookup" || string(second.Arguments) != `{"id":42}` {
		t.Fatalf("ToolCalls[1] = %+v, want Index 1 (call-1)", second)
	}
}

func TestRunTurnStreamTerminalChunkContentIsAggregated(t *testing.T) {
	chunks := []provider.Chunk{
		{Delta: "Hel"},
		{
			Delta:         "lo",
			ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0", Name: "search", Arguments: []byte(`{"q":"cats"}`)},
			Done:          true,
			FinishReason:  "tool_calls",
		},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if got.Message.Content != "Hello" {
		t.Fatalf("Message.Content = %q, want %q (terminal chunk's Delta must be included)", got.Message.Content, "Hello")
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1 (terminal chunk's ToolCallDelta must be included)", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "call-0" || tc.Name != "search" || string(tc.Arguments) != `{"q":"cats"}` {
		t.Fatalf("ToolCalls[0] = %+v, want the terminal chunk's fragment", tc)
	}
}

func TestRunTurnStreamClosedEarlyReturnsZeroResponse(t *testing.T) {
	chunks := []provider.Chunk{
		{Delta: "partial "},
		{Delta: "more"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrStreamClosedEarly) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrStreamClosedEarly", err)
	}
	if err != provider.ErrStreamClosedEarly {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrStreamClosedEarly (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
}

func TestRunTurnStreamMidStreamFailureDiscardsPartial(t *testing.T) {
	failure := errors.New("connection reset")
	chunks := []provider.Chunk{
		{Delta: "partial "},
		{Delta: "output"},
		{Err: failure},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, failure) {
		t.Fatalf("RunTurn() error = %v, want errors.Is failure", err)
	}
	if err != failure {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel failure (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
}

func TestRunTurnPropagatesChatError(t *testing.T) {
	f := &fakeCompleter{name: "fake", chatErr: errFakeChat}
	req := provider.Request{Stream: false}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, errFakeChat) {
		t.Fatalf("RunTurn() error = %v, want errors.Is errFakeChat", err)
	}
	if err != errFakeChat {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel errFakeChat (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
}

func TestRunTurnPropagatesChatStreamBeforeFirstChunkError(t *testing.T) {
	f := &fakeCompleter{name: "fake", streamErr: errFakeStream}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, errFakeStream) {
		t.Fatalf("RunTurn() error = %v, want errors.Is errFakeStream", err)
	}
	if err != errFakeStream {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel errFakeStream (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
}

func TestRunTurnValidatesMessagesBeforeDispatch(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	req := provider.Request{
		Stream:   true,
		Messages: []provider.Message{{Role: provider.RoleTool, ToolCallID: ""}},
	}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrToolCallIDRequired", err)
	}
	if err != provider.ErrToolCallIDRequired {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrToolCallIDRequired (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
	if f.chatCalled || f.streamCalled {
		t.Fatal("RunTurn() dispatched to the Completer despite an invalid message")
	}
}

func TestRunTurnValidatesMessagesBeforeDispatchNonStreaming(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	req := provider.Request{
		Stream:   false,
		Messages: []provider.Message{{Role: provider.RoleTool, ToolCallID: ""}},
	}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrToolCallIDRequired", err)
	}
	if err != provider.ErrToolCallIDRequired {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrToolCallIDRequired (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
	if f.chatCalled || f.streamCalled {
		t.Fatal("RunTurn() dispatched to the Completer despite an invalid message on the non-streaming path")
	}
}

func TestRunTurnValidatesAllMessagesInOrder(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	validThenInvalid := provider.Request{
		Stream: false,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
			{Role: provider.RoleTool, ToolCallID: ""},
		},
	}
	got, err := provider.RunTurn(context.Background(), f, validThenInvalid)
	if !errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrToolCallIDRequired", err)
	}
	if err != provider.ErrToolCallIDRequired {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrToolCallIDRequired (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
	if f.chatCalled || f.streamCalled {
		t.Fatal("RunTurn() dispatched to the Completer despite an invalid trailing message")
	}

	g := &fakeCompleter{name: "fake"}
	invalidThenValid := provider.Request{
		Stream: false,
		Messages: []provider.Message{
			{Role: provider.RoleTool, ToolCallID: ""},
			{Role: provider.RoleUser, Content: "hello"},
		},
	}
	got, err = provider.RunTurn(context.Background(), g, invalidThenValid)
	if !errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrToolCallIDRequired", err)
	}
	if err != provider.ErrToolCallIDRequired {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrToolCallIDRequired (identity)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
	if g.chatCalled || g.streamCalled {
		t.Fatal("RunTurn() dispatched to the Completer despite a leading invalid message")
	}
}

func TestRunTurnValidatesFirstInvalidEntryAmongDifferentSentinels(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	req := provider.Request{
		Stream: false,
		Messages: []provider.Message{
			{Role: provider.Role("unknown")},
			{Role: provider.RoleTool, ToolCallID: ""},
		},
	}

	got, err := provider.RunTurn(context.Background(), f, req)
	if !errors.Is(err, provider.ErrUnknownRole) {
		t.Fatalf("RunTurn() error = %v, want errors.Is ErrUnknownRole (the first invalid entry)", err)
	}
	if err != provider.ErrUnknownRole {
		t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel ErrUnknownRole (identity)", err)
	}
	if errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("RunTurn() error = %v, unexpectedly errors.Is ErrToolCallIDRequired (the second, later, entry)", err)
	}
	if !reflect.DeepEqual(got, provider.Response{}) {
		t.Fatalf("RunTurn() response = %+v, want zero value", got)
	}
	if f.chatCalled || f.streamCalled {
		t.Fatal("RunTurn() dispatched to the Completer despite an invalid leading message")
	}
}

// TestRunTurnValidatesToolCallsPairingBeforeDispatch pins RunTurn's
// dispatch guard against the ErrToolCallIDUnexpected and
// ErrToolCallsUnexpected sentinels, the two Message.Validate branches
// no other RunTurn-level test reaches; the guard's coverage of
// ErrToolCallIDRequired and ErrUnknownRole lives in the tests above.
func TestRunTurnValidatesToolCallsPairingBeforeDispatch(t *testing.T) {
	cases := []struct {
		name    string
		msg     provider.Message
		wantErr error
	}{
		{
			name:    "tool call id on a non-tool role",
			msg:     provider.Message{Role: provider.RoleUser, ToolCallID: "call-1"},
			wantErr: provider.ErrToolCallIDUnexpected,
		},
		{
			name: "tool calls on a non-assistant role",
			msg: provider.Message{
				Role:      provider.RoleUser,
				ToolCalls: []provider.ToolCall{{Index: 0, ID: "call-1", Name: "search"}},
			},
			wantErr: provider.ErrToolCallsUnexpected,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeCompleter{name: "fake"}
			req := provider.Request{Stream: false, Messages: []provider.Message{c.msg}}

			got, err := provider.RunTurn(context.Background(), f, req)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("RunTurn() error = %v, want errors.Is %v", err, c.wantErr)
			}
			if err != c.wantErr {
				t.Fatalf("RunTurn() error = %v, want the unwrapped sentinel %v (identity)", err, c.wantErr)
			}
			if !reflect.DeepEqual(got, provider.Response{}) {
				t.Fatalf("RunTurn() response = %+v, want zero value", got)
			}
			if f.chatCalled || f.streamCalled {
				t.Fatal("RunTurn() dispatched to the Completer despite an invalid message")
			}
		})
	}
}

func TestRunTurnStreamMergeKeepsFirstNonEmptyIDAndName(t *testing.T) {
	chunks := []provider.Chunk{
		{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0", Name: "search", Arguments: []byte(`{"q":`)}},
		{ToolCallDelta: &provider.ToolCall{Index: 0, ID: "call-0-duplicate", Name: "search-duplicate", Arguments: []byte(`"cats"}`)}},
		{Done: true, FinishReason: "tool_calls"},
	}
	f := &fakeCompleter{name: "fake", streamChunks: chunks}
	req := provider.Request{Stream: true}

	got, err := provider.RunTurn(context.Background(), f, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "call-0" {
		t.Fatalf("ToolCalls[0].ID = %q, want first-seen %q", tc.ID, "call-0")
	}
	if tc.Name != "search" {
		t.Fatalf("ToolCalls[0].Name = %q, want first-seen %q", tc.Name, "search")
	}
	if string(tc.Arguments) != `{"q":"cats"}` {
		t.Fatalf("ToolCalls[0].Arguments = %q, want %q", tc.Arguments, `{"q":"cats"}`)
	}
}

func TestRunTurnRespectsContextCancellation(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f := &fakeCompleter{
			name:         "fake",
			streamChunks: []provider.Chunk{{Delta: "first"}},
			neverClose:   true,
		}
		req := provider.Request{Stream: true}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		got, err := provider.RunTurn(ctx, f, req)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("RunTurn() error = %v, want errors.Is context.DeadlineExceeded", err)
		}
		if wantErr := ctx.Err(); err != wantErr {
			t.Errorf("RunTurn() error = %v, want the unwrapped ctx.Err() %v (identity)", err, wantErr)
		}
		if !reflect.DeepEqual(got, provider.Response{}) {
			t.Errorf("RunTurn() response = %+v, want zero value", got)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunTurn() did not return promptly on context cancellation")
	}
}
