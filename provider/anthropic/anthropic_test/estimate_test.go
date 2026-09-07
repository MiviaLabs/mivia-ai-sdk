package anthropic_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestEstimateTokensUsesCountTokensEndpoint proves EstimateTokens
// returns the endpoint's exact count for a non-empty request, and
// that the count path posts only the fields count_tokens accepts.
func TestEstimateTokensUsesCountTokensEndpoint(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		for _, key := range []string{"max_tokens", "stream", "temperature", "tool_choice", "output_config"} {
			if _, present := body[key]; present {
				t.Errorf("count_tokens body carries rejected field %q", key)
			}
		}
		if _, present := body["model"]; !present {
			t.Error("count_tokens body lacks model")
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

// TestEstimateTokensPostsOnlyCountFields decodes the posted body and
// proves it carries model and messages but no max_tokens: the
// count_tokens endpoint rejects Messages-only fields.
func TestEstimateTokensPostsOnlyCountFields(t *testing.T) {
	var body map[string]json.RawMessage
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode count_tokens body: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]int{"input_tokens": 7})
	})
	est := provider.TokenEstimator(fix.client)
	if _, err := est.EstimateTokens(provider.Request{
		Model: "claude-test",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "hello"},
		},
	}); err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("count_tokens body carries max_tokens: %s", body["max_tokens"])
	}
	for _, key := range []string{"model", "messages"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("count_tokens body lacks %q", key)
		}
	}
}

// TestEstimateTokensCountsReplayedReasoningBlocks proves a history
// whose only weight is a signed reasoning block estimates nonzero:
// the count body replays the thinking part, and the fallback counts
// reasoning content too.
func TestEstimateTokensCountsReplayedReasoningBlocks(t *testing.T) {
	var sawThinking bool
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			for _, part := range m.Content {
				if part.Type == "thinking" {
					sawThinking = true
				}
			}
		}
		http.Error(w, "down", http.StatusInternalServerError)
	})
	est := provider.TokenEstimator(fix.client)
	req := provider.Request{
		ReasoningEffort: provider.ReasoningEffortMedium,
		Messages: []provider.Message{
			{Role: provider.RoleAssistant, ReasoningBlocks: []provider.ReasoningBlock{
				{Content: strings.Repeat("r", 200), Signature: "sig"},
			}},
		},
	}
	n, err := est.EstimateTokens(req)
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if n == 0 {
		t.Fatal("EstimateTokens = 0 for a thinking-only history; reasoning blocks must count")
	}
	if !sawThinking {
		t.Fatal("count body did not replay the reasoning block the next Chat would send")
	}
}

// TestEstimateTokensFallsBackOnNonconforming200 proves a 200 reply
// whose body is not a count_tokens envelope degrades to the ratio
// estimate instead of a confident zero.
func TestEstimateTokensFallsBackOnNonconforming200(t *testing.T) {
	_, fix := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"error": map[string]string{"type": "overloaded_error"}})
	})
	est := provider.TokenEstimator(fix.client)
	n, err := est.EstimateTokens(provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: strings.Repeat("a", 400)},
		},
	})
	if err != nil {
		t.Fatalf("EstimateTokens: %v", err)
	}
	if want := 400/4 + 1; n != want {
		t.Fatalf("EstimateTokens = %d, want the ratio estimate %d", n, want)
	}
}
