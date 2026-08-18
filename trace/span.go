// Package trace gives a caller a structured trace of a multi-step
// run: Span records one named operation, Tracer issues spans and
// links them through ctx. A leaf package: no internal imports, no
// exporter, no sampling policy.
package trace

import (
	"sync"
	"time"
)

// SpanID identifies one Span within its Tracer. Allocation-free and
// comparable. The zero value means "no parent".
type SpanID uint64

// Span is one named operation with a start time, an end time, a
// parent link, and caller-set string attributes. Tracer.Start is the
// constructor; a caller never builds one directly. Safe for
// concurrent End and SetAttribute calls on one shared *Span, so one
// parent span may serve several child goroutines.
type Span struct {
	// ID identifies this span within its Tracer; never zero.
	ID SpanID
	// ParentID is the parent span's ID; the zero SpanID on a root
	// span.
	ParentID SpanID
	// Name is the operation name Start received.
	Name string
	// Start is the time Tracer.Start created the span.
	Start time.Time

	mu    sync.Mutex
	end   time.Time
	attrs []attribute
}

// attribute is one recorded key-value pair on a Span. A slice of
// these backs the attributes: the first SetAttribute call costs one
// allocation, where a map would cost two (header plus first bucket).
type attribute struct {
	key   string
	value string
}

// End records the current time as the span's end time. A second call
// is a no-op; only the first call's time sticks.
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.end.IsZero() {
		return
	}
	s.end = time.Now()
}

// EndTime returns the recorded end time. The result is the zero
// time.Time before End runs.
func (s *Span) EndTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.end
}

// Duration returns EndTime minus Start. The result is zero before End
// runs and non-negative after.
func (s *Span) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.end.IsZero() {
		return 0
	}
	return s.end.Sub(s.Start)
}

// SetAttribute records one key-value pair. A later call with the same
// key overwrites the earlier value. The backing store allocates on
// the first call only. Works on a live or ended span.
func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.attrs {
		if s.attrs[i].key == key {
			s.attrs[i].value = value
			return
		}
	}
	s.attrs = append(s.attrs, attribute{key: key, value: value})
}

// Attributes returns a copy of the attribute map, safe to read and
// mutate without the span's lock. The result is empty and non-nil
// when no attribute was ever set.
func (s *Span) Attributes() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.attrs))
	for _, a := range s.attrs {
		out[a.key] = a.value
	}
	return out
}
