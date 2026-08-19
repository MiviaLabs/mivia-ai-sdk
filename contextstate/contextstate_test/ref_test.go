package contextstate_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// emptyDigest is the known SHA-256 digest of the empty input.
const emptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// alphaDigest is the pinned SHA-256 digest of "alpha".
const alphaDigest = "8ed3f6ad685b959ead7022518e1af76cd816f8e8ec7ccdda1ed4018e8f2223f8"

func TestMintDeterministic(t *testing.T) {
	first := contextstate.Mint([]byte("alpha"))
	second := contextstate.Mint([]byte("alpha"))
	if first != second {
		t.Fatalf("Mint not deterministic: %q vs %q", first, second)
	}
	if want := contextstate.HashPrefix + alphaDigest; first != want {
		t.Fatalf("Mint([]byte(\"alpha\")) = %q, want %q", first, want)
	}
	if prefix, ok := strings.CutPrefix(first, contextstate.HashPrefix); !ok {
		t.Fatalf("Mint result %q lacks the hash prefix", first)
	} else if prefix != alphaDigest {
		t.Fatalf("digest part %q, want %q", prefix, alphaDigest)
	}
}

func TestMintChunkConcatenation(t *testing.T) {
	// Byte-aligned and rune-splitting cuts: every cut of the whole,
	// including cuts inside a multi-byte rune, mints the whole.
	for _, whole := range []string{"alpha-omega", "héllo-wörld-αβγ"} {
		bytes := []byte(whole)
		want := contextstate.Mint(bytes)
		for cut := 1; cut < len(bytes); cut++ {
			got := contextstate.Mint(bytes[:cut], bytes[cut:])
			if got != want {
				t.Fatalf("split of %q at %d: %q, want %q", whole, cut, got, want)
			}
		}
	}
	// Empty chunks between filled ones do not change the digest.
	a, b := []byte("alpha"), []byte("-omega")
	joined := append(append([]byte{}, a...), b...)
	want := contextstate.Mint(joined)
	if got := contextstate.Mint(a, nil, []byte{}, b); got != want {
		t.Fatalf("Mint with empty middle chunks = %q, want %q", got, want)
	}
}

func TestMintEmptyInput(t *testing.T) {
	want := contextstate.HashPrefix + emptyDigest
	cases := []struct {
		name  string
		chunk []byte
	}{
		{"nil chunk", nil},
		{"empty chunk", []byte{}},
	}
	if got := contextstate.Mint(); got != want {
		t.Fatalf("Mint() = %q, want %q", got, want)
	}
	if got := contextstate.Digest(); got != emptyDigest {
		t.Fatalf("Digest() = %q, want %q", got, emptyDigest)
	}
	for _, tc := range cases {
		if got := contextstate.Mint(tc.chunk); got != want {
			t.Fatalf("Mint(%s) = %q, want %q", tc.name, got, want)
		}
	}
	if got := contextstate.Mint(nil, nil); got != want {
		t.Fatalf("Mint(nil, nil) = %q, want %q", got, want)
	}
}

func TestIsRef(t *testing.T) {
	canonical := contextstate.HashPrefix + emptyDigest
	hex64 := strings.Repeat("0", 16)
	cases := []struct {
		name string
		ref  string
		want bool
	}{
		{"canonical", canonical, true},
		{"canonical of data", contextstate.Mint([]byte("alpha")), true},
		{"empty", "", false},
		{"bare digest", emptyDigest, false},
		{"16-hex truncated", contextstate.HashPrefix + hex64, false},
		{"63-hex", contextstate.HashPrefix + emptyDigest[:63], false},
		{"65-hex", canonical + "a", false},
		{"oversized", canonical + "0123456789abcdef", false},
		{"uppercase", contextstate.HashPrefix + strings.ToUpper(emptyDigest), false},
		{"non-hex", contextstate.HashPrefix + strings.Replace(emptyDigest, "a", "g", 1), false},
		{"leading whitespace", " " + canonical, false},
		{"trailing whitespace", canonical + " ", false},
		{"inner whitespace", canonical[:32] + " " + canonical[32:], false},
		{"extra colon after prefix", contextstate.HashPrefix + ":" + emptyDigest, false},
		{"extra colon before digest", "sha256::" + emptyDigest, false},
		{"prefix alone", contextstate.HashPrefix, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextstate.IsRef(tc.ref); got != tc.want {
				t.Fatalf("IsRef(%q) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}
