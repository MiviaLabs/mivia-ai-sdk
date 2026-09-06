package provider_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// signatureEchoFake records the request and echoes the assistant
// carrier fields back onto its response message.
type signatureEchoFake struct {
	fakeCompleter
}

func (f *signatureEchoFake) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	f.chatCalled = true
	f.lastRequest = req
	resp := f.chatResp
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleAssistant && msg.ReasoningSignature != "" {
			resp.Message.ReasoningContent = msg.ReasoningContent
			resp.Message.ReasoningSignature = msg.ReasoningSignature
			break
		}
	}
	return resp, nil
}

// TestMessageReasoningSignatureRoundTrip pins that
// ReasoningSignature round-trips through RunTurn unchanged: provider
// never validates, strips, or interprets the field.
func TestMessageReasoningSignatureRoundTrip(t *testing.T) {
	// A completer that never sets the field behaves as before: the
	// response keeps the zero value and nothing else changes.
	plain := &fakeCompleter{
		name: "fake",
		chatResp: provider.Response{
			Message: provider.Message{Role: provider.RoleAssistant, Content: "hi"},
		},
	}
	req := provider.Request{
		Stream:   false,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	}
	got, err := provider.RunTurn(context.Background(), plain, req)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if got.Message.ReasoningSignature != "" {
		t.Fatalf("ReasoningSignature = %q, want empty when the completer never sets it", got.Message.ReasoningSignature)
	}
	if !reflect.DeepEqual(got, plain.chatResp) {
		t.Fatalf("RunTurn() = %+v, want %+v", got, plain.chatResp)
	}

	// A completer that copies the carrier fields from the request gets
	// them back unchanged: RunTurn forwards the field and returns the
	// response untouched.
	history := provider.Request{
		Stream: false,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "go"},
			{Role: provider.RoleAssistant,
				ReasoningContent:   "Deep thought.",
				ReasoningSignature: "sig-123"},
		},
	}
	echo := &signatureEchoFake{
		fakeCompleter: fakeCompleter{
			name: "fake",
			chatResp: provider.Response{
				Message: provider.Message{Role: provider.RoleAssistant, Content: "echo"},
			},
		},
	}
	got, err = provider.RunTurn(context.Background(), echo, history)
	if err != nil {
		t.Fatalf("RunTurn() error = %v, want nil", err)
	}
	if got.Message.ReasoningContent != "Deep thought." || got.Message.ReasoningSignature != "sig-123" {
		t.Fatalf("response carriers = (%q, %q), want the echoed values unchanged",
			got.Message.ReasoningContent, got.Message.ReasoningSignature)
	}
	forwarded := echo.lastRequest.Messages[1]
	if forwarded.ReasoningContent != "Deep thought." || forwarded.ReasoningSignature != "sig-123" {
		t.Fatalf("forwarded carriers = (%q, %q), want the caller's values unchanged",
			forwarded.ReasoningContent, forwarded.ReasoningSignature)
	}

	// Validate never reads the field: it passes set on RoleAssistant
	// and zero-valued on all four roles.
	withField := provider.Message{
		Role:               provider.RoleAssistant,
		ReasoningContent:   "Deep thought.",
		ReasoningSignature: "sig-123",
	}
	if err := withField.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil with ReasoningSignature set on RoleAssistant", err)
	}
	for _, role := range []provider.Role{provider.RoleSystem, provider.RoleUser, provider.RoleAssistant, provider.RoleTool} {
		msg := provider.Message{Role: role}
		if role == provider.RoleTool {
			msg.ToolCallID = "call-1"
		}
		if err := msg.Validate(); err != nil {
			t.Fatalf("Validate() on %s = %v, want nil with zero ReasoningSignature", role, err)
		}
	}
}
