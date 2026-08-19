package skills_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/skills"
)

func TestAddInvalidSkillLeavesRegistryEmpty(t *testing.T) {
	r := skills.New()
	err := r.Add(skills.Skill{Name: "", Instructions: "x"})
	if !errors.Is(err, skills.ErrBlankName) {
		t.Fatalf("Add() error = %v, want ErrBlankName", err)
	}
	if names := r.Names(); len(names) != 0 {
		t.Fatalf("Names() = %v, want empty", names)
	}
}

func TestAddDuplicateName(t *testing.T) {
	r := skills.New()
	s := skills.Skill{Name: "deploy", Instructions: "do the thing"}
	if err := r.Add(s); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	err := r.Add(s)
	if !errors.Is(err, skills.ErrDuplicateName) {
		t.Fatalf("Add() error = %v, want ErrDuplicateName", err)
	}
}

func TestGetRegisteredAndUnknown(t *testing.T) {
	r := skills.New()
	s := skills.Skill{Name: "deploy", Instructions: "do the thing"}
	if err := r.Add(s); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}
	got, ok := r.Get("deploy")
	if !ok {
		t.Fatalf("Get(deploy) ok = false, want true")
	}
	if got.Name != s.Name || got.Instructions != s.Instructions {
		t.Fatalf("Get(deploy) = %+v, want %+v", got, s)
	}

	zero, ok := r.Get("unknown")
	if ok {
		t.Fatalf("Get(unknown) ok = true, want false")
	}
	if zero.Name != "" || zero.Instructions != "" || zero.Triggers != nil || zero.RequiredTools != nil {
		t.Fatalf("Get(unknown) = %+v, want zero value", zero)
	}
}

func TestRemovePresentAndAbsent(t *testing.T) {
	r := skills.New()
	s := skills.Skill{Name: "deploy", Instructions: "do the thing"}
	if err := r.Add(s); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}

	if ok := r.Remove("deploy"); !ok {
		t.Fatalf("Remove(deploy) = false, want true")
	}
	if _, ok := r.Get("deploy"); ok {
		t.Fatalf("Get(deploy) ok = true after Remove, want false")
	}

	if ok := r.Remove("deploy"); ok {
		t.Fatalf("Remove(deploy) second call = true, want false")
	}
}

// TestAddAfterRemoveReusesName proves a name becomes available again
// after Remove: Add, Remove, then Add the same Name again must
// succeed.
func TestAddAfterRemoveReusesName(t *testing.T) {
	r := skills.New()
	s := skills.Skill{Name: "deploy", Instructions: "do the thing"}
	if err := r.Add(s); err != nil {
		t.Fatalf("first Add() error = %v, want nil", err)
	}
	if ok := r.Remove("deploy"); !ok {
		t.Fatalf("Remove(deploy) = false, want true")
	}
	if err := r.Add(s); err != nil {
		t.Fatalf("second Add() after Remove error = %v, want nil", err)
	}
	if _, ok := r.Get("deploy"); !ok {
		t.Fatalf("Get(deploy) ok = false after re-Add, want true")
	}
}

func TestNamesListsEveryRegisteredName(t *testing.T) {
	r := skills.New()
	want := map[string]bool{"deploy": true, "review": true, "incident": true}
	for name := range want {
		if err := r.Add(skills.Skill{Name: name, Instructions: "x"}); err != nil {
			t.Fatalf("Add(%s) error = %v, want nil", name, err)
		}
	}
	got := r.Names()
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %d entries", got, len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("Names() contains unexpected %q", name)
		}
	}
}

// TestAddSharesTriggersBackingStorage proves Add does not defensively
// copy Triggers: mutating index zero of the caller's slice after Add
// must be visible through a following Get, matching discovery.Card's
// no-copy convention.
func TestAddSharesTriggersBackingStorage(t *testing.T) {
	r := skills.New()
	triggers := []string{"deploy"}
	s := skills.Skill{Name: "deploy", Instructions: "do the thing", Triggers: triggers}
	if err := r.Add(s); err != nil {
		t.Fatalf("Add() error = %v, want nil", err)
	}

	triggers[0] = "mutated"

	got, ok := r.Get("deploy")
	if !ok {
		t.Fatalf("Get(deploy) ok = false, want true")
	}
	if got.Triggers[0] != "mutated" {
		t.Fatalf("Triggers[0] = %q, want mutated (Add must share backing storage)", got.Triggers[0])
	}
}
