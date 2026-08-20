package agentloop_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// blockingCompleter answers a call already scripted from
// responses/errs, scriptedCompleter-style; every call past both
// blocks on ctx.Done() and returns ctx.Err(). entered, when non-nil,
// is closed the first time a blocking call starts, letting a test
// synchronize a Trigger call against the in-flight Chat call instead
// of sleeping.
type blockingCompleter struct {
	mu        sync.Mutex
	calls     int
	responses []provider.Response
	errs      []error
	entered   chan struct{}
	once      sync.Once
}

func (c *blockingCompleter) Name() string { return "blocking" }

func (c *blockingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	idx := c.calls
	c.calls++
	c.mu.Unlock()
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	if idx < len(c.errs) && c.errs[idx] != nil {
		return provider.Response{}, c.errs[idx]
	}
	if c.entered != nil {
		c.once.Do(func() { close(c.entered) })
	}
	<-ctx.Done()
	return provider.Response{}, ctx.Err()
}

func (c *blockingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("blockingCompleter: ChatStream not supported")
}

func (c *blockingCompleter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// newSteerLoop wires a minimal Loop over completer with no tools.
func newSteerLoop(t *testing.T, completer provider.Completer, maxIterations int) *agentloop.Loop {
	t.Helper()
	loop, err := agentloop.New(agentloop.Options{
		Completer:     completer,
		Tools:         tools.New(),
		MaxIterations: maxIterations,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop
}

// gateTool records each call's argument string and, on the call whose
// zero-based index equals gateIndex, closes entered and blocks on
// release before returning: a test uses this to fire Trigger while
// this call is in flight and observe the calls after it still run.
type gateTool struct {
	mu        sync.Mutex
	calls     []string
	gateIndex int
	entered   chan struct{}
	release   chan struct{}
}

func (g *gateTool) Name() string { return "seq" }

func (g *gateTool) ParameterSchema() []byte { return []byte(`{"type":"object"}`) }

func (g *gateTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

func (g *gateTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	g.mu.Lock()
	idx := len(g.calls)
	g.calls = append(g.calls, in.Value.(string))
	g.mu.Unlock()
	if idx == g.gateIndex {
		close(g.entered)
		<-g.release
	}
	return tools.Out{Value: "ok"}, nil
}

func (g *gateTool) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

// recoveryBlockCompleter rejects call 0 with provider.ErrPromptTooLong,
// blocks on call 1 (the recovery retry) until release is closed, then
// answers with recovered, and blocks on ctx.Done() from call 2 on.
type recoveryBlockCompleter struct {
	mu        sync.Mutex
	calls     int
	recovered provider.Response
	entered   chan struct{}
	release   chan struct{}
}

func (c *recoveryBlockCompleter) Name() string { return "recovery-block" }

func (c *recoveryBlockCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	idx := c.calls
	c.calls++
	c.mu.Unlock()
	switch idx {
	case 0:
		return provider.Response{}, provider.ErrPromptTooLong
	case 1:
		close(c.entered)
		<-c.release
		return c.recovered, nil
	default:
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}
}

func (c *recoveryBlockCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("recoveryBlockCompleter: ChatStream not supported")
}
