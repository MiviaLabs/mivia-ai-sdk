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
		t.Error("Matches(sibling) = true, want false: a metacharacter-free body needs a segment boundary")
	}
}

func TestMatchesDirectoryGlob(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		{"glob body matches file under the tree", []string{"secret*/"}, "secrets/key.pem", true},
		{"glob body matches the directory itself", []string{"secret*/"}, "secrets", true},
		{"glob body matches a deep file", []string{"secret*/"}, "secrets/deep/nested/key.pem", true},
		{"glob body anchors at the start", []string{"secret*/"}, "public/secrets/key.pem", false},
		{"multi-segment body matches the directory", []string{"a/*/keys/"}, "a/x/keys", true},
		{"multi-segment body matches a deep file", []string{"a/*/keys/"}, "a/x/keys/deep/file.txt", true},
		{"multi-segment body needs every segment", []string{"a/*/keys/"}, "a/keys", false},
		{"star never crosses a separator", []string{"a/*/keys/"}, "a/x/y/keys/f", false},
		{"empty body matches any absolute path", []string{"/"}, "/etc/passwd", true},
		{"empty body never matches a relative path", []string{"/"}, "a/b", false},
		{"star body matches a lone segment", []string{"*/"}, "a", true},
		{"star body matches the first segment of a deeper path", []string{"*/"}, "a/b/c", true},
		{"star body never matches an absolute path", []string{"*/"}, "/etc/passwd", false},
		{"absolute body matches a file under the tree", []string{"/etc*/"}, "/etc/passwd", true},
		{"absolute body matches the directory itself", []string{"/etc*/"}, "/etcx", true},
		{"absolute body anchors at the root", []string{"/etc*/"}, "/opt/etc/passwd", false},
		{"backslash escapes the next character", []string{`sec\rets/`}, "secrets/key.pem", true},
		{"backslash escapes a metacharacter", []string{`sec\*ets/`}, "secXets/f", false},
		{"escaped metacharacter matches itself", []string{`sec\*ets/`}, "sec*ets/f", true},
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

func TestMatchesDirectoryGlobNegation(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		checks   map[string]bool
	}{
		{
			name:     "negated file inside a glob directory tree",
			patterns: []string{"secret*/", "!secret*/public.txt"},
			checks: map[string]bool{
				"secrets/public.txt": false,
				"secrets/key.pem":    true,
			},
		},
		{
			name:     "star directory negation never clears an absolute path",
			patterns: []string{"/etc/", "!*/"},
			checks: map[string]bool{
				"/etc/passwd": true,
			},
		},
		{
			name:     "glob directory negation clears a whole tree",
			patterns: []string{"secrets/key.pem", "!secret*/"},
			checks: map[string]bool{
				"secrets/key.pem": false,
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

func TestNewMatcherInvalidDirectoryGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{"positive directory pattern", "bad[/"},
		{"negated directory pattern", "!bad[/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := secretpath.NewMatcher([]string{tt.pattern})
			if err == nil {
				t.Fatalf("NewMatcher(%q) = nil error, want error", tt.pattern)
			}
			if !strings.Contains(err.Error(), "pattern 0") {
				t.Errorf("NewMatcher(%q) error = %v, want to name pattern index 0", tt.pattern, err)
			}
		})
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

func TestNewMatcherEmptyPatternList(t *testing.T) {
	m, err := secretpath.NewMatcher(nil)
	if err != nil {
		t.Fatalf("NewMatcher(nil): %v", err)
	}
	if m.Matches("secrets/key.pem") {
		t.Error("Matches() with no patterns = true, want false")
	}
}

// TestMatchesNilReceiver pins the nil guard. Without it the method
// reads m.patterns on a nil receiver and panics.
func TestMatchesNilReceiver(t *testing.T) {
	var m *secretpath.Matcher
	cases := []string{"secrets/key.pem", "", "/etc/passwd"}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if m.Matches(input) {
				t.Errorf("Matches(%q) on a nil Matcher = true, want false", input)
			}
		})
	}
}

func TestMatchesEmptyPathInput(t *testing.T) {
	m, err := secretpath.NewMatcher([]string{"secrets/*"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if m.Matches("") {
		t.Error("Matches(\"\") = true, want false")
	}
}

func TestMatchesTraversal(t *testing.T) {
	m, err := secretpath.NewMatcher([]string{"secrets/"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if m.Matches("../secrets/key.pem") {
		t.Error("Matches(../secrets/key.pem) = true, want false: parent traversal leaves the pattern's tree")
	}
	if !m.Matches("a/../secrets/key.pem") {
		t.Error("Matches(a/../secrets/key.pem) = false, want true: path.Clean resolves the internal traversal to secrets/key.pem")
	}
}

func TestMatchesCaseSensitive(t *testing.T) {
	m, err := secretpath.NewMatcher([]string{"secrets/key.pem"})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if m.Matches("Secrets/Key.pem") {
		t.Error("Matches(Secrets/Key.pem) = true, want false: matching is case sensitive")
	}
}

// FuzzMatches feeds arbitrary pattern lists and path strings to
// NewMatcher and Matches. It must never panic, and Matches must
// return the same result on the same compiled Matcher and input
// twice, since Matches has no side effect on its input.
// Run: go test -fuzz=FuzzMatches ./secretpath/secretpath_test/
func FuzzMatches(f *testing.F) {
	seeds := []struct {
		pattern string
		path    string
	}{
		{"secrets/key.pem", "secrets/key.pem"},
		{"secrets/*.pem", "secrets/key.pem"},
		{"secrets/", "secrets/a/b/file.txt"},
		{"!secrets/public.txt", "secrets/public.txt"},
		{"", ""},
		{"secrets/key[AB].pem", "secrets/keyA.pem"},
		{`secrets\key.pem`, `secrets\key.pem`},
		{"../secrets/key.pem", "../secrets/key.pem"},
		{"secret*/", "secrets/key.pem"},
		{"a/*/keys/", "a/x/keys/f"},
		{"*/", "a/b"},
		{"/", "/etc/passwd"},
		{"*/", "/etc/passwd"},
	}
	for _, s := range seeds {
		f.Add(s.pattern, s.path)
	}
	f.Fuzz(func(t *testing.T, pattern, path string) {
		m, err := secretpath.NewMatcher([]string{pattern})
		if err != nil {
			return
		}
		first := m.Matches(path)
		second := m.Matches(path)
		if first != second {
			t.Fatalf("Matches(%q) not stable: first %v, second %v", path, first, second)
		}
	})
}
