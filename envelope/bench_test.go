package envelope

import (
	"crypto/ed25519"
	"testing"
)

var benchSink struct {
	msg   Message
	bytes []byte
	err   error
	ref   string
}

func benchMessage() Message {
	return Message{
		Version:     Version,
		ID:          "bench-1",
		Room:        "platform-team",
		ThreadID:    "thread-bench",
		To:          []string{"agent-a", "agent-b"},
		Intent:      IntentRequest,
		Epistemic:   EpistemicVerified,
		Confidence:  0.9,
		ContextRefs: []string{ContextRef("shared context blob")},
		Provenance: Provenance{
			Source:   "tool:grep",
			Chain:    []string{"agent-a"},
			Evidence: []string{ContextRef("grep output")},
		},
		MaxHops: 3,
		Payload: "Summarize the config loading path in 5 bullets.",
	}
}

func benchKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	return ed25519.NewKeyFromSeed(seed)
}

func BenchmarkValidate(b *testing.B) {
	m := benchMessage()
	for i := 0; i < b.N; i++ {
		benchSink.err = m.Validate()
	}
}

func BenchmarkEncode(b *testing.B) {
	m := benchMessage()
	for i := 0; i < b.N; i++ {
		benchSink.bytes, benchSink.err = m.Encode()
	}
}

func BenchmarkDecode(b *testing.B) {
	m, _ := Sign(benchKey(), benchMessage())
	data, _ := m.Encode()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink.msg, benchSink.err = Decode(data)
	}
}

func BenchmarkSign(b *testing.B) {
	key := benchKey()
	m := benchMessage()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink.msg, benchSink.err = Sign(key, m)
	}
}

func BenchmarkVerifySignature(b *testing.B) {
	m, _ := Sign(benchKey(), benchMessage())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink.err = m.VerifySignature()
	}
}

func BenchmarkHash(b *testing.B) {
	m := benchMessage()
	for i := 0; i < b.N; i++ {
		benchSink.ref = m.Hash()
	}
}

func BenchmarkVerifyThread(b *testing.B) {
	msgs := make([]Message, 50)
	for i := range msgs {
		m := benchMessage()
		m.ID = string(rune('a' + i))
		m.Intent = IntentAssert
		if i > 0 {
			m.PrevHash = msgs[i-1].Hash()
		}
		msgs[i] = m
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink.err = VerifyThread(msgs)
	}
}
