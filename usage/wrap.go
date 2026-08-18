package usage

import (
	"context"
	"fmt"
	"strings"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// WrapCompleter returns a provider.Completer that records every
// completed turn's usage under sessionID in a. The wrapper keeps the
// inner Completer's name, messages, and streaming behavior; it only
// adds the Record call after a turn completes. A blank sessionID
// fails construction with ErrBlankSessionID, wrapped, so counts are
// never silently dropped. A turn that errors records nothing.
func WrapCompleter(sessionID string, a *Accumulator, c provider.Completer) (provider.Completer, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("usage: wrap: sessionID %q: %w", sessionID, ErrBlankSessionID)
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
	if recErr := r.acc.Record(r.sessionID, resp.Usage); recErr != nil {
		return resp, recErr
	}
	return resp, nil
}

// ChatStream runs the inner ChatStream, aggregates the chunks through
// provider.RunTurn's own contract, and records nothing: a streamed
// turn's usage lands when the caller completes it through Chat's
// path. Callers that need streamed totals wrap the aggregated turn
// themselves.
func (r *recordingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return r.inner.ChatStream(ctx, req)
}
