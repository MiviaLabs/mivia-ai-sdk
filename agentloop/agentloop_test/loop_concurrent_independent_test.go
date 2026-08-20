package agentloop_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// reflectingToolCompleter answers Chat by reading req.Messages[0]'s
// Content, the caller's original starting message, and echoing it
// back: as a tool call's Arguments on the one-message first
// iteration, then as the final assistant reply's Content once the
// tool result has been appended. It reads the origin message instead
// of the request's last message, so its answer stays keyed to what
// each individual Run call started with, not to what the loop appended
// meanwhile; that keeps it correct under concurrent callers sharing
// one *Loop.
type reflectingToolCompleter struct{ tool string }

func (r reflectingToolCompleter) Name() string { return "reflecting" }

func (r reflectingToolCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	origin := req.Messages[0].Content
	if len(req.Messages) == 1 {
		args := []byte(fmt.Sprintf(`{"echo":%q}`, origin))
		return toolCallResponse(provider.ToolCall{ID: "call-" + origin, Name: r.tool, Arguments: args}), nil
	}
	return provider.Response{Message: provider.Message{Role: provider.RoleAssistant, Content: "final:" + origin}}, nil
}

func (r reflectingToolCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("reflectingToolCompleter: ChatStream not supported")
}

// TestRunConcurrentIndependentCallsDoNotBleedState proves that two
// fully independent Run calls on one shared *Loop, each carrying its
// own distinct starting message and its own distinct tool-call
// argument, produce results that match only their own input: neither
// call's history, tool result, or final message ever carries the
// other call's content. Loop holds no mutable per-run field (l.defs
// and l.schemas are built once at New and only read after), so a
// content mismatch here would mean a caller-visible regression, not a
// data race the race detector alone would catch.
func TestRunConcurrentIndependentCallsDoNotBleedState(t *testing.T) {
	reg := tools.New()
	tool := &schemaEchoTool{name: "reflect", schema: []byte(`{"type":"object"}`), result: "tool-result"}
	mustAdd(t, reg, tool)
	loop, err := agentloop.New(agentloop.Options{
		Completer: reflectingToolCompleter{tool: "reflect"}, Tools: reg, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]agentloop.Result, n)
	for g := 0; g < n; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			msgs := []provider.Message{textMessage(provider.RoleUser, fmt.Sprintf("caller-%d", g))}
			results[g], errs[g] = loop.Run(context.Background(), msgs)
		}(g)
	}
	wg.Wait()

	for g := 0; g < n; g++ {
		want := fmt.Sprintf("caller-%d", g)
		if errs[g] != nil {
			t.Fatalf("goroutine %d: Run() error = %v, want nil", g, errs[g])
		}
		res := results[g]
		if res.Final.Content != "final:"+want {
			t.Fatalf("goroutine %d: Final.Content = %q, want %q: another goroutine's content leaked in", g, res.Final.Content, "final:"+want)
		}
		if len(res.History) != 4 {
			t.Fatalf("goroutine %d: History len = %d, want 4 (user, assistant tool-call, tool result, final assistant)", g, len(res.History))
		}
		if res.History[0].Content != want {
			t.Fatalf("goroutine %d: History[0].Content = %q, want %q", g, res.History[0].Content, want)
		}
		if res.History[2].Role != provider.RoleTool || res.History[2].ToolCallID != "call-"+want {
			t.Fatalf("goroutine %d: History[2] = %+v, want the tool result for call-%s", g, res.History[2], want)
		}
	}
}
