package contextstate_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
)

// isCanonicalRef is the test oracle for the canonical form: HashPrefix
// then exactly 64 lowercase hex characters, nothing else.
func isCanonicalRef(s string) bool {
	rest, ok := strings.CutPrefix(s, contextstate.HashPrefix)
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
	canonical := contextstate.HashPrefix + emptyDigest
	seeds := []string{
		canonical,
		contextstate.Mint([]byte("alpha")),
		"",
		emptyDigest,
		contextstate.HashPrefix + emptyDigest[:16],
		contextstate.HashPrefix + emptyDigest[:63],
		canonical + "a",
		contextstate.HashPrefix + strings.ToUpper(emptyDigest),
		" " + canonical,
		canonical + " ",
		contextstate.HashPrefix + ":" + emptyDigest,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := contextstate.IsRef(s), isCanonicalRef(s); got != want {
			t.Fatalf("IsRef(%q) = %v, want %v", s, got, want)
		}
	})
}

func FuzzReassemble(f *testing.F) {
	f.Add([]byte("alpha-omega"), byte(3), byte(1))
	f.Add([]byte{}, byte(0), byte(0))
	f.Add([]byte("héllo"), byte(2), byte(0))
	f.Fuzz(func(t *testing.T, data []byte, cut, flip byte) {
		ref, err := contextstate.NewContentRef("fuzz-ns", "workspace-a", "fuzz-session", "subject-a", data)
		if err != nil {
			t.Fatalf("NewContentRef: %v", err)
		}
		if len(data) == 0 {
			record, err := contextstate.Reassemble(ref, contextstate.RetentionSession)
			if err != nil {
				t.Fatalf("Reassemble of empty input: %v", err)
			}
			if len(record.Data) != 0 {
				t.Fatalf("empty input reassembled to %d bytes", len(record.Data))
			}
			return
		}
		at := int(cut) % len(data)
		head, tail := data[:at], data[at:]
		record, err := contextstate.Reassemble(ref, contextstate.RetentionSession, head, tail)
		if err != nil {
			t.Fatalf("Reassemble of a partition of %d bytes at %d: %v", len(data), at, err)
		}
		if !bytes.Equal(record.Data, data) {
			t.Fatal("reassembled data differs from the whole")
		}
		mutHead := append([]byte(nil), head...)
		mutTail := append([]byte(nil), tail...)
		if len(mutHead) == 0 {
			mutTail[int(flip)%len(mutTail)] ^= 1
		} else {
			mutHead[int(flip)%len(mutHead)] ^= 1
		}
		if _, err := contextstate.Reassemble(ref, contextstate.RetentionSession, mutHead, mutTail); err == nil {
			t.Fatal("Reassemble accepted a flipped byte")
		}
	})
}
