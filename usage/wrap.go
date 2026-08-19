package usage

import (
	"context"
	"fmt"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// WrapCompleter returns a provider.Completer that records every
// completed Chat turn's usage under sessionID in a. The wrapper
// keeps the inner Completer's name and messages. A blank sessionID,
// a nil Accumulator, or a nil Completer fails construction, so
// counts are never silently dropped. A turn that errors records
// nothing; a streamed turn records nothing, matching ChatStream's
// passthrough.
func WrapCompleter(sessionID string, a *Accumulator, c provider.Completer) (provider.Completer, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("usage: wrap: sessionID %q: %w", sessionID, ErrBlankSessionID)
	}
	if a == nil {
		return nil, fmt.Errorf("usage: wrap: %w", ErrNilAccumulator)
	}
	if c == nil {
		return nil, fmt.Errorf("usage: wrap: %w", ErrNilCompleter)
	}
	return &recordingCompleter{sessionID: sessionID, acc: a, inner: c}, nil
}

// recordingCompleter is WrapCompleter's adapter.
type recordingCompleter struct {
	sessionID string
	acc       *Accumulator
	inner     provider.Completer
}

// Name returns the inner Completer's name.
func (r *recordingCompleter) Name() string { return r.inner.Name() }

// Chat runs the inner Chat and records the response's usage.
func (r *recordingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
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
func (r *recordingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return r.inner.ChatStream(ctx, req)
}
