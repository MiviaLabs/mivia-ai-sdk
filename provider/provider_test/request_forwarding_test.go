package provider_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// widenedRequestFields builds a Request with every field 1eda013 added
// set to a distinguishing non-zero value, so a forwarding test can
// prove RunTurn passes each one through to the Completer unmodified,
// rather than reconstructing a narrower Request that drops them.
func widenedRequestFields(stream bool) provider.Request {
	temperature := 0.0
	maxTokens := 256
	return provider.Request{
		Model:                 "widened-model",
		Messages:              []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Stream:                stream,
		Temperature:           &temperature,
		MaxTokens:             &maxTokens,
		ToolChoice:            provider.ToolChoiceAuto,
		Timeout:               30 * time.Second,
		SessionID:             "session-widened",
		DisableProviderReplay: true,
		ReasoningEffort:       provider.ReasoningEffortHigh,
		ReasoningDialect:      provider.ReasoningDialect("dialect-x"),
	}
}

// TestRunTurnForwardsWidenedRequestFieldsNonStream pins that RunTurn's
// non-streaming path forwards every 1eda013 Request field to
// Completer.Chat unmodified, including the pointer identity of
// Temperature (set to a non-nil pointer to zero, distinct from nil, per
// Request's doc comment). Without this test, a RunTurn rewritten to
// build a narrower Request before dispatch, dropping the new fields,
// would still pass every other test in this package.
func TestRunTurnForwardsWidenedRequestFieldsNonStream(t *testing.T) {
	f := &fakeCompleter{name: "fake"}
	req := widenedRequestFields(false)

	if _, err := provider.RunTurn(context.Background(), f, req); err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(f.lastRequest, req) {
		t.Fatalf("RunTurn() forwarded request = %+v, want the caller's %+v (unmodified)", f.lastRequest, req)
	}
	if f.lastRequest.Temperature != req.Temperature {
		t.Fatalf("RunTurn() forwarded Temperature pointer = %p, want the caller's own pointer %p", f.lastRequest.Temperature, req.Temperature)
	}
	if f.lastRequest.MaxTokens != req.MaxTokens {
		t.Fatalf("RunTurn() forwarded MaxTokens pointer = %p, want the caller's own pointer %p", f.lastRequest.MaxTokens, req.MaxTokens)
	}
}

// TestRunTurnForwardsWidenedRequestFieldsStream is the streamed
// counterpart to TestRunTurnForwardsWidenedRequestFieldsNonStream: it
// pins the same forwarding contract against Completer.ChatStream.
func TestRunTurnForwardsWidenedRequestFieldsStream(t *testing.T) {
	f := &fakeCompleter{name: "fake", streamChunks: []provider.Chunk{{Done: true}}}
	req := widenedRequestFields(true)

	if _, err := provider.RunTurn(context.Background(), f, req); err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(f.lastRequest, req) {
		t.Fatalf("RunTurn() forwarded request = %+v, want the caller's %+v (unmodified)", f.lastRequest, req)
	}
	if f.lastRequest.Temperature != req.Temperature {
		t.Fatalf("RunTurn() forwarded Temperature pointer = %p, want the caller's own pointer %p", f.lastRequest.Temperature, req.Temperature)
	}
	if f.lastRequest.MaxTokens != req.MaxTokens {
		t.Fatalf("RunTurn() forwarded MaxTokens pointer = %p, want the caller's own pointer %p", f.lastRequest.MaxTokens, req.MaxTokens)
	}
}
