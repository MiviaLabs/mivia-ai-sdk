package contextsummary_test

import (
	"context"
	"sync"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
)

// scriptCompleter is a scripted provider.Completer: it returns fixed
// replies in order, counts calls, records the last request, and can
// wait for ctx to finish instead of returning.
type scriptCompleter struct {
	mu       sync.Mutex
	replies  []string
	err      error
	calls    int
	lastReq  provider.Request
	deadline time.Time
	hasDL    bool
	waitCtx  bool
}

func (f *scriptCompleter) Name() string { return "scripted" }

func (f *scriptCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	f.mu.Lock()
	f.calls++
	f.lastReq = req
	f.deadline, f.hasDL = ctx.Deadline()
	n := f.calls - 1
	wait := f.waitCtx
	f.mu.Unlock()
	if wait {
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}
	if f.err != nil {
		return provider.Response{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	content := ""
	if n < len(f.replies) {
		content = f.replies[n]
	}
	return provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Content: content},
	}, nil
}

func (f *scriptCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, provider.ErrStreamClosedEarly
}

// stats reads the call count and last request under the lock.
func (f *scriptCompleter) stats() (int, provider.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.lastReq
}

// validReply is one strict-schema reply every field set.
const validReply = `{"Objective":"Ship the release","State":"Two tests fail","Decisions":["Use SQLite"],"OpenWork":["Fix tests"],"Risks":["Deadline slips"]}`
