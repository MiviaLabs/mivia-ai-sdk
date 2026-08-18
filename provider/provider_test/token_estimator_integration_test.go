package provider_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// TestTokenEstimatorContextWindowComposition proves EstimateTokens and
// ContextWindow compose in caller code, with no provider-internal
// glue, per docs/plans/agents/phase44_provider_token_estimation.md's
// caller-side composition decision.
func TestTokenEstimatorContextWindowComposition(t *testing.T) {
	cases := []struct {
		name   string
		tokens int
		window int
		want   bool
	}{
		{name: "under window", tokens: 500, window: 1000, want: true},
		{name: "over window", tokens: 1500, window: 1000, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &capableTokenEstimatingFake{
				capableFake: capableFake{fakeCompleter: fakeCompleter{name: "capable-estimator"}, contextWindow: tc.window},
				tokens:      tc.tokens,
			}
			var c provider.Completer = f

			te, ok := c.(provider.TokenEstimator)
			if !ok {
				t.Fatal("capableTokenEstimatingFake does not satisfy TokenEstimator")
			}
			ca, ok := c.(provider.ContextAccountant)
			if !ok {
				t.Fatal("capableTokenEstimatingFake does not satisfy ContextAccountant")
			}

			req := provider.Request{Model: "test-model", Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}}}
			n, err := te.EstimateTokens(req)
			if err != nil {
				t.Fatalf("EstimateTokens() error = %v, want nil", err)
			}
			fits := n < ca.ContextWindow()
			if fits != tc.want {
				t.Fatalf("EstimateTokens() < ContextWindow() = %v, want %v", fits, tc.want)
			}
		})
	}
}
