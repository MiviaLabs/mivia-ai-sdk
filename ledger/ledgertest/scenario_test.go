package ledgertest_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/ledger/ledgertest"
)

// fullScenario returns a ledgertest.Scenario with every field set,
// wired against a fresh referenceClaim.
func fullScenario() ledgertest.Scenario {
	return newReferenceClaim().scenario()
}

// TestScenarioValidateComplete proves Validate reports nil when every
// field is set.
func TestScenarioValidateComplete(t *testing.T) {
	if err := fullScenario().Validate(); err != nil {
		t.Fatalf("Validate: %v, want nil", err)
	}
}

// TestScenarioValidateMissingField proves Validate reports
// ErrIncompleteScenario when exactly one field is nil, one case per
// field.
func TestScenarioValidateMissingField(t *testing.T) {
	cases := []struct {
		name  string
		clear func(*ledgertest.Scenario)
	}{
		{"Claim", func(s *ledgertest.Scenario) { s.Claim = nil }},
		{"Takeover", func(s *ledgertest.Scenario) { s.Takeover = nil }},
		{"Mutate", func(s *ledgertest.Scenario) { s.Mutate = nil }},
		{"Release", func(s *ledgertest.Scenario) { s.Release = nil }},
		{"IsHeld", func(s *ledgertest.Scenario) { s.IsHeld = nil }},
		{"IsFenced", func(s *ledgertest.Scenario) { s.IsFenced = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fullScenario()
			c.clear(&s)
			err := s.Validate()
			if !errors.Is(err, ledgertest.ErrIncompleteScenario) {
				t.Fatalf("Validate: got %v, want ErrIncompleteScenario", err)
			}
		})
	}
}
