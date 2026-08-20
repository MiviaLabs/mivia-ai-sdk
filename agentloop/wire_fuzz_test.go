package agentloop

import (
	"encoding/json"
	"testing"
)

// FuzzCanonicalizeArgs proves canonicalizeArgs never panics on
// arbitrary bytes, and that a successful canonicalization is
// idempotent: re-canonicalizing the returned form yields the same
// string. canonicalizeArgs is unexported, so this fuzz target lives in
// the internal package, the documented exception for an invariant the
// external test package cannot reach directly. See dedup_test.go and
// dedup_canonicalize_test.go for the same function's behavioral cases
// through the exported Loop path.
func FuzzCanonicalizeArgs(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{}`),
		[]byte(`{"a":1}`),
		[]byte(`{"a":1,"b":2}`),
		[]byte(`{"b":2,"a":1}`),
		[]byte(`{"n":1.0}`),
		[]byte(`{"id":9007199254740993}`),
		[]byte(`{"a":1,"a":2}`),
		[]byte(`{"s":"café"}`),
		[]byte(`{not json`),
		[]byte(`{"a":1}garbage`),
		[]byte(`{"a":1}{"b":2}`),
		[]byte(``),
		[]byte(`null`),
		[]byte(`[1,2,3]`),
		[]byte(`"a string"`),
		[]byte(`   `),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		canon, err := canonicalizeArgs(json.RawMessage(raw))
		if err != nil {
			return
		}
		again, err := canonicalizeArgs(json.RawMessage(canon))
		if err != nil {
			t.Fatalf("canonicalizeArgs(%q) succeeded, but re-canonicalizing its own output %q failed: %v", raw, canon, err)
		}
		if again != canon {
			t.Fatalf("canonicalizeArgs is not idempotent: canonicalizeArgs(%q) = %q, canonicalizeArgs(%q) = %q", raw, canon, canon, again)
		}
	})
}
