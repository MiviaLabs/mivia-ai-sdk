package trace

import "context"

// spanContextKey is the unexported key withSpan stores a *Span under.
type spanContextKey struct{}

// withSpan stores s in ctx under the span context key. Tracer.Start
// calls this for each new span.
func withSpan(ctx context.Context, s *Span) context.Context {
	return context.WithValue(ctx, spanContextKey{}, s)
}

// SpanFrom reads the *Span Tracer.Start injected into ctx. The
// boolean is false when ctx carries no span, matching flow's
// LoopStateFrom and FailureFrom shape.
func SpanFrom(ctx context.Context) (*Span, bool) {
	s, ok := ctx.Value(spanContextKey{}).(*Span)
	return s, ok
}
