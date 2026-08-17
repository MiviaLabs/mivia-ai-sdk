package a2a_test

import (
	"reflect"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
)

// FuzzFromPart feeds arbitrary bytes to FromPart as Part.Data. It
// must never panic. Anything it accepts must re-encode cleanly
// through ToPart, and once mapped, a further FromPart/ToPart pass must
// reproduce the exact same envelope.Message: mapped output must settle
// into a stable fixed point, not drift or silently corrupt on repeated
// mapping. The comparison starts one cycle in (m2 vs m3, not m vs m2)
// because raw fuzz input may spell an empty slice field as `"to":[]`;
// encoding/json's omitempty treats a nil slice and a zero-length slice
// as equally empty on encode, so the first mapping cycle can normalize
// `[]` away to nil. That is a property of envelope.Message's own JSON
// encoding, not an a2a defect, and every cycle after the first is
// stable. Run: go test -fuzz=FuzzFromPart ./a2a/a2a_test
func FuzzFromPart(f *testing.F) {
	seeds := []string{
		// Minimal valid message.
		`{"version":"v1","id":"msg-1","thread_id":"thread-1","intent":"assert","epistemic":"inferred","confidence":0.5,"provenance":{"source":"model:self"},"payload":"The build is green."}`,
		// Every optional field populated, matching fullMessage in
		// mapping_bench_test.go: To, InReplyTo, ContextRefs, PrevHash,
		// Provenance.Chain/Evidence, MaxHops, CostBudget, AckRequired,
		// Signer, Signature.
		`{"version":"v1","id":"msg-2","room":"platform-team","thread_id":"thread-1","to":["agent-a","agent-b"],"in_reply_to":"msg-0","intent":"challenge","epistemic":"verified","confidence":0.9,"context_refs":["sha256:bd335a0b4cfc020e9c7880814ba5d89f15a218f730691766df2de1d44f6d1c09"],"prev_hash":"sha256:bd335a0b4cfc020e9c7880814ba5d89f15a218f730691766df2de1d44f6d1c09","provenance":{"source":"tool:grep","chain":["agent-a","agent-b"],"evidence":["sha256:e1c6b2c6fdfed4042f72a98d0fd8e954ae90e2ccc72a4df2cb47c6becc8110aa"]},"max_hops":3,"cost_budget":2000,"ack_required":true,"payload":"The config loader reads mivia.toml.","signer":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","signature":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`,
		// Non-ASCII payload.
		`{"version":"v1","id":"msg-3","thread_id":"thread-1","intent":"assert","epistemic":"inferred","confidence":0.5,"provenance":{"source":"model:self"},"payload":"日本語 😀"}`,
		// Confidence out of range: decodes cleanly, fails Validate.
		`{"version":"v1","id":"msg-4","thread_id":"thread-1","intent":"assert","epistemic":"inferred","confidence":1.5,"provenance":{"source":"model:self"},"payload":"x"}`,
		// Unknown extra field alongside a valid message.
		`{"version":"v1","id":"msg-5","thread_id":"thread-1","intent":"assert","epistemic":"inferred","confidence":0.5,"provenance":{"source":"model:self"},"payload":"x","unknown_field":"ignored"}`,
		`{}`,
		`{"payload":"x"`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		mapped := a2a.Mapped{
			Part:      a2a.Part{Data: data},
			ContextID: "thread-1",
			MessageID: "msg-1",
		}
		m, err := a2a.FromPart(mapped)
		if err != nil {
			return
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("FromPart returned a message that fails its own Validate: %v", err)
		}

		remapped, err := a2a.ToPart(m)
		if err != nil {
			t.Fatalf("decoded but cannot re-encode: %v", err)
		}
		m2, err := a2a.FromPart(remapped)
		if err != nil {
			t.Fatalf("re-encoded data cannot be decoded again: %v", err)
		}

		// From here on, mapping must be a stable fixed point: cycling
		// m2 through ToPart/FromPart again must reproduce m2 exactly.
		remapped2, err := a2a.ToPart(m2)
		if err != nil {
			t.Fatalf("m2 decoded but cannot re-encode: %v", err)
		}
		m3, err := a2a.FromPart(remapped2)
		if err != nil {
			t.Fatalf("m2 re-encoded data cannot be decoded again: %v", err)
		}
		if !reflect.DeepEqual(m2, m3) {
			t.Fatalf("round trip is not a fixed point:\nm2: %+v\nm3: %+v", m2, m3)
		}
	})
}
