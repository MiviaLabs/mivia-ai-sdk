package provider_test

import (
	"context"
	"errors"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// errFakeChat is the fake Completer's fixed Chat failure sentinel.
var errFakeChat = errors.New("fakeCompleter: chat failed")

// errFakeStream is the fake Completer's fixed ChatStream failure
// sentinel.
var errFakeStream = errors.New("fakeCompleter: stream failed")

// fakeCompleter is a Completer test double. It never performs I/O; it
// returns and records exactly what its fields configure.
type fakeCompleter struct {
	name string

	chatResp provider.Response
	chatErr  error

	streamChunks []provider.Chunk
	streamErr    error
	neverClose   bool

	chatCalled   bool
	streamCalled bool
	lastRequest  provider.Request
}

func (f *fakeCompleter) Name() string { return f.name }

func (f *fakeCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	f.chatCalled = true
	f.lastRequest = req
	if f.chatErr != nil {
		return provider.Response{}, f.chatErr
	}
	return f.chatResp, nil
}

func (f *fakeCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	f.streamCalled = true
	f.lastRequest = req
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	ch := make(chan provider.Chunk)
	go func() {
		for _, c := range f.streamChunks {
			select {
			case ch <- c:
			case <-ctx.Done():
				return
			}
		}
		if f.neverClose {
			<-ctx.Done()
			return
		}
		close(ch)
	}()
	return ch, nil
}

// capableFake implements ContextAccountant and ReasoningPolicy, on
// top of the required Completer methods.
type capableFake struct {
	fakeCompleter
	contextWindow   int
	reasoningEffort string
}

func (c *capableFake) ContextWindow() int      { return c.contextWindow }
func (c *capableFake) ReasoningEffort() string { return c.reasoningEffort }

// tokenEstimatingFake implements TokenEstimator only, on top of the
// required Completer methods.
type tokenEstimatingFake struct {
	fakeCompleter
	tokens int
	err    error
}

func (t *tokenEstimatingFake) EstimateTokens(req provider.Request) (int, error) {
	if t.err != nil {
		return 0, t.err
	}
	return t.tokens, nil
}

// capableTokenEstimatingFake implements ContextAccountant,
// ReasoningPolicy, and TokenEstimator together, for the composition
// test between EstimateTokens and ContextWindow.
type capableTokenEstimatingFake struct {
	capableFake
	tokens int
	err    error
}

func (c *capableTokenEstimatingFake) EstimateTokens(req provider.Request) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return c.tokens, nil
}
