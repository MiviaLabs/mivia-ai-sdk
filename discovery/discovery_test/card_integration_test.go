package discovery_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
)

// TestParseMatchIntegration parses a real card fixture, matches a
// request to a capability, and rejects a stranger request. It crosses
// the Parse/Match boundary on real fixture bytes, not a struct
// literal.
func TestParseMatchIntegration(t *testing.T) {
	data := readFixture(t, "valid.json")
	card, err := discovery.Parse(data)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	got, ok := card.Match("write")
	if !ok {
		t.Fatal("Match(\"write\") = false, want true")
	}
	if got != "write" {
		t.Fatalf("Match(\"write\") = %q, want %q", got, "write")
	}
	if _, ok := card.Match("deploy"); ok {
		t.Fatal("Match(\"deploy\") = true, want false")
	}
}

// TestParseMalformedFixtureFails proves a malformed card fixture fails
// Parse end to end.
func TestParseMalformedFixtureFails(t *testing.T) {
	data := readFixture(t, "malformed.json")
	if _, err := discovery.Parse(data); err == nil {
		t.Fatal("Parse(malformed.json) returned nil error, want error")
	}
}

// TestValidateRejectsStructLiteralCard proves Validate rejects a Card
// built by struct literal, bypassing Parse.
func TestValidateRejectsStructLiteralCard(t *testing.T) {
	card := discovery.Card{
		Name:         "",
		Capabilities: nil,
	}
	if err := card.Validate(); err == nil {
		t.Fatal("Validate() returned nil error, want error")
	}
}

// TestMatchOnUnvalidatedCardFirstMatchWins proves Match on a
// struct-literal, unvalidated Card with a duplicate-case capability
// entry returns the first slice-order match. Match never calls
// Validate, so this Card, which Validate would reject, still works
// with Match.
func TestMatchOnUnvalidatedCardFirstMatchWins(t *testing.T) {
	card := discovery.Card{
		Name:         "Agent A",
		Capabilities: []string{"Read", "read", "READ"},
	}
	if err := card.Validate(); err == nil {
		t.Fatal("Validate() returned nil error for a duplicate-case card, want error")
	}
	got, ok := card.Match("read")
	if !ok {
		t.Fatal("Match(\"read\") = false, want true")
	}
	if got != "Read" {
		t.Fatalf("Match(\"read\") = %q, want first slice-order entry %q", got, "Read")
	}
	if !strings.EqualFold(got, "read") {
		t.Fatalf("Match(\"read\") = %q, want a case-insensitive match", got)
	}
}
