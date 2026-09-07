package anthropic_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestEstimateTokensUsesCountTokensEndpoint proves EstimateTokens
// returns the endpoint's exact count for a non-empty request.
func TestEstimateTokensUsesCountTokensEndpoint(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/count_tokens" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"input_tokens": 42})
	})
	est, ok := any(fix.client).(provider.TokenEstimator)
	if !ok {
		t.Fatal("anthropic.Client does not implement provider.TokenEstimator")
	}
	n, err := est.EstimateTokens(provider.Request{
		Model: "claude-test",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if n != 42 {
		t.Fatalf("EstimateTokens = %d, want 42 from the count_tokens endpoint", n)
	}
}

// TestEstimateTokensFallsBackOnServerFailure proves a server failure
// degrades to the character-ratio estimate instead of an error.
func TestEstimateTokensFallsBackOnServerFailure(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	est := provider.TokenEstimator(fix.client)
	content := strings.Repeat("a", 400)
	n, err := est.EstimateTokens(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: content},
		},
	})
	if err != nil {
		t.Fatalf("EstimateTokens fallback: %v", err)
	}
	if want := 400/4 + 1; n != want {
		t.Fatalf("EstimateTokens fallback = %d, want %d", n, want)
	}
}

// TestEstimateTokensZeroRequest pins the zero-input contract: an
// empty request estimates to zero through the fallback without an
// endpoint round trip.
func TestEstimateTokensZeroRequest(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("count_tokens endpoint must not be called for an empty request")
		writeJSON(w, http.StatusOK, map[string]int{"input_tokens": 1})
	})
	est := provider.TokenEstimator(fix.client)
	n, err := est.EstimateTokens(provider.Request{Model: "claude-test"})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if n != 0 {
		t.Fatalf("EstimateTokens = %d, want 0", n)
	}
}

// TestEstimateTokensMakesOneAttempt proves the count path makes one
// attempt with no retry schedule; the fallback covers transient
// failures instead.
func TestEstimateTokensMakesOneAttempt(t *testing.T) {
	calls := 0
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "still down", http.StatusInternalServerError)
	})
	est := provider.TokenEstimator(fix.client)
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
		},
	}
	if _, err := est.EstimateTokens(req); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if calls != 1 {
		t.Fatalf("count_tokens endpoint called %d times, want exactly 1", calls)
	}
}
