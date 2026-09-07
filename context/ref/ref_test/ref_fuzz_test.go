package ref_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/ref"
)

// isCanonicalRef is the test oracle for the canonical form: HashPrefix
// then exactly 64 lowercase hex characters, nothing else.
func isCanonicalRef(s string) bool {
	rest, ok := strings.CutPrefix(s, ref.HashPrefix)
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
	canonical := ref.HashPrefix + emptyDigest
	seeds := []string{
		canonical,
		ref.Mint([]byte("alpha")),
		"",
		emptyDigest,
		ref.HashPrefix + emptyDigest[:16],
		ref.HashPrefix + emptyDigest[:63],
		canonical + "a",
		ref.HashPrefix + strings.ToUpper(emptyDigest),
		" " + canonical,
		canonical + " ",
		ref.HashPrefix + ":" + emptyDigest,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := ref.IsRef(s), isCanonicalRef(s); got != want {
			t.Fatalf("IsRef(%q) = %v, want %v", s, got, want)
		}
	})
}
