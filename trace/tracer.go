package trace

import (
	"context"
	"sync"
	"time"
)

// Tracer issues sequential SpanID values through Start. Create one
// with New. Safe for concurrent Start calls; a sync.Mutex guards the
// counter.
type Tracer struct {
	mu   sync.Mutex
	next SpanID
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
	id := t.next
	t.mu.Unlock()
	s := &Span{
		ID:       id,
		ParentID: parentID,
		Name:     name,
		Start:    time.Now(),
	}
	return withSpan(ctx, s), s
}
