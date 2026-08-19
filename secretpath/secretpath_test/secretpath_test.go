package secretpath_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/secretpath"
)

func TestMatchesGlobCases(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{"exact file match", []string{"secrets/key.pem"}, "secrets/key.pem", true},
		{"star wildcard", []string{"secrets/*.pem"}, "secrets/key.pem", true},
		{"question wildcard", []string{"secrets/key?.pem"}, "secrets/keyA.pem", true},
		{"character class", []string{"secrets/key[AB].pem"}, "secrets/keyA.pem", true},
		{"no match", []string{"secrets/key.pem"}, "public/key.pem", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := secretpath.NewMatcher(tt.patterns)
			if err != nil {
				t.Fatalf("NewMatcher: %v", err)
			}
			if got := m.Matches(tt.path); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchesDirectoryPattern(t *testing.T) {
	m, err := secretpath.NewMatcher([]string{"secrets/"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if !m.Matches("secrets/a/b/file.txt") {
		t.Error("Matches(nested file under secrets/) = false, want true")
	}
	if m.Matches("secrets-other/file.txt") {
		t.Error("Matches(sibling with shared prefix) = true, want false")
	}
}

func TestMatchesNegation(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		checks   map[string]bool
	}{
		{
			name:     "positive then negated file flips that file",
			patterns: []string{"secrets/*", "!secrets/public.txt"},
			checks: map[string]bool{
				"secrets/key.pem":    true,
				"secrets/public.txt": false,
			},
		},
		{
			name:     "negation before positive has no effect",
			patterns: []string{"!secrets/public.txt", "secrets/*"},
			checks: map[string]bool{
				"secrets/public.txt": true,
			},
		},
		{
			name:     "directory pattern with negated file, siblings stay secret",
			patterns: []string{"secrets/", "!secrets/public.txt"},
			checks: map[string]bool{
				"secrets/public.txt": false,
				"secrets/key.pem":    true,
			},
		},
		{
			name:     "four pattern last matching wins",
			patterns: []string{"secrets/", "!secrets/a.txt", "secrets/a.txt", "!secrets/a.txt"},
			checks: map[string]bool{
				"secrets/a.txt": false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := secretpath.NewMatcher(tt.patterns)
			if err != nil {
				t.Fatalf("NewMatcher: %v", err)
			}
			for p, want := range tt.checks {
				if got := m.Matches(p); got != want {
					t.Errorf("Matches(%q) = %v, want %v", p, got, want)
				}
			}
		})
	}
}

func TestMatchesNormalization(t *testing.T) {
	m, err := secretpath.NewMatcher([]string{"secrets/key.pem"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	inputs := []string{
		"./secrets/key.pem",
		"secrets//key.pem",
		`secrets\key.pem`,
	}
	for _, in := range inputs {
		if !m.Matches(in) {
			t.Errorf("Matches(%q) = false, want true", in)
		}
	}
}

func TestNewMatcherInvalidPattern(t *testing.T) {
	_, err := secretpath.NewMatcher([]string{"ok/*", "bad/[unbalanced"})
	if err == nil {
		t.Fatal("NewMatcher() = nil error, want error")
	}
	if !strings.Contains(err.Error(), "pattern 1") {
		t.Errorf("NewMatcher() error = %v, want to name pattern index 1", err)
	}
}
