package a2a_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// roundTripAllocBudget is the allocation ceiling for one ToPart plus
// FromPart round trip. Measured: 34 allocs/op plain, 37 allocs/op
// under -race (AMD Ryzen 9 9900X, go test -bench). The Encode/Unmarshal
// pair through Part.Data is the expected allocation source, so the
// budget is not zero; the margin above the race count catches a real
// regression without flaking on GC or allocator jitter.
const roundTripAllocBudget = 45

// fullMessage builds a message with every optional field set, for the
// round-trip benchmark.
func fullMessage() envelope.Message {
	return envelope.Message{
		Version:     envelope.Version,
		ID:          "msg-bench-1",
		Room:        "platform-team",
		ThreadID:    "thread-bench-1",
		To:          []string{"agent-a", "agent-b"},
		InReplyTo:   "msg-bench-0",
		Intent:      envelope.IntentChallenge,
		Epistemic:   envelope.EpistemicVerified,
		Confidence:  0.9,
		ContextRefs: []string{envelope.ContextRef("shared context")},
		PrevHash:    envelope.ContextRef("previous message"),
		Provenance: envelope.Provenance{
			Source:   "tool:grep",
			Chain:    []string{"agent-a", "agent-b"},
			Evidence: []string{envelope.ContextRef("evidence blob")},
		},
		MaxHops:     3,
		CostBudget:  2000,
		AckRequired: true,
		Payload:     "The config loader reads mivia.toml.",
		Signer:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Signature:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

// BenchmarkRoundTrip measures ToPart followed by FromPart on a full
// message. Target: under fifty microseconds per round trip on the
// reference machine.
// Baseline (empty implementation): no code, no measurement.
// Measured: 6.1 us/op, 2580 B/op, 34 allocs/op (AMD Ryzen 9 9900X, go
// test -bench).
func BenchmarkRoundTrip(b *testing.B) {
	m := fullMessage()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mapped, err := a2a.ToPart(m)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := a2a.FromPart(mapped); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRoundTripAllocBudget guards the allocation budget for one ToPart
// plus FromPart round trip.
func TestRoundTripAllocBudget(t *testing.T) {
	m := fullMessage()
	alloc := testing.AllocsPerRun(1000, func() {
		mapped, err := a2a.ToPart(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a2a.FromPart(mapped); err != nil {
			t.Fatal(err)
		}
	})
	if alloc > roundTripAllocBudget {
		t.Fatalf("round trip allocated %v times per call; budget is %d", alloc, roundTripAllocBudget)
	}
}
