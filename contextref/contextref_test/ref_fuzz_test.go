package contextref_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextref"
)

// isCanonicalRef is the test oracle for the canonical form: HashPrefix
// then exactly 64 lowercase hex characters, nothing else.
func isCanonicalRef(s string) bool {
	rest, ok := strings.CutPrefix(s, contextref.HashPrefix)
	if !ok || len(rest) != 64 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func FuzzIsRef(f *testing.F) {
	canonical := contextref.HashPrefix + emptyDigest
	seeds := []string{
		canonical,
		contextref.Mint([]byte("alpha")),
		"",
		emptyDigest,
		contextref.HashPrefix + emptyDigest[:16],
		contextref.HashPrefix + emptyDigest[:63],
		canonical + "a",
		contextref.HashPrefix + strings.ToUpper(emptyDigest),
		" " + canonical,
		canonical + " ",
		contextref.HashPrefix + ":" + emptyDigest,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := contextref.IsRef(s), isCanonicalRef(s); got != want {
			t.Fatalf("IsRef(%q) = %v, want %v", s, got, want)
		}
	})
}
