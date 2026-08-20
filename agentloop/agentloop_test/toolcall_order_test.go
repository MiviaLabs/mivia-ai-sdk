package agentloop_test

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// orderRecordingTool appends its own name to a shared, mutex-guarded
// slice on every Run call, so a test can read back the call order.
type orderRecordingTool struct {
	name  string
	mu    *sync.Mutex
	order *[]string
}

func (t *orderRecordingTool) Name() string { return t.name }

func (t *orderRecordingTool) ParameterSchema() []byte { return []byte(`{}`) }

func (t *orderRecordingTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{}, nil
}

func (t *orderRecordingTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	t.mu.Lock()
	*t.order = append(*t.order, t.name)
	t.mu.Unlock()
	return tools.Out{Value: t.name}, nil
}

// TestRunToolCallsPreservesOrderOnDuplicateIndex pins toolcall.go's
// sort.Slice comparator: two provider.ToolCall values sharing one
// Index must keep the response's original order. sort.Slice's
// insertion sort on a 2-element slice swaps element 1 before element 0
// exactly when Less(1, 0) is true; the original "<" keeps equal-Index
// elements in place, while a mutant "<=" reverses them.
func TestRunToolCallsPreservesOrderOnDuplicateIndex(t *testing.T) {
	var mu sync.Mutex
	var order []string
	reg := tools.New()
	mustAdd(t, reg, &orderRecordingTool{name: "first", mu: &mu, order: &order})
	mustAdd(t, reg, &orderRecordingTool{name: "second", mu: &mu, order: &order})

	responses := []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "first", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Name: "second", Index: 0, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "done")},
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	want := []string{"first", "second"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("call order = %v, want %v: duplicate Index must keep the response's supplied order", order, want)
	}
}
