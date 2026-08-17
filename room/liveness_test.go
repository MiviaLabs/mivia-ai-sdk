package room

import (
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
)

// TestStaleMembersNilMonitor proves a nil hb returns ErrNoMonitor and
// a nil slice.
func TestStaleMembersNilMonitor(t *testing.T) {
	r := newRoom(t)
	got, err := r.StaleMembers(nil, time.Now())
	if !errors.Is(err, ErrNoMonitor) {
		t.Fatalf("StaleMembers(nil) error = %v, want ErrNoMonitor", err)
	}
	if got != nil {
		t.Fatalf("StaleMembers(nil) = %v, want nil", got)
	}
}

// TestStaleMembersNoBeatsRecorded proves a Monitor with no beat ever
// recorded returns an empty slice and a nil error.
func TestStaleMembersNoBeatsRecorded(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("agent-a", "founder"); err != nil {
		t.Fatalf("admit: %v", err)
	}
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	got, err := r.StaleMembers(hb, time.Now())
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("StaleMembers() = %v, want empty", got)
	}
}

// TestStaleMembersMixedAliveAndStale proves the result names only the
// past-timeout member, not the one within the timeout.
func TestStaleMembersMixedAliveAndStale(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("stale-agent", "founder"); err != nil {
		t.Fatalf("admit stale-agent: %v", err)
	}
	if err := r.Admit("fresh-agent", "founder"); err != nil {
		t.Fatalf("admit fresh-agent: %v", err)
	}
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := hb.Beat("stale-agent", base); err != nil {
		t.Fatalf("Beat(stale-agent) unexpected error: %v", err)
	}
	now := base.Add(90 * time.Second)
	if err := hb.Beat("fresh-agent", base.Add(80*time.Second)); err != nil {
		t.Fatalf("Beat(fresh-agent) unexpected error: %v", err)
	}
	got, err := r.StaleMembers(hb, now)
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	want := []string{"stale-agent"}
	if !equalStrings(got, want) {
		t.Fatalf("StaleMembers() = %v, want %v", got, want)
	}
}

// TestStaleMembersRosterIsSourceOfTruthForRemoval proves a Monitor
// that tracks an id Remove already dropped from the roster does not
// name that id: the roster, not the Monitor, decides who counts as a
// member.
func TestStaleMembersRosterIsSourceOfTruthForRemoval(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("gone-agent", "founder"); err != nil {
		t.Fatalf("admit gone-agent: %v", err)
	}
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := hb.Beat("gone-agent", base); err != nil {
		t.Fatalf("Beat(gone-agent) unexpected error: %v", err)
	}
	if err := r.Remove("gone-agent", "founder"); err != nil {
		t.Fatalf("remove gone-agent: %v", err)
	}
	got, err := r.StaleMembers(hb, base.Add(90*time.Second))
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("StaleMembers() = %v, want empty: a removed member must not appear", got)
	}
}

// TestStaleMembersNeverBeatIsNotStale proves a freshly Admitted member
// with no beat ever recorded is absent from the result: "never beat"
// is not the same as "stale."
func TestStaleMembersNeverBeatIsNotStale(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("new-agent", "founder"); err != nil {
		t.Fatalf("admit new-agent: %v", err)
	}
	hb, err := heartbeat.New(time.Nanosecond)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	got, err := r.StaleMembers(hb, time.Now())
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("StaleMembers() = %v, want empty: a never-beaten member must not appear", got)
	}
}

// TestStaleMembersSortedAcrossRoles proves the result is sorted and
// names both a stale moderator and a stale plain member: StaleMembers
// does not filter by role.
func TestStaleMembersSortedAcrossRoles(t *testing.T) {
	r := newRoom(t)
	if err := r.Admit("zebra-member", "founder"); err != nil {
		t.Fatalf("admit zebra-member: %v", err)
	}
	if err := r.Admit("apple-mod", "founder"); err != nil {
		t.Fatalf("admit apple-mod: %v", err)
	}
	if err := r.Promote("apple-mod", "founder"); err != nil {
		t.Fatalf("promote apple-mod: %v", err)
	}
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := hb.Beat("zebra-member", base); err != nil {
		t.Fatalf("Beat(zebra-member) unexpected error: %v", err)
	}
	if err := hb.Beat("apple-mod", base); err != nil {
		t.Fatalf("Beat(apple-mod) unexpected error: %v", err)
	}
	got, err := r.StaleMembers(hb, base.Add(90*time.Second))
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	want := []string{"apple-mod", "zebra-member"}
	if !equalStrings(got, want) {
		t.Fatalf("StaleMembers() = %v, want %v (sorted)", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
