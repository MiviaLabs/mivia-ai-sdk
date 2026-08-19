package trace

import (
	"context"
	"sync"
	"time"
)

// Tracer issues sequential SpanID values through Start. Create one
// with New. Safe for concurrent Start calls; a sync.Mutex guards the
// counter. Every started span is retained, so Spans can report the
// whole tree after a run ends.
type Tracer struct {
	mu    sync.Mutex
	next  SpanID
	spans []*Span
}

// New creates a Tracer with no spans started. It has no error path.
func New() *Tracer {
	return &Tracer{}
}

// Start creates a Span named name, sets its ParentID from the span
// already in ctx, if any, and returns a ctx carrying the new span
// alongside the span itself. IDs start at one and never repeat, so
// the zero SpanID stays reserved for "no parent". This is the
// exported entry point; a caller never constructs a Span directly.
func (t *Tracer) Start(ctx context.Context, name string) (context.Context, *Span) {
	var parentID SpanID
	if parent, ok := SpanFrom(ctx); ok {
		parentID = parent.ID
	}
	t.mu.Lock()
	t.next++
	s := &Span{
		ID:       t.next,
		ParentID: parentID,
		Name:     name,
		Start:    time.Now(),
	}
	t.spans = append(t.spans, s)
	t.mu.Unlock()
	return withSpan(ctx, s), s
}

// Spans returns every span this Tracer started, in start order. The
// slice is a copy; the spans themselves stay shared, so Attributes
// and EndTime read live values. The result is empty and non-nil
// before any Start call.
func (t *Tracer) Spans() []*Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*Span, 0, len(t.spans))
	return append(out, t.spans...)
}
