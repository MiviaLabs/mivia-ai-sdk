package room_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// TestStaleMembersAcceptsThenBeatPattern proves the recommended
// pattern: a real Room, a real Monitor, and real signed
// envelope.Message values run through Accepts, paired with an
// explicit hb.Beat call on success. Admit two members, beat one of
// them, advance past the timeout, and assert StaleMembers names
// exactly the member that never beat again after its one beat.
func TestStaleMembersAcceptsThenBeatPattern(t *testing.T) {
	founder := newAgent(t, "founder")
	alice := newAgent(t, "alice")
	bob := newAgent(t, "bob")

	r, err := room.New("liveness-room", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(alice.id, founder.id); err != nil {
		t.Fatalf("admit alice: %v", err)
	}
	if err := r.Admit(bob.id, founder.id); err != nil {
		t.Fatalf("admit bob: %v", err)
	}

	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := baseMessage(r.ID())
	m.ID = "m1"
	m = alice.post(t, m)
	if err := r.Accepts(m); err != nil {
		t.Fatalf("Accepts(alice) unexpected error: %v", err)
	}
	if err := hb.Beat(alice.id, base); err != nil {
		t.Fatalf("Beat(alice) unexpected error: %v", err)
	}

	now := base.Add(90 * time.Second)
	got, err := r.StaleMembers(hb, now)
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	want := []string{alice.id}
	if !equalIDs(got, want) {
		t.Fatalf("StaleMembers() = %v, want %v: alice beat once and never again, bob never beat at all", got, want)
	}
}

// TestStaleMembersDropsRemovedMemberOnNextCall proves a Remove after a
// member goes stale drops it from the next StaleMembers call: the
// roster check re-runs on every call instead of caching a snapshot.
func TestStaleMembersDropsRemovedMemberOnNextCall(t *testing.T) {
	founder := newAgent(t, "founder")
	alice := newAgent(t, "alice")

	r, err := room.New("liveness-room", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(alice.id, founder.id); err != nil {
		t.Fatalf("admit alice: %v", err)
	}

	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := hb.Beat(alice.id, base); err != nil {
		t.Fatalf("Beat(alice) unexpected error: %v", err)
	}
	now := base.Add(90 * time.Second)

	first, err := r.StaleMembers(hb, now)
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	if !equalIDs(first, []string{alice.id}) {
		t.Fatalf("first StaleMembers() = %v, want [%s]", first, alice.id)
	}

	if err := r.Remove(alice.id, founder.id); err != nil {
		t.Fatalf("remove alice: %v", err)
	}
	second, err := r.StaleMembers(hb, now)
	if err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second StaleMembers() = %v, want empty after Remove", second)
	}
}

// TestStaleMembersConcurrentAccess mixes concurrent Accepts, Beat,
// Admit, Remove, and Promote calls against one shared Room and one
// shared Monitor. A final StaleMembers call must return without panic
// or a torn read. Synchronize with sync.WaitGroup only; never
// time.Sleep. Run under go test -race.
func TestStaleMembersConcurrentAccess(t *testing.T) {
	founder := newAgent(t, "founder")
	poster := newAgent(t, "poster")

	r, err := room.New("liveness-room", founder.id)
	if err != nil {
		t.Fatalf("new room: %v", err)
	}
	if err := r.Admit(poster.id, founder.id); err != nil {
		t.Fatalf("admit poster: %v", err)
	}
	hb, err := heartbeat.New(time.Minute)
	if err != nil {
		t.Fatalf("heartbeat.New() unexpected error: %v", err)
	}
	msg := baseMessage(r.ID())
	msg.ID = "stress"
	msg = poster.post(t, msg)

	const workers = 8
	const opsPerWorker = 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				id := fmt.Sprintf("agent-%d-%d", w, i)
				switch i % 6 {
				case 0:
					_ = r.Admit(id, founder.id)
				case 1:
					_ = r.Promote(id, founder.id)
				case 2:
					_ = r.Leave(id)
				case 3:
					_ = r.Remove(id, founder.id)
				case 4:
					if err := r.Accepts(msg); err == nil {
						_ = hb.Beat(msg.Signer, time.Now())
					}
				case 5:
					_, _ = r.StaleMembers(hb, time.Now())
				}
			}
		}(w)
	}
	wg.Wait()

	if _, err := r.StaleMembers(hb, time.Now()); err != nil {
		t.Fatalf("StaleMembers() unexpected error: %v", err)
	}
}

func equalIDs(a, b []string) bool {
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
