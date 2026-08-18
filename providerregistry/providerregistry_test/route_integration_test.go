package providerregistry_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/providerregistry"
)

// TestRouteIntegrationRunsThroughRunTurn wires two fakes through a
// real Registry and a real provider.RunTurn call. The first fake's
// Chat fails; the second returns a valid Response for a non-streaming
// Request. Route falls through on an always-retryable predicate and
// returns the second fake's Response. No mock of RunTurn itself
// exists anywhere in this package's tests.
func TestRouteIntegrationRunsThroughRunTurn(t *testing.T) {
	rateLimited := errors.New("alpha: rate limited")
	first := &fakeCompleter{name: "alpha", chatErr: rateLimited}
	want := provider.Response{
		Model: "beta-model",
		Message: provider.Message{
			Role:    provider.RoleAssistant,
			Content: "beta served the turn",
		},
		Usage:        provider.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10},
		FinishReason: "stop",
	}
	second := &fakeCompleter{name: "beta", chatResp: want}

	r := providerregistry.New()
	if err := r.Register("alpha", first); err != nil {
		t.Fatalf("Register(alpha) error = %v, want nil", err)
	}
	if err := r.Register("beta", second); err != nil {
		t.Fatalf("Register(beta) error = %v, want nil", err)
	}

	req := userRequest()
	got, err := r.Route(context.Background(), req, []string{"alpha", "beta"}, func(error) bool { return true })
	if err != nil {
		t.Fatalf("Route() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Route() response = %+v, want beta's %+v", got, want)
	}
	if !reflect.DeepEqual(first.lastRequest, req) {
		t.Fatalf("first fake's request = %+v, want the caller's %+v", first.lastRequest, req)
	}
	if !reflect.DeepEqual(second.lastRequest, req) {
		t.Fatalf("second fake's request = %+v, want the caller's %+v", second.lastRequest, req)
	}
	if second.lastRequest.Stream {
		t.Fatal("second fake saw Stream = true, want false on the non-streaming path")
	}
}
