package secretpath

import (
	"fmt"
	"path"
	"strings"
)

// compiledPattern is one parsed pattern in the list a Matcher checks
// in order.
type compiledPattern struct {
	negate bool
	body   string
	isDir  bool
}

// Matcher holds a compiled, ordered list of secret path patterns.
type Matcher struct {
	patterns []compiledPattern
}

// NewMatcher compiles patterns into a Matcher. patterns follow
// path.Match glob syntax, plus a trailing '/' to match a directory
// and everything under it, and a leading '!' to negate a later
// match. An invalid glob returns an error naming the pattern's index.
func NewMatcher(patterns []string) (*Matcher, error) {
	compiled := make([]compiledPattern, 0, len(patterns))
	for i, p := range patterns {
		cp, err := compilePattern(p)
		if err != nil {
			return nil, fmt.Errorf("secretpath: pattern %d: %w", i, err)
		}
		compiled = append(compiled, cp)
	}
	return &Matcher{patterns: compiled}, nil
}

// compilePattern parses one pattern's negation and directory markers
// and validates its glob syntax.
func compilePattern(p string) (compiledPattern, error) {
	cp := compiledPattern{}
	body := p
	if strings.HasPrefix(body, "!") {
		cp.negate = true
		body = body[1:]
	}
	if strings.HasSuffix(body, "/") {
		cp.isDir = true
		body = strings.TrimSuffix(body, "/")
	}
	if _, err := path.Match(body, body); err != nil {
		return compiledPattern{}, err
	}
	cp.body = body
	return cp, nil
}

// Matches reports whether path matches the compiled pattern set. It
// cleans the input with path.Clean and treats '\' the same as '/'
// first. Patterns apply in list order; the last matching pattern,
// positive or negated, decides the result.
func (m *Matcher) Matches(input string) bool {
	clean := path.Clean(strings.ReplaceAll(input, "\\", "/"))
	result := false
	for _, cp := range m.patterns {
		if patternMatches(cp, clean) {
			result = !cp.negate
		}
	}
	return result
}

// patternMatches reports whether one compiled pattern matches clean.
func patternMatches(cp compiledPattern, clean string) bool {
	if cp.isDir {
		return clean == cp.body || strings.HasPrefix(clean, cp.body+"/")
	}
	matched, err := path.Match(cp.body, clean)
	return err == nil && matched
}
