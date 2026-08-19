package skills_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

func TestMatchCaseInsensitiveHit(t *testing.T) {
	r := skills.New()
	if err := r.Add(skills.Skill{Name: "deploy-bot", Instructions: "x", Triggers: []string{"Deploy"}}); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	got := r.Match("deploy")
	if len(got) != 1 || got[0].Name != "deploy-bot" {
		t.Fatalf("Match(deploy) = %v, want [deploy-bot]", got)
	}
}

func TestMatchNoHitReturnsNil(t *testing.T) {
	r := skills.New()
	if err := r.Add(skills.Skill{Name: "deploy-bot", Instructions: "x", Triggers: []string{"deploy"}}); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	got := r.Match("unrelated")
	if got != nil {
		t.Fatalf("Match(unrelated) = %v, want nil", got)
	}
}

func TestMatchBlankQueryReturnsNil(t *testing.T) {
	r := skills.New()
	if err := r.Add(skills.Skill{Name: "deploy-bot", Instructions: "x", Triggers: []string{"deploy"}}); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	got := r.Match("")
	if got != nil {
		t.Fatalf("Match(\"\") = %v, want nil", got)
	}
}

func TestMatchDoesNotTrimQuery(t *testing.T) {
	r := skills.New()
	if err := r.Add(skills.Skill{Name: "deploy-bot", Instructions: "x", Triggers: []string{"deploy"}}); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	got := r.Match(" deploy")
	if got != nil {
		t.Fatalf("Match(' deploy') = %v, want nil (padded query must not match unpadded trigger)", got)
	}
}

func TestMatchReturnsAllHitsSortedByName(t *testing.T) {
	r := skills.New()
	names := []string{"charlie", "alpha", "bravo"}
	for _, name := range names {
		if err := r.Add(skills.Skill{Name: name, Instructions: "x", Triggers: []string{"shared"}}); err != nil {
			t.Fatalf("Add(%s) error = %v, want nil", name, err)
		}
	}
	got := r.Match("shared")
	if len(got) != 3 {
		t.Fatalf("Match(shared) = %v, want 3 entries", got)
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("Match(shared)[%d].Name = %q, want %q", i, got[i].Name, w)
		}
	}
}

// TestMatchSearchesTriggersOnlyNotName proves Match never compares
// against Name: a skill named "deploy-bot" with Triggers that do not
// contain "deploy-bot" must not match a query for its own Name.
func TestMatchSearchesTriggersOnlyNotName(t *testing.T) {
	r := skills.New()
	if err := r.Add(skills.Skill{Name: "deploy-bot", Instructions: "x", Triggers: []string{"deploy", "release"}}); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	got := r.Match("deploy-bot")
	if got != nil {
		t.Fatalf("Match(deploy-bot) = %v, want nil (Match must search Triggers, not Name)", got)
	}
}
