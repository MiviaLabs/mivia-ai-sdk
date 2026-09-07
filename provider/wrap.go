package provider

import (
	"context"
	"fmt"
	"strings"
)

// WrapCompleter returns a Completer that records every
// completed Chat turn's usage under sessionID in a. The wrapper
// keeps the inner Completer's name and messages. A blank sessionID,
// a nil Accumulator, or a nil Completer fails construction, so
// counts are never silently dropped. A turn that errors records
// nothing; a streamed turn records nothing, matching ChatStream's
// passthrough.
func WrapCompleter(sessionID string, a *Accumulator, c Completer) (Completer, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("provider: wrap: sessionID %q: %w", sessionID, ErrBlankSessionID)
	}
	if a == nil {
		return nil, fmt.Errorf("provider: wrap: %w: %s", ErrInvalidOptions, "Accumulator: must not be nil")
	}
	if c == nil {
		return nil, fmt.Errorf("provider: wrap: %w: %s", ErrInvalidOptions, "Completer: must not be nil")
	}
	return &recordingCompleter{sessionID: sessionID, acc: a, inner: c}, nil
}

// recordingCompleter is WrapCompleter's adapter.
type recordingCompleter struct {
	sessionID string
	acc       *Accumulator
	inner     Completer
}

// Name returns the inner Completer's name.
func (r *recordingCompleter) Name() string { return r.inner.Name() }

// Chat runs the inner Chat and records the response's usage.
func (r *recordingCompleter) Chat(ctx context.Context, req Request) (Response, error) {
	resp, err := r.inner.Chat(ctx, req)
	if err != nil {
		return resp, err
	}
	// The constructor validated the sessionID, so Record cannot fail.
	_ = r.acc.Record(r.sessionID, resp.Usage)
	return resp, nil
}

// ChatStream passes the inner ChatStream through unchanged. A
// streamed turn records nothing; a caller that needs streamed totals
// wraps the aggregated turn itself.
func (r *recordingCompleter) ChatStream(ctx context.Context, req Request) (<-chan Chunk, error) {
	return r.inner.ChatStream(ctx, req)
}
