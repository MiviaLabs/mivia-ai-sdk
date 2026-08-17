// Package discovery_test holds the red-green unit cases for Parse,
// Validate, and Match. Each case asserted first against the empty
// package, then went green once card.go implemented the behavior.
package discovery_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
)

// readFixture loads a testdata JSON fixture by name.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// assertErrSubstr fails t unless err is non-nil and contains substr.
// A substr unique to one error path pins the test to that path.
func assertErrSubstr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatal("got nil error, want error")
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

// TestParse covers Parse's decode and validation paths against the
// card fixtures.
func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantErr   bool
		errSubstr string
		wantName  string
		wantCaps  int
	}{
		{name: "valid card parses", fixture: "valid.json", wantName: "Agent A", wantCaps: 3},
		{name: "blank name after trim is rejected", fixture: "blank_name.json", wantErr: true, errSubstr: "name is required"},
		{name: "empty capability list is rejected", fixture: "empty_capabilities.json", wantErr: true, errSubstr: "capabilities must not be empty"},
		{name: "whitespace-only capability entry is rejected after trim", fixture: "whitespace_capability.json", wantErr: true, errSubstr: "capability entry must not be blank"},
		{name: "duplicate capability entry is rejected", fixture: "duplicate_capability.json", wantErr: true, errSubstr: "duplicate capability"},
		{name: "malformed JSON is a syntax decode error", fixture: "malformed.json", wantErr: true, errSubstr: "unexpected end of JSON input"},
		{name: "type-mismatch JSON is a decode error", fixture: "type_mismatch.json", wantErr: true, errSubstr: "cannot unmarshal string"},
		{name: "unknown extra JSON field is ignored", fixture: "extra_field.json", wantName: "Agent A", wantCaps: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := discovery.Parse(readFixture(t, tt.fixture))
			if tt.wantErr {
				assertErrSubstr(t, err, tt.errSubstr)
				return
			}
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			if c.Name != tt.wantName || len(c.Capabilities) != tt.wantCaps {
				t.Fatalf("Parse() = %+v, want name %q with %d capabilities", c, tt.wantName, tt.wantCaps)
			}
		})
	}
}

// TestParseEmptyInput confirms Parse rejects zero-length input.
func TestParseEmptyInput(t *testing.T) {
	_, err := discovery.Parse([]byte(""))
	if err == nil {
		t.Fatal("Parse(\"\") returned nil error, want error")
	}
}

// TestCardValidate covers Validate directly against struct-literal
// Cards, including cases a JSON fixture cannot express cleanly.
func TestCardValidate(t *testing.T) {
	tests := []struct {
		name      string
		card      discovery.Card
		wantErr   bool
		errSubstr string
	}{
		{name: "valid card", card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", "write"}}},
		{name: "whitespace-only name is rejected", card: discovery.Card{Name: "   ", Capabilities: []string{"read"}}, wantErr: true, errSubstr: "name is required"},
		{name: "empty capabilities is rejected", card: discovery.Card{Name: "Agent A", Capabilities: []string{}}, wantErr: true, errSubstr: "capabilities must not be empty"},
		{name: "nil capabilities is rejected", card: discovery.Card{Name: "Agent A"}, wantErr: true, errSubstr: "capabilities must not be empty"},
		{name: "blank capability entry after trim is rejected", card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", ""}}, wantErr: true, errSubstr: "capability entry must not be blank"},
		{name: "whitespace-only capability entry is rejected", card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", "\t\n "}}, wantErr: true, errSubstr: "capability entry must not be blank"},
		{name: "duplicate capability differing only in case is rejected", card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", "READ"}}, wantErr: true, errSubstr: "duplicate capability"},
		{name: "duplicate capability differing only in padding is rejected", card: discovery.Card{Name: "Agent A", Capabilities: []string{"read", " read "}}, wantErr: true, errSubstr: "duplicate capability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.card.Validate()
			if tt.wantErr {
				assertErrSubstr(t, err, tt.errSubstr)
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

// TestCardMatch covers Match against a fixed capability list.
func TestCardMatch(t *testing.T) {
	card := discovery.Card{
		Name:         "Agent A",
		Capabilities: []string{"read", "write", "execute"},
	}
	tests := []struct {
		name    string
		need    string
		wantCap string
		wantHit bool
	}{
		{name: "exact match", need: "read", wantCap: "read", wantHit: true},
		{name: "case-insensitive match", need: "WRITE", wantCap: "write", wantHit: true},
		{name: "partial word does not match", need: "rea"},
		{name: "unknown capability does not match", need: "deploy"},
		{name: "blank need does not match", need: ""},
		{name: "padded need does not match unpadded entry", need: " read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := card.Match(tt.need)
			if ok != tt.wantHit {
				t.Fatalf("Match(%q) hit = %v, want %v", tt.need, ok, tt.wantHit)
			}
			if got != tt.wantCap {
				t.Fatalf("Match(%q) = %q, want %q", tt.need, got, tt.wantCap)
			}
		})
	}
}
