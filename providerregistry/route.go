package providerregistry

import (
	"context"
	"errors"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// Retryable is the fallback predicate Route consults after each
// failed attempt. A nil Retryable falls through on every error,
// mirroring flow.RetryPolicy.Retryable's nil rule, so the two
// caller-supplied predicates in this SDK read the same way.
type Retryable func(error) bool

// Route tries each name in order, in sequence, calling
// provider.RunTurn for the Completer Get resolves. Route returns the
// first successful Response at once. On a RunTurn error, Route checks
// retryable: nil or true moves to the next name; false stops the loop
// and returns that error unwrapped. Route rejects an empty order with
// ErrEmptyOrder before any call. Route rejects a name Get cannot
// resolve with ErrUnknownName naming the missing entry, and stops at
// once rather than skipping it. When every name was tried and every
// attempt failed the retryable check, Route returns ErrAllFailed
// wrapping the last attempt's error. Route checks ctx.Err() before
// each attempt after the first; a canceled ctx stops the loop and
// returns ctx.Err(). Route walks order once, in the caller's
// sequence; it never repeats or skips a name on its own.
func (r *Registry) Route(ctx context.Context, req provider.Request, order []string, retryable Retryable) (provider.Response, error) {
	if len(order) == 0 {
		return provider.Response{}, ErrEmptyOrder
	}
	var lastErr error
	for i, name := range order {
		if i > 0 {
			if err := ctx.Err(); err != nil {
				return provider.Response{}, err
			}
		}
		c, ok := r.Get(name)
		if !ok {
			return provider.Response{}, fmt.Errorf("%w: %s", ErrUnknownName, name)
		}
		resp, err := provider.RunTurn(ctx, c, req)
		if err == nil {
			return resp, nil
		}
		if retryable != nil && !retryable(err) {
			return provider.Response{}, err
		}
		lastErr = err
	}
	return provider.Response{}, newAllFailedError(lastErr)
}

// allFailedError is Route's ErrAllFailed wrap. Error text and
// errors.Is semantics come from the fmt.Errorf %w wrap of ErrAllFailed
// and the last attempt's error. Unwrap yields the last attempt's error
// directly, which fmt.Errorf's multi-%w form cannot express: its
// errors.Unwrap result is nil, not the wrapped errors.
type allFailedError struct {
	wrap error
	last error
}

// newAllFailedError builds Route's ErrAllFailed wrap around last, the
// final attempt's error.
func newAllFailedError(last error) *allFailedError {
	return &allFailedError{
		wrap: fmt.Errorf("%w: %w", ErrAllFailed, last),
		last: last,
	}
}

// Error returns the fmt.Errorf wrap's message.
func (e *allFailedError) Error() string { return e.wrap.Error() }

// Is reports whether target matches the wrap's chain.
func (e *allFailedError) Is(target error) bool { return errors.Is(e.wrap, target) }

// Unwrap returns the last attempt's error.
func (e *allFailedError) Unwrap() error { return e.last }
