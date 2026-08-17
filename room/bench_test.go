package room_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/heartbeat"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

// buildThousandMemberRoom creates a Room with one thousand admitted
// members plus its founder, and a Monitor tracking a beat for each
// member: half past the timeout at the returned now, half within it.
func buildThousandMemberRoom() (*room.Room, *heartbeat.Monitor, time.Time) {
	r, err := room.New("bench-room", "founder")
	if err != nil {
		panic("buildThousandMemberRoom: " + err.Error())
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	timeout := time.Minute
	hb, err := heartbeat.New(timeout)
	if err != nil {
		panic("buildThousandMemberRoom: " + err.Error())
	}
	now := base.Add(90 * time.Second)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("agent-%04d", i)
		if err := r.Admit(id, "founder"); err != nil {
			panic("buildThousandMemberRoom: " + err.Error())
		}
		at := base
		if i%2 == 1 {
			at = now.Add(-time.Second)
		}
		if err := hb.Beat(id, at); err != nil {
			panic("buildThousandMemberRoom: " + err.Error())
		}
	}
	return r, hb, now
}

// BenchmarkStaleMembersThousandMembers benchmarks StaleMembers against
// a Room holding one thousand members, half of them past the timeout.
// Measured: ~139000 ns/op (dominated by hb.Dead's own linear scan and
// sort over one thousand tracked ids).
func BenchmarkStaleMembersThousandMembers(b *testing.B) {
	r, hb, now := buildThousandMemberRoom()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := r.StaleMembers(hb, now); err != nil || len(got) != 500 {
			b.Fatalf("StaleMembers() = (%v, %v), want 500 ids, nil error", got, err)
		}
	}
}

// TestStaleMembersAllocBudget guards the allocation floor for
// StaleMembers over one thousand members. The measured baseline is
// ~17 allocations: hb.Dead's own copy and sort, the dead-id lookup
// set StaleMembers builds from it, and the sorted result slice. The
// budget allows headroom above the baseline to absorb a small,
// legitimate change in either package without masking a real
// regression, such as StaleMembers switching to an O(n^2) scan.
func TestStaleMembersAllocBudget(t *testing.T) {
	r, hb, now := buildThousandMemberRoom()
	alloc := testing.AllocsPerRun(100, func() {
		if got, err := r.StaleMembers(hb, now); err != nil || len(got) != 500 {
			t.Fatalf("StaleMembers() = (%v, %v), want 500 ids, nil error", got, err)
		}
	})
	if alloc > 25 {
		t.Fatalf("StaleMembers allocated %v times per call; budget is 25", alloc)
	}
}

// buildThousandMemberRoomWithPoster creates a Room with one thousand
// members plus a distinguished poster, and returns a validly signed
// message from the poster ready for Accepts.
func buildThousandMemberRoomWithPoster(b *testing.B) (*room.Room, envelope.Message) {
	b.Helper()
	r, err := room.New("bench-room", "founder")
	if err != nil {
		b.Fatalf("new room: %v", err)
	}
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("agent-%04d", i)
		if err := r.Admit(id, "founder"); err != nil {
			b.Fatalf("admit: %v", err)
		}
	}
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}
	posterID := hex.EncodeToString(pub)
	if err := r.Admit(posterID, "founder"); err != nil {
		b.Fatalf("admit poster: %v", err)
	}
	msg := baseMessage(r.ID())
	msg.ID = "bench-msg"
	signed, err := envelope.Sign(key, msg)
	if err != nil {
		b.Fatalf("sign: %v", err)
	}
	return r, signed
}

// BenchmarkAcceptsThousandMembers benchmarks Accepts against a Room
// holding one thousand members: signature verification plus a
// roster-membership check on the signer.
// Measured: ~37700 ns/op (dominated by ed25519 signature
// verification; the roster-membership check itself is a map lookup).
func BenchmarkAcceptsThousandMembers(b *testing.B) {
	r, signed := buildThousandMemberRoomWithPoster(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Accepts(signed); err != nil {
			b.Fatalf("Accepts() unexpected error: %v", err)
		}
	}
}
