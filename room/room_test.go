package room

import (
	"errors"
	"testing"
)

func newRoom(t *testing.T) *Room {
	t.Helper()
	r, err := New("platform-team", "founder")
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	return r
}

func TestNewRequiresIDAndFounder(t *testing.T) {
	if _, err := New("", "founder"); err == nil {
		t.Fatal("empty id must fail")
	}
	if _, err := New("room", "  "); err == nil {
		t.Fatal("empty founder must fail")
	}
}

func TestAdmitFlow(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("agent-a", "founder"); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !r.IsMember("agent-a") {
		t.Fatal("admitted agent must be a member")
	}
	if got := r.Members(); len(got) != 2 || got[0] != "agent-a" || got[1] != "founder" {
		t.Fatalf("members = %v, want sorted [agent-a founder]", got)
	}
}

func TestAdmitRejectsEmptyMemberID(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("   ", "founder"); err == nil {
		t.Fatal("admit of an empty member id must fail")
	}
}

func TestMembershipGuards(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("agent-a", "founder"); err != nil {
		t.Fatalf("admit: %v", err)
	}

	cases := map[string]struct {
		err  error
		call func() error
	}{
		"non-moderator admits":   {ErrNotModerator, func() error { return r.Admit("agent-b", "agent-a") }},
		"stranger admits":        {ErrNotModerator, func() error { return r.Admit("agent-b", "ghost") }},
		"duplicate admit":        {ErrAlreadyMember, func() error { return r.Admit("agent-a", "founder") }},
		"remove stranger":        {ErrNotMember, func() error { return r.Remove("ghost", "founder") }},
		"remove last moderator":  {ErrLastModerator, func() error { return r.Remove("founder", "founder") }},
		"last moderator leaves":  {ErrLastModerator, func() error { return r.Leave("founder") }},
		"stranger leaves":        {ErrNotMember, func() error { return r.Leave("ghost") }},
		"non-moderator removes":  {ErrNotModerator, func() error { return r.Remove("agent-a", "agent-a") }},
		"non-moderator promotes": {ErrNotModerator, func() error { return r.Promote("agent-a", "agent-a") }},
		"promote stranger":       {ErrNotMember, func() error { return r.Promote("ghost", "founder") }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestPromoteThenRemoveFounder(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("agent-a", "founder"); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := r.Promote("agent-a", "founder"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	// Two moderators now; founder can leave.
	if err := r.Leave("founder"); err != nil {
		t.Fatalf("founder leave: %v", err)
	}
	if r.IsMember("founder") {
		t.Fatal("founder must be gone")
	}
	// agent-a is the last moderator and cannot be removed.
	if err := r.Remove("agent-a", "agent-a"); !errors.Is(err, ErrLastModerator) {
		t.Fatalf("err = %v, want %v", err, ErrLastModerator)
	}
}

func TestMemberLeave(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("agent-a", "founder"); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := r.Leave("agent-a"); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if r.IsMember("agent-a") {
		t.Fatal("member must be gone after leave")
	}
}
