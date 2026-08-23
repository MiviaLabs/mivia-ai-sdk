package agentloop_test

// Fixtures and helpers for the steer-injector tests. The injector
// fixture and newInjectorLoop helper live here, separated from the
// test cases in steer_injector_test.go to keep both files under the
// structure gate's per-file line cap.

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// injectorFixture is the in-process injector the steer-injector tests
// share. It records every call (callCount), every returned payload
// (drained messages, in drain order), and exposes a settable next
// return (next). Concurrency: a single mutex guards every read and
// write; the injector runs on the loop goroutine, so the contention
// is between the test goroutine (writes) and the loop goroutine
// (reads). The lock-free fast path matters less than correctness
// here because the tests do not bombard the injector.
//
// The fixture's drain() supports two modes:
//   - "one-shot" (default): the next non-empty payload is delivered
//     once and cleared. Subsequent drains return nil. Matches the
//     "deliver once" semantics many tests want.
//   - "queue" (SetQueue): the next payloads are queued and
//     delivered one-at-a-time on each drain. Tests that need
//     different payloads on iteration 1 vs iteration 2 use the queue
//     mode and queue []provider.Message{iter1, iter2}.
type injectorFixture struct {
	mu      sync.Mutex
	calls   int
	drained [][]provider.Message
	next    []provider.Message // one-shot payload
	queue   [][]provider.Message
	stop    bool // when true, drainInjected returns nil (no-op injector)
}

func (f *injectorFixture) drain() []provider.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.stop {
		f.drained = append(f.drained, nil)
		return nil
	}
	if len(f.queue) > 0 {
		out := f.queue[0]
		f.queue = f.queue[1:]
		f.drained = append(f.drained, out)
		return out
	}
	if len(f.next) == 0 {
		f.drained = append(f.drained, nil)
		return nil
	}
	out := make([]provider.Message, len(f.next))
	copy(out, f.next)
	f.drained = append(f.drained, out)
	f.next = nil
	return out
}

func (f *injectorFixture) setNext(msgs []provider.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next = append([]provider.Message(nil), msgs...)
}

func (f *injectorFixture) setQueue(payloads [][]provider.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = make([][]provider.Message, len(payloads))
	for i, p := range payloads {
		f.queue[i] = append([]provider.Message(nil), p...)
	}
}

func (f *injectorFixture) setNoOp() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stop = true
}

func (f *injectorFixture) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *injectorFixture) drainedLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.drained)
}

// newInjectorLoop wires a minimal Loop over a scriptedCompleter with
// no tools, sized at maxIterations iterations. The steer is wired by
// the caller.
func newInjectorLoop(t *testing.T, c provider.Completer, maxIterations int) *agentloop.Loop {
	t.Helper()
	loop, err := agentloop.New(agentloop.Options{
		Completer:     c,
		Tools:         tools.New(),
		MaxIterations: maxIterations,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return loop
}

// injectorGateCompleter scripts a prefix of synchronous responses
// (indexes 0..mid-1), then a blocking midpoint at index mid (the
// "Trigger can fire mid-call" window), then a suffix of synchronous
// responses (indexes mid+1..). The midpoint closes entered and
// blocks on ctx.Done(); Trigger cancels the derived context, the
// steered-stop downgrade runs, the injector delivers a payload, and
// the next call (idx == mid+1) returns the next scripted response.
// Responses[mid] is therefore skipped on purpose: the midpoint
// never returns a response, only an error.
type injectorGateCompleter struct {
	mu        sync.Mutex
	calls     int
	responses []provider.Response
	mid       int
	entered   chan struct{}
	once      sync.Once
}

func newInjectorGateCompleter(responses []provider.Response, mid int) *injectorGateCompleter {
	return &injectorGateCompleter{
		responses: responses,
		mid:       mid,
		entered:   make(chan struct{}),
	}
}

func (c *injectorGateCompleter) Name() string { return "injector-gate" }

func (c *injectorGateCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.mu.Lock()
	idx := c.calls
	c.calls++
	c.mu.Unlock()
	if idx == c.mid {
		if c.entered != nil {
			c.once.Do(func() { close(c.entered) })
		}
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	return provider.Response{}, ctx.Err()
}

func (c *injectorGateCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

func (c *injectorGateCompleter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}
