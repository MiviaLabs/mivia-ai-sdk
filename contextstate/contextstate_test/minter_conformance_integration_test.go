package contextstate_test

import (
	"bytes"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
)

// Pinned digests, computed with the canonical minter. A change in any
// pinned value is a wire change for every ref in this SDK.
const (
	pinnedEmptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	pinnedTextDigest  = "db357b57a30b3e39b5e9bb6786669d291add338a496fdc3a5fba8a8806eef254"
	pinnedWholeDigest = "ad75b988ebc23bb94926555e6e491c65859daaa4a1cd7e027aed7ed1444d922c"
)

// pinnedText is the pinned input for pinnedTextDigest.
const pinnedText = "contextstate minter conformance"

func TestEnvelopeContextRefDelegatesToMinter(t *testing.T) {
	cases := []struct {
		name    string
		content string
		digest  string
	}{
		{"pinned input", pinnedText, pinnedTextDigest},
		{"empty input", "", pinnedEmptyDigest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := contextstate.HashPrefix + tc.digest
			if got := contextstate.Mint([]byte(tc.content)); got != want {
				t.Fatalf("Mint = %q, want pinned %q", got, want)
			}
			if got := envelope.ContextRef(tc.content); got != want {
				t.Fatalf("envelope.ContextRef = %q, want %q", got, want)
			}
		})
	}
	t.Run("multi-chunk split", func(t *testing.T) {
		want := contextstate.HashPrefix + pinnedWholeDigest
		if got := contextstate.Mint([]byte("alpha"), []byte("-omega")); got != want {
			t.Fatalf("Mint of the split = %q, want %q", got, want)
		}
		if got := envelope.ContextRef("alpha-omega"); got != want {
			t.Fatalf("envelope.ContextRef of the joined = %q, want %q", got, want)
		}
	})
}

func TestMemoryPutDelegatesToMinter(t *testing.T) {
	store, err := memory.New(1 << 20)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	want := contextstate.HashPrefix + pinnedTextDigest
	ref, err := store.Put([]byte(pinnedText))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}
	if ref != want {
		t.Fatalf("memory ref = %q, want %q", ref, want)
	}
	if ref != contextstate.Mint([]byte(pinnedText)) {
		t.Fatal("memory ref differs from Mint")
	}
	got, err := store.Get(ref)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if !bytes.Equal(got, []byte(pinnedText)) {
		t.Fatalf("memory round trip returned %q", got)
	}
	emptyRef, err := store.Put(nil)
	if err != nil {
		t.Fatalf("store.Put of empty: %v", err)
	}
	if emptyRef != contextstate.HashPrefix+pinnedEmptyDigest {
		t.Fatalf("memory empty ref = %q", emptyRef)
	}
}
