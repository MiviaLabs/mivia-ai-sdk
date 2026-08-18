package provider_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// errFakeEstimate is the fake TokenEstimator's fixed failure sentinel.
var errFakeEstimate = errors.New("tokenEstimatingFake: estimate failed")

func TestTokenEstimatorReturnsConfiguredCount(t *testing.T) {
	cases := []struct {
		name   string
		req    provider.Request
		tokens int
	}{
		{
			name:   "empty request",
			req:    provider.Request{},
			tokens: 0,
		},
		{
			name: "single message",
			req: provider.Request{
				Model:    "test-model",
				Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
			},
			tokens: 12,
		},
		{
			name: "multiple messages and tools",
			req: provider.Request{
				Model: "test-model",
				Messages: []provider.Message{
					{Role: provider.RoleUser, Content: "hello"},
					{Role: provider.RoleAssistant, Content: "hi there"},
				},
				Tools: []provider.ToolDefinition{{Name: "lookup"}},
			},
			tokens: 42,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &tokenEstimatingFake{fakeCompleter: fakeCompleter{name: "estimator"}, tokens: tc.tokens}
			var c provider.Completer = f

			te, ok := c.(provider.TokenEstimator)
			if !ok {
				t.Fatal("tokenEstimatingFake does not satisfy TokenEstimator")
			}
			n, err := te.EstimateTokens(tc.req)
			if err != nil {
				t.Fatalf("EstimateTokens() error = %v, want nil", err)
			}
			if n != tc.tokens {
				t.Fatalf("EstimateTokens() = %d, want %d", n, tc.tokens)
			}
		})
	}
}

func TestTokenEstimatorReturnsErrorUnwrapped(t *testing.T) {
	f := &tokenEstimatingFake{fakeCompleter: fakeCompleter{name: "estimator"}, err: errFakeEstimate}
	var c provider.Completer = f

	te, ok := c.(provider.TokenEstimator)
	if !ok {
		t.Fatal("tokenEstimatingFake does not satisfy TokenEstimator")
	}
	n, err := te.EstimateTokens(provider.Request{})
	if !errors.Is(err, errFakeEstimate) {
		t.Fatalf("EstimateTokens() error = %v, want errors.Is errFakeEstimate", err)
	}
	if n != 0 {
		t.Fatalf("EstimateTokens() count = %d, want 0", n)
	}
}

func TestCapableFakeDoesNotSatisfyTokenEstimator(t *testing.T) {
	capable := &capableFake{fakeCompleter: fakeCompleter{name: "capable"}, contextWindow: 128000, reasoningEffort: "high"}
	var c provider.Completer = capable

	if _, ok := c.(provider.TokenEstimator); ok {
		t.Fatal("capableFake unexpectedly satisfies TokenEstimator")
	}
}
