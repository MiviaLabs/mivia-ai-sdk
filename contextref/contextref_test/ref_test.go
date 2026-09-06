package contextref_test

import (
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextref"
)

// emptyDigest is the known SHA-256 digest of the empty input.
const emptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// alphaDigest is the pinned SHA-256 digest of "alpha".
const alphaDigest = "8ed3f6ad685b959ead7022518e1af76cd816f8e8ec7ccdda1ed4018e8f2223f8"

func TestMintDeterministic(t *testing.T) {
	first := contextref.Mint([]byte("alpha"))
	second := contextref.Mint([]byte("alpha"))
	if first != second {
		t.Fatalf("Mint not deterministic: %q vs %q", first, second)
	}
	if want := contextref.HashPrefix + alphaDigest; first != want {
		t.Fatalf("Mint([]byte(\"alpha\")) = %q, want %q", first, want)
	}
	if prefix, ok := strings.CutPrefix(first, contextref.HashPrefix); !ok {
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
		want := contextref.Mint(bytes)
		for cut := 1; cut < len(bytes); cut++ {
			got := contextref.Mint(bytes[:cut], bytes[cut:])
			if got != want {
				t.Fatalf("split of %q at %d: %q, want %q", whole, cut, got, want)
			}
		}
	}
	// Empty chunks between filled ones do not change the digest.
	a, b := []byte("alpha"), []byte("-omega")
	joined := append(append([]byte{}, a...), b...)
	want := contextref.Mint(joined)
	if got := contextref.Mint(a, nil, []byte{}, b); got != want {
		t.Fatalf("Mint with empty middle chunks = %q, want %q", got, want)
	}
}

func TestMintEmptyInput(t *testing.T) {
	want := contextref.HashPrefix + emptyDigest
	cases := []struct {
		name  string
		chunk []byte
	}{
		{"nil chunk", nil},
		{"empty chunk", []byte{}},
	}
	if got := contextref.Mint(); got != want {
		t.Fatalf("Mint() = %q, want %q", got, want)
	}
	if got := contextref.Digest(); got != emptyDigest {
		t.Fatalf("Digest() = %q, want %q", got, emptyDigest)
	}
	for _, tc := range cases {
		if got := contextref.Mint(tc.chunk); got != want {
			t.Fatalf("Mint(%s) = %q, want %q", tc.name, got, want)
		}
	}
	if got := contextref.Mint(nil, nil); got != want {
		t.Fatalf("Mint(nil, nil) = %q, want %q", got, want)
	}
}

func TestIsRef(t *testing.T) {
	canonical := contextref.HashPrefix + emptyDigest
	hex64 := strings.Repeat("0", 16)
	cases := []struct {
		name string
		ref  string
		want bool
	}{
		{"canonical", canonical, true},
		{"canonical of data", contextref.Mint([]byte("alpha")), true},
		{"empty", "", false},
		{"bare digest", emptyDigest, false},
		{"16-hex truncated", contextref.HashPrefix + hex64, false},
		{"63-hex", contextref.HashPrefix + emptyDigest[:63], false},
		{"65-hex", canonical + "a", false},
		{"oversized", canonical + "0123456789abcdef", false},
		{"uppercase", contextref.HashPrefix + strings.ToUpper(emptyDigest), false},
		{"non-hex", contextref.HashPrefix + strings.Replace(emptyDigest, "a", "g", 1), false},
		{"leading whitespace", " " + canonical, false},
		{"trailing whitespace", canonical + " ", false},
		{"inner whitespace", canonical[:32] + " " + canonical[32:], false},
		{"extra colon after prefix", contextref.HashPrefix + ":" + emptyDigest, false},
		{"extra colon before digest", "sha256::" + emptyDigest, false},
		{"prefix alone", contextref.HashPrefix, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextref.IsRef(tc.ref); got != tc.want {
				t.Fatalf("IsRef(%q) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}
