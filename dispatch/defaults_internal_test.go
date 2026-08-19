package dispatch

import (
	"testing"
	"time"
)

// TestResolveReplayLease pins the zero-means-default boundary and its
// immediate neighbors, so a mutation to the == comparison in
// resolveReplayLease cannot swap which side gets the default.
func TestResolveReplayLease(t *testing.T) {
	cases := []struct {
		name       string
		configured time.Duration
		want       time.Duration
	}{
		{"zero selects the default", 0, DefaultReplayLease},
		{"exactly minReplayLease passes through unchanged", minReplayLease, minReplayLease},
		{"one nanosecond above zero passes through unchanged", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveReplayLease(tc.configured); got != tc.want {
				t.Fatalf("resolveReplayLease(%v) = %v, want %v", tc.configured, got, tc.want)
			}
		})
	}
}

// TestResolveReplayCapacity pins the zero-means-default boundary and
// its immediate neighbor, so a mutation to the == comparison in
// resolveReplayCapacity cannot swap which side gets the default.
func TestResolveReplayCapacity(t *testing.T) {
	cases := []struct {
		name       string
		configured int
		want       int
	}{
		{"zero selects the default", 0, DefaultReplayCapacity},
		{"one passes through unchanged", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveReplayCapacity(tc.configured); got != tc.want {
				t.Fatalf("resolveReplayCapacity(%v) = %v, want %v", tc.configured, got, tc.want)
			}
		})
	}
}

// TestDecodeErrorLineRejectsMalformedJSON pins decodeErrorLine's
// early return on unparseable input, distinct from its ok=false
// return for parseable-but-empty-Error input.
func TestDecodeErrorLineRejectsMalformedJSON(t *testing.T) {
	if _, ok := decodeErrorLine([]byte("not json")); ok {
		t.Fatalf("decodeErrorLine(malformed) ok = true, want false")
	}
	if _, ok := decodeErrorLine([]byte(`{"error":""}`)); ok {
		t.Fatalf("decodeErrorLine(empty error) ok = true, want false")
	}
	msg, ok := decodeErrorLine([]byte(`{"error":"boom"}`))
	if !ok || msg != "boom" {
		t.Fatalf("decodeErrorLine(valid) = (%q, %v), want (\"boom\", true)", msg, ok)
	}
}
