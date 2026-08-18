package trace_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// newSpan starts one root span on a bare ctx. Span tests need no
// parent linkage, so one span per case is enough.
func newSpan(t *testing.T) *trace.Span {
	t.Helper()
	_, s := trace.New().Start(context.Background(), "span-under-test")
	return s
}

// TestDurationIsEndTimeMinusStart pins Duration's definition: the
// recorded end time minus the start time, exactly.
func TestDurationIsEndTimeMinusStart(t *testing.T) {
	s := newSpan(t)
	s.End()
	want := s.EndTime().Sub(s.Start)
	if got := s.Duration(); got != want {
		t.Fatalf("Duration() = %v, want %v", got, want)
	}
}

// TestDurationZeroBeforeEnd pins the zero value before End runs: an
// ended span reports elapsed time, a live one reports zero.
func TestDurationZeroBeforeEnd(t *testing.T) {
	s := newSpan(t)
	if got := s.Duration(); got != 0 {
		t.Fatalf("Duration() before End = %v, want 0", got)
	}
}

// TestSecondEndKeepsFirstEndTime pins End's no-op rule. The spin
// waits until the clock reads past the first end time, so a second
// End that overwrote would record a strictly later time and fail the
// equality.
func TestSecondEndKeepsFirstEndTime(t *testing.T) {
	s := newSpan(t)
	s.End()
	first := s.EndTime()
	for time.Now().Equal(first) {
		// Spin: the clock has not advanced past the first end
		// reading yet.
	}
	s.End()
	if got := s.EndTime(); !got.Equal(first) {
		t.Fatalf("EndTime() after second End = %v, want %v", got, first)
	}
}

// TestSetAttributeOverwritesKey pins SetAttribute's overwrite rule for
// one key set twice.
func TestSetAttributeOverwritesKey(t *testing.T) {
	s := newSpan(t)
	s.SetAttribute("stage", "first")
	s.SetAttribute("stage", "second")
	got := s.Attributes()
	if got["stage"] != "second" {
		t.Fatalf("Attributes()[%q] = %q, want %q", "stage", got["stage"], "second")
	}
}

// TestSetAttributeOverwriteAmongManyKeys pins the scan: an overwrite
// must find its key among several, not just the first slot.
func TestSetAttributeOverwriteAmongManyKeys(t *testing.T) {
	s := newSpan(t)
	s.SetAttribute("first", "1")
	s.SetAttribute("second", "2")
	s.SetAttribute("third", "3")
	s.SetAttribute("second", "2b")
	got := s.Attributes()
	if len(got) != 3 {
		t.Fatalf("len(Attributes()) = %d, want 3", len(got))
	}
	if got["second"] != "2b" {
		t.Fatalf("Attributes()[%q] = %q, want %q", "second", got["second"], "2b")
	}
}

// TestSetAttributeAfterEndStillLands pins the live-or-ended rule from
// SetAttribute's contract: a set after End records, and a set from
// before End survives.
func TestSetAttributeAfterEndStillLands(t *testing.T) {
	s := newSpan(t)
	s.SetAttribute("before", "kept")
	s.End()
	s.SetAttribute("after", "recorded")
	got := s.Attributes()
	if len(got) != 2 {
		t.Fatalf("len(Attributes()) = %d, want 2", len(got))
	}
	if got["after"] != "recorded" {
		t.Fatalf("Attributes()[%q] = %q, want %q", "after", got["after"], "recorded")
	}
	if got["before"] != "kept" {
		t.Fatalf("Attributes()[%q] = %q, want %q", "before", got["before"], "kept")
	}
}

// TestAttributesLifecycle covers the reader across the span's life:
// empty and non-nil before any set, one entry after one set.
func TestAttributesLifecycle(t *testing.T) {
	s := newSpan(t)
	empty := s.Attributes()
	if empty == nil {
		t.Fatal("Attributes() before any set = nil, want non-nil")
	}
	if len(empty) != 0 {
		t.Fatalf("len(Attributes()) before any set = %d, want 0", len(empty))
	}
	s.SetAttribute("key", "value")
	got := s.Attributes()
	if len(got) != 1 || got["key"] != "value" {
		t.Fatalf("Attributes() after one set = %v, want map with key=value", got)
	}
}

// TestAttributesReturnsCopy pins the copy rule: mutating the returned
// map never reaches the span, and a later read reports the span's own
// values unchanged.
func TestAttributesReturnsCopy(t *testing.T) {
	s := newSpan(t)
	s.SetAttribute("key", "value")
	got := s.Attributes()
	got["key"] = "mutated"
	got["added"] = "mutated"
	again := s.Attributes()
	if len(again) != 1 {
		t.Fatalf("len(Attributes()) after caller mutation = %d, want 1", len(again))
	}
	if again["key"] != "value" {
		t.Fatalf("Attributes()[%q] after caller mutation = %q, want %q",
			"key", again["key"], "value")
	}
}
