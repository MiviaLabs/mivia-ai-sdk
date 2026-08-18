package a2aclient

import (
	"context"
	"crypto/ed25519"
	"sync"
	"sync/atomic"

	"github.com/MiviaLabs/mivia-ai-sdk/a2a"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
)

// testBaseURL is the placeholder base URL passed to newFromTransport;
// stubTransport never dials it.
const testBaseURL = "https://agent.example.invalid"

// stubTransport is a recorded, in-memory transcript standing in for a
// live a2a-go transport. It never opens a network connection, and its
// exported fields script the outcome of each Send/State/Result call.
type stubTransport struct {
	mu sync.Mutex

	sendErr error
	taskID  string

	// states is consumed one entry per State call; the last entry
	// repeats once exhausted, so a caller can poll past the recorded
	// script and still observe a stable terminal state.
	states     []State
	stateErr   error
	stateCalls atomic.Int64

	result    a2a.Mapped
	resultErr error

	// ignoreCtx makes Send/State/Result skip their own ctx.Err() check,
	// so a test using it proves Client enforces the ctx check itself
	// rather than relying on the transport to do it.
	ignoreCtx bool

	closed     atomic.Bool
	closeCalls atomic.Int64
	closeErr   error
}

var _ transport = (*stubTransport)(nil)

func (s *stubTransport) Send(ctx context.Context, mapped a2a.Mapped) (string, error) {
	if !s.ignoreCtx {
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	if s.sendErr != nil {
		return "", s.sendErr
	}
	return s.taskID, nil
}

func (s *stubTransport) State(ctx context.Context, taskID string) (State, error) {
	if !s.ignoreCtx {
		if err := ctx.Err(); err != nil {
			return StateUnspecified, err
		}
	}
	if s.stateErr != nil {
		return StateUnspecified, s.stateErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.states) == 0 {
		return StateUnspecified, nil
	}
	idx := int(s.stateCalls.Add(1)) - 1
	if idx >= len(s.states) {
		idx = len(s.states) - 1
	}
	return s.states[idx], nil
}

func (s *stubTransport) Result(ctx context.Context, taskID string) (a2a.Mapped, error) {
	if !s.ignoreCtx {
		if err := ctx.Err(); err != nil {
			return a2a.Mapped{}, err
		}
	}
	if s.resultErr != nil {
		return a2a.Mapped{}, s.resultErr
	}
	return s.result, nil
}

func (s *stubTransport) Close() error {
	s.closeCalls.Add(1)
	s.closed.Store(true)
	return s.closeErr
}

// signedMessage returns a valid, signed envelope.Message for tests
// that need one.
func signedMessage(t interface{ Fatalf(string, ...any) }) envelope.Message {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	msg := envelope.Message{
		Version:    envelope.Version,
		ID:         "msg-1",
		ThreadID:   "thread-1",
		Intent:     envelope.IntentAssert,
		Epistemic:  envelope.EpistemicAssumed,
		Confidence: 0.5,
		Payload:    "hello remote agent",
	}
	signed, err := envelope.Sign(key, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

// mappedResult maps a signed message to a2a.Mapped, the wire shape
// stubTransport.result carries.
func mappedResult(t interface{ Fatalf(string, ...any) }, msg envelope.Message) a2a.Mapped {
	mapped, err := a2a.ToPart(msg)
	if err != nil {
		t.Fatalf("map result: %v", err)
	}
	return mapped
}
