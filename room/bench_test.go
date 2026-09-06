package room_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/room"
)

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
