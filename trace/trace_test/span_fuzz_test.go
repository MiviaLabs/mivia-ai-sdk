package trace_test

import (
	"testing"
)

// FuzzSetAttribute feeds random key-value pairs into one span. The
// invariants: the last write per key wins, and the entry count never
// exceeds the count of distinct keys.
func FuzzSetAttribute(f *testing.F) {
	f.Add("stage", "run")
	f.Add("", "")
	f.Add("dup", "first")
	f.Add("stage", "")
	f.Fuzz(func(t *testing.T, key, value string) {
		s := newSpan(t)
		s.SetAttribute(key, value)
		last := value + "-last"
		s.SetAttribute(key, last)
		got := s.Attributes()
		if got[key] != last {
			t.Fatalf("last write for key %q = %q, want %q", key, got[key], last)
		}
		if len(got) != 1 {
			t.Fatalf("len(Attributes()) = %d, want 1", len(got))
		}
	})
}
