package agentloop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

var errBatchAudit = errors.New("agentloop: audit rejected")

// batchScriptedCompleter replays scripted responses in order.
type batchScriptedCompleter struct {
	mu        sync.Mutex
	responses []provider.Response
	errs      []error
	calls     int
}

func (s *batchScriptedCompleter) Name() string { return "scripted" }

func (s *batchScriptedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.calls
	s.calls++
	if idx < len(s.errs) && s.errs[idx] != nil {
		return provider.Response{}, s.errs[idx]
	}
	if idx >= len(s.responses) {
		return provider.Response{}, fmt.Errorf("batchScriptedCompleter: no response scripted for call %d", idx)
	}
	return s.responses[idx], nil
}

func (s *batchScriptedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("batchScriptedCompleter: ChatStream not supported")
}

func textMessage(role provider.Role, content string) provider.Message {
	return provider.Message{Role: role, Content: content}
}

func assistantToolCallMessage(calls ...provider.ToolCall) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: calls}
}

func toolCallResponse(calls ...provider.ToolCall) provider.Response {
	return provider.Response{Message: assistantToolCallMessage(calls...), ToolCalls: calls}
}

func mustAdd(t *testing.T, reg *tools.Registry, tool tools.Tool) {
	t.Helper()
	if err := reg.Add(tool); err != nil {
		t.Fatalf("Add(%s) error = %v, want nil", tool.Name(), err)
	}
}

// batchEchoTool is a minimal schema tool: it echoes its input as the
// result, or fails, per its fields.
type batchEchoTool struct {
	name   string
	schema []byte
	result any
	runErr error
}

func (t *batchEchoTool) Name() string            { return t.name }
func (t *batchEchoTool) ParameterSchema() []byte { return t.schema }

func (t *batchEchoTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

func (t *batchEchoTool) run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if t.runErr != nil {
		return tools.Out{}, t.runErr
	}
	return tools.Out{Value: t.result}, nil
}

// orderObservingTool records the BatchOrder each Run observed on its ctx.
type orderObservingTool struct {
	batchEchoTool
	mu     sync.Mutex
	orders []*BatchOrder
}

func (t *orderObservingTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if order, ok := BatchOrderFromContext(ctx); ok {
		t.mu.Lock()
		t.orders = append(t.orders, order)
		t.mu.Unlock()
	}
	return t.batchEchoTool.run(ctx, in)
}

func (t *orderObservingTool) observed() []*BatchOrder {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*BatchOrder(nil), t.orders...)
}

// runBatchOrderTurn drives one turn with calls [0 ok, 1 unknown-name
// reject, 2 ok, 3 duplicate-of-0] and returns the BatchOrder the tools
// observed. Index 3 is a byte-identical duplicate of index 0 under
// DedupWithinTurn, so it must not be dispatched at all.
func runBatchOrderTurn(t *testing.T, maxConcurrent int) *BatchOrder {
	t.Helper()
	tool := &orderObservingTool{batchEchoTool: batchEchoTool{name: "observer", schema: []byte(`{}`), result: "ok"}}
	reg := tools.New()
	mustAdd(t, reg, tool)

	resp := toolCallResponse(
		provider.ToolCall{Index: 0, ID: "c0", Name: "observer", Arguments: []byte(`{}`)},
		provider.ToolCall{Index: 1, ID: "c1", Name: "no_such_tool", Arguments: []byte(`{}`)},
		provider.ToolCall{Index: 2, ID: "c2", Name: "observer", Arguments: []byte(`{"k":1}`)},
		provider.ToolCall{Index: 3, ID: "c3", Name: "observer", Arguments: []byte(`{}`)},
	)
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "done")}
	completer := &batchScriptedCompleter{responses: []provider.Response{resp, final}}

	loop, err := New(Options{
		Completer: completer, Tools: reg, Bounds: Bounds{MaxIterations: 3, MaxConcurrentTools: maxConcurrent},
		DedupWithinTurn: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "go")}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	orders := tool.observed()
	if len(orders) == 0 {
		t.Fatal("no tool run observed a BatchOrder on its context")
	}
	for _, o := range orders[1:] {
		if o != orders[0] {
			t.Fatal("tool runs in one batch observed different BatchOrder instances")
		}
	}
	return orders[0]
}

// TestBatchOrderSettlementContract pins the published ledger's contract on
// both dispatch paths: the dispatched set is exactly the non-duplicate
// provider indices, and every dispatched index - including one rejected
// before the tools layer saw it - is settled once the turn's batch ends.
func TestBatchOrderSettlementContract(t *testing.T) {
	for _, tc := range []struct {
		name          string
		maxConcurrent int
	}{
		{name: "serial", maxConcurrent: 0},
		{name: "worker pool", maxConcurrent: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := runBatchOrderTurn(t, tc.maxConcurrent)

			got := order.Dispatched()
			if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
				t.Fatalf("Dispatched = %v, want [0 1 2] (index 3 is a duplicate and never dispatches)", got)
			}
			for _, d := range got {
				if !order.Settled(d) {
					t.Fatalf("dispatched index %d is unsettled after the batch ended", d)
				}
			}
			if order.UnsettledBefore(3) {
				t.Fatal("UnsettledBefore(3) = true after every dispatched index settled")
			}
		})
	}
}

// TestBatchOrderSettlesAbandonedCallsOnAbort pins the abort path: a hard
// tool failure (ErrorPolicyFail) stops the batch, and the calls the abort
// abandoned must still settle - a permanently unsettled dispatched index
// would strand any tool waiting on it.
func TestBatchOrderSettlesAbandonedCallsOnAbort(t *testing.T) {
	failing := &orderObservingTool{batchEchoTool: batchEchoTool{name: "boom", schema: []byte(`{}`), runErr: errBatchAudit}}
	reg := tools.New()
	mustAdd(t, reg, failing)

	resp := toolCallResponse(
		provider.ToolCall{Index: 0, ID: "c0", Name: "boom", Arguments: []byte(`{}`)},
		provider.ToolCall{Index: 1, ID: "c1", Name: "boom", Arguments: []byte(`{"k":1}`)},
		provider.ToolCall{Index: 2, ID: "c2", Name: "boom", Arguments: []byte(`{"k":2}`)},
	)
	completer := &batchScriptedCompleter{responses: []provider.Response{resp}}

	loop, err := New(Options{
		Completer: completer, Tools: reg, Bounds: Bounds{MaxIterations: 3},
		OnToolError: ErrorPolicyFail,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "go")}); err == nil {
		t.Fatal("Run must fail under ErrorPolicyFail with a failing tool")
	}

	orders := failing.observed()
	if len(orders) == 0 {
		t.Fatal("the failing tool never observed a BatchOrder")
	}
	order := orders[0]
	for _, d := range order.Dispatched() {
		if !order.Settled(d) {
			t.Fatalf("dispatched index %d left unsettled after abort abandonment", d)
		}
	}
}
