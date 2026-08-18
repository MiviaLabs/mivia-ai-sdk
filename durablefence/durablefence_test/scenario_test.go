package durablefence_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/durablefence"
)

// fullScenario returns a durablefence.Scenario with every field set,
// wired against a fresh referenceClaim.
func fullScenario() durablefence.Scenario {
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
		clear func(*durablefence.Scenario)
	}{
		{"Claim", func(s *durablefence.Scenario) { s.Claim = nil }},
		{"Takeover", func(s *durablefence.Scenario) { s.Takeover = nil }},
		{"Mutate", func(s *durablefence.Scenario) { s.Mutate = nil }},
		{"Release", func(s *durablefence.Scenario) { s.Release = nil }},
		{"IsHeld", func(s *durablefence.Scenario) { s.IsHeld = nil }},
		{"IsFenced", func(s *durablefence.Scenario) { s.IsFenced = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := fullScenario()
			c.clear(&s)
			err := s.Validate()
			if !errors.Is(err, durablefence.ErrIncompleteScenario) {
				t.Fatalf("Validate: got %v, want ErrIncompleteScenario", err)
			}
		})
	}
}
