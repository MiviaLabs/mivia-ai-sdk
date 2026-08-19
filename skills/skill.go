// Package skills holds a reusable instruction bundle a caller
// registers under a name and finds again by trigger phrase or by
// name. A Skill is read, not called: it carries guidance text, not a
// callable action.
package skills

import (
	"errors"
	"strings"
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
// defensively copy either, matching discovery.Card's documented
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
	seen := make([]string, 0, len(s.Triggers))
	for _, trigger := range s.Triggers {
		trimmed := strings.TrimSpace(trigger)
		if trimmed == "" {
			return ErrBlankTrigger
		}
		for _, prior := range seen {
			if strings.EqualFold(trimmed, prior) {
				return ErrDuplicateTrigger
			}
		}
		seen = append(seen, trimmed)
	}
	return nil
}
