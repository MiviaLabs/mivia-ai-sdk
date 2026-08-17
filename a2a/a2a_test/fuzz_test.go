package a2a_test

import (
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
)

// FuzzFromPart feeds arbitrary bytes to FromPart as Part.Data. It
// must never panic, and anything it accepts must re-encode cleanly
// through ToPart. Run: go test -fuzz=FuzzFromPart ./a2a/a2a_test
func FuzzFromPart(f *testing.F) {
	seeds := []string{
		`{"version":"v1","id":"msg-1","thread_id":"thread-1","intent":"assert","epistemic":"inferred","confidence":0.5,"provenance":{"source":"model:self"},"payload":"The build is green."}`,
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
		if _, err := a2a.ToPart(m); err != nil {
			t.Fatalf("decoded but cannot re-encode: %v", err)
		}
	})
}
