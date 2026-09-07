package anthropic

import (
	"net/http"
	"testing"
	"time"
)

// TestParseRetryAfterSeconds pins the RFC 9110 delay-seconds form: a
// non-negative integer count of seconds, nothing else.
func TestParseRetryAfterSeconds(t *testing.T) {
	if got := parseRetryAfter("5"); got != 5*time.Second {
		t.Errorf("parseRetryAfter(%q) = %v, want 5s", "5", got)
	}
	if got := parseRetryAfter("0"); got != 0 {
		t.Errorf("parseRetryAfter(%q) = %v, want 0", "0", got)
	}
}

// TestParseRetryAfterRejectsUnitSuffix pins the fix for a value like
// "1m": the old implementation appended "s" and called
// time.ParseDuration, so "1m" parsed as "1ms", one millisecond
// instead of being rejected. RFC 9110's delay-seconds form is digits
// only; a unit suffix must fall through to the HTTP-date parse, which
// also rejects it, yielding no hint.
func TestParseRetryAfterRejectsUnitSuffix(t *testing.T) {
	if got := parseRetryAfter("1m"); got != 0 {
		t.Errorf("parseRetryAfter(%q) = %v, want 0 (not 1ms)", "1m", got)
	}
	if got := parseRetryAfter("1h"); got != 0 {
		t.Errorf("parseRetryAfter(%q) = %v, want 0", "1h", got)
	}
}

// TestParseRetryAfterClampsHugeValue pins the overflow guard: a huge
// server-supplied second count clamps to maxRetryAfterSeconds instead
// of reaching the int64-nanosecond multiply unbounded.
func TestParseRetryAfterClampsHugeValue(t *testing.T) {
	got := parseRetryAfter("99999999999999999")
	want := time.Duration(maxRetryAfterSeconds) * time.Second
	if got != want {
		t.Errorf("parseRetryAfter(huge) = %v, want %v (clamped)", got, want)
	}
	if got <= 0 {
		t.Fatalf("parseRetryAfter(huge) = %v, want a positive clamped duration, not an overflow", got)
	}
}

// TestParseRetryAfterHTTPDate pins the second accepted form.
func TestParseRetryAfterHTTPDate(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC()
	got := parseRetryAfter(future.Format(http.TimeFormat))
	if got <= 0 || got > 30*time.Second {
		t.Errorf("parseRetryAfter(HTTP-date) = %v, want in (0, 30s]", got)
	}
}

// TestParseRetryAfterRejectsGarbage pins the no-hint fallback.
func TestParseRetryAfterRejectsGarbage(t *testing.T) {
	if got := parseRetryAfter("not-a-value"); got != 0 {
		t.Errorf("parseRetryAfter(garbage) = %v, want 0", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("parseRetryAfter(empty) = %v, want 0", got)
	}
	if got := parseRetryAfter("-5"); got != 0 {
		t.Errorf("parseRetryAfter(%q) = %v, want 0 (negative rejected)", "-5", got)
	}
}
