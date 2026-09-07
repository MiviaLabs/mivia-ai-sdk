package skills_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

// FuzzSkillValidate feeds arbitrary Name and Instructions strings to
// Skill.Validate. It must never panic, and the result must match the
// documented check order: Name is checked before Instructions, both
// after strings.TrimSpace.
func FuzzSkillValidate(f *testing.F) {
	seeds := []string{"", " ", "\t", "\n", "deploy", "  deploy  ", "emoji-🔧-name", "\x00\x01"}
	for _, name := range seeds {
		for _, instructions := range seeds {
			f.Add(name, instructions)
		}
	}
	f.Fuzz(func(t *testing.T, name, instructions string) {
		s := skills.Skill{Name: name, Instructions: instructions}
		err := s.Validate()

		switch {
		case strings.TrimSpace(name) == "":
			if !errors.Is(err, skills.ErrBlankName) {
				t.Fatalf("Validate() with name %q instructions %q = %v, want errors.Is ErrBlankName", name, instructions, err)
			}
		case strings.TrimSpace(instructions) == "":
			if !errors.Is(err, skills.ErrBlankInstructions) {
				t.Fatalf("Validate() with name %q instructions %q = %v, want errors.Is ErrBlankInstructions", name, instructions, err)
			}
		default:
			if err != nil {
				t.Fatalf("Validate() with name %q instructions %q = %v, want nil", name, instructions, err)
			}
		}
	})
}

// FuzzAddGetRemove feeds arbitrary Skill names through Add, Get, and
// Remove. It proves no name panics the Registry, and that a blank
// name after strings.TrimSpace always fails Add with ErrBlankName
// while any other name round-trips: Add succeeds, Get finds it, and
// Remove drops it so a later Get reports it absent.
func FuzzAddGetRemove(f *testing.F) {
	f.Add("deploy")
	f.Add("")
	f.Add("   ")
	f.Add(" deploy")
	f.Add("deploy ")
	f.Add("\x00\x01")
	f.Add("emoji-🔧-name")

	f.Fuzz(func(t *testing.T, name string) {
		r := skills.New()
		err := r.Add(skills.Skill{Name: name, Instructions: "do the thing"})

		if strings.TrimSpace(name) == "" {
			if !errors.Is(err, skills.ErrBlankName) {
				t.Fatalf("Add(%q) error = %v, want ErrBlankName", name, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Add(%q) error = %v, want nil", name, err)
		}

		got, ok := r.Get(name)
		if !ok || got.Name != name {
			t.Fatalf("Get(%q) = %+v, %v, want the added Skill and true", name, got, ok)
		}
		if removed := r.Remove(name); !removed {
			t.Fatalf("Remove(%q) = false, want true", name)
		}
		if _, ok := r.Get(name); ok {
			t.Fatalf("Get(%q) ok = true after Remove, want false", name)
		}
	})
}
