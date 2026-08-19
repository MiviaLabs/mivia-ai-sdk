package spool

import "context"

// principalContextKey is the unexported key WithPrincipal stores a
// principal under, following flow.LoopStateFrom's precedent: inject
// before the call, read inside.
type principalContextKey struct{}

// WithPrincipal returns a context carrying principal for a later
// SpoolTool call to read.
func WithPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFrom reads the principal WithPrincipal attached to ctx. The
// second return is false when no principal was attached.
func PrincipalFrom(ctx context.Context) (string, bool) {
	p, ok := ctx.Value(principalContextKey{}).(string)
	return p, ok
}
