package agentloop

import (
	"strings"
	"testing"
	"unicode/utf8"
)

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
