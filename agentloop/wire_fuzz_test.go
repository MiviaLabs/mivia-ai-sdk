package agentloop

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
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
		[]byte(`{"a":1}]`),
		[]byte(`{"a":1}}`),
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

// FuzzTruncateContent feeds random content and budget pairs to
// truncateContent under its documented precondition (0 < budget <
// len(content), the only shape render ever calls it with). The
// invariants: the result never exceeds budget bytes and stays valid
// UTF-8, matching TestRenderTruncationStaysValidUTF8's hand-picked
// mid-rune case in agentloop_test, but sampled instead of fixed.
func FuzzTruncateContent(f *testing.F) {
	f.Add("hello world", 5)
	f.Add(strings.Repeat("x", 100), 30)
	f.Add("héllo wörld中文字 ", 9)
	f.Add("ab", 1)
	f.Add(truncationMarker+"tail", len(truncationMarker))
	f.Fuzz(func(t *testing.T, content string, budget int) {
		if budget <= 0 || budget >= len(content) {
			t.Skip("outside truncateContent's documented precondition")
		}
		got := truncateContent(content, budget)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateContent(%q, %d) = %q, not valid UTF-8", content, budget, got)
		}
		if len(got) > budget {
			t.Fatalf("truncateContent(%q, %d) len = %d, want at most %d", content, budget, len(got), budget)
		}
	})
}
