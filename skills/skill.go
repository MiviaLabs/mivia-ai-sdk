// Package skills holds a reusable instruction bundle a caller
// registers under a name and finds again by trigger phrase or by
// name. A Skill is read, not called: it carries guidance text, not a
// callable action.
package skills

import (
	"errors"
	"strings"
	"unicode"
)

// Sentinel errors for Skill.Validate and Registry.Add; test with
// errors.Is.
var (
	// ErrBlankName is Validate's error when Name is blank after
	// strings.TrimSpace.
	ErrBlankName = errors.New("skills: name must not be blank")
	// ErrBlankInstructions is Validate's error when Instructions is
	// blank after strings.TrimSpace.
	ErrBlankInstructions = errors.New("skills: instructions must not be blank")
	// ErrBlankTrigger is Validate's error when a Triggers entry is
	// blank after strings.TrimSpace.
	ErrBlankTrigger = errors.New("skills: trigger entry must not be blank")
	// ErrDuplicateTrigger is Validate's error when two Triggers
	// entries are equal under strings.EqualFold after trim.
	ErrDuplicateTrigger = errors.New("skills: duplicate trigger entry")
	// ErrDuplicateName is Add's error for a Name already registered.
	ErrDuplicateName = errors.New("skills: name already registered")
)

// Skill is a reusable instruction bundle. Name is the registration
// key. Instructions is the full guidance text a caller reads.
// Triggers is the phrase list Registry.Match compares a query
// against. RequiredTools names tool names this skill expects
// available; this package never reads or enforces it. Triggers and
// RequiredTools are exported slices; Registry.Add does not
// defensively copy either, matching flow.Card's documented
// no-copy convention for Capabilities and envelope.Message's same
// rule for its own slice fields. A caller that mutates a slice after
// Add mutates the registry's stored Skill too.
type Skill struct {
	Name          string
	Instructions  string
	Triggers      []string
	RequiredTools []string
}

// Validate checks Skill's invariants: Name is non-blank after
// strings.TrimSpace; Instructions is non-blank after
// strings.TrimSpace; every Triggers entry is non-blank after
// strings.TrimSpace; no two Triggers entries are equal under
// strings.EqualFold after trim. Triggers may be empty. RequiredTools
// carries no check; it is advisory metadata. Registry.Add calls
// Validate before it registers a skill.
func (s Skill) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return ErrBlankName
	}
	if strings.TrimSpace(s.Instructions) == "" {
		return ErrBlankInstructions
	}
	seen := make(map[string]struct{}, len(s.Triggers))
	for _, trigger := range s.Triggers {
		trimmed := strings.TrimSpace(trigger)
		if trimmed == "" {
			return ErrBlankTrigger
		}
		key := foldKey(trimmed)
		if _, ok := seen[key]; ok {
			return ErrDuplicateTrigger
		}
		seen[key] = struct{}{}
	}
	return nil
}

// foldKey maps s to a key equal exactly where strings.EqualFold calls
// the operands equal: every rune is replaced by the smallest rune in
// its unicode.SimpleFold orbit, so fold-equivalent runes collide and
// unrelated runes never do. strings.ToLower is not fold-faithful: it
// leaves "ſ" (U+017F LATIN SMALL LETTER LONG S) and "K" (U+212A
// KELVIN SIGN) unchanged even though EqualFold folds them with "s"
// and "k".
func foldKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(minFold(r))
	}
	return b.String()
}

// minFold returns the smallest rune in r's unicode.SimpleFold orbit,
// or r itself when r folds to nothing but itself. The walk terminates
// because SimpleFold always cycles back to r.
func minFold(r rune) rune {
	small := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		if f < small {
			small = f
		}
	}
	return small
}
