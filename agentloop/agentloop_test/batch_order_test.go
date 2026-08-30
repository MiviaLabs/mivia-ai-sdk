package agentloop_test

import (
	"context"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/toolcallctx"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// orderObservingTool records the BatchOrder each Run observed on its ctx.
type orderObservingTool struct {
	schemaEchoTool
	mu     sync.Mutex
	orders []*toolcallctx.BatchOrder
}

func (t *orderObservingTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if order, ok := toolcallctx.BatchOrderFromContext(ctx); ok {
		t.mu.Lock()
		t.orders = append(t.orders, order)
		t.mu.Unlock()
	}
	return t.schemaEchoTool.Run(ctx, in)
}

func (t *orderObservingTool) observed() []*toolcallctx.BatchOrder {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*toolcallctx.BatchOrder(nil), t.orders...)
}

// runBatchOrderTurn drives one turn with calls [0 ok, 1 unknown-name
// reject, 2 ok, 3 duplicate-of-0] and returns the BatchOrder the tools
// observed. Index 3 is a byte-identical duplicate of index 0 under
// DedupWithinTurn, so it must not be dispatched at all.
func runBatchOrderTurn(t *testing.T, maxConcurrent int) *toolcallctx.BatchOrder {
	t.Helper()
	tool := &orderObservingTool{schemaEchoTool: schemaEchoTool{name: "observer", schema: []byte(`{}`), result: "ok"}}
	reg := tools.New()
	mustAdd(t, reg, tool)

	resp := toolCallResponse(
		provider.ToolCall{Index: 0, ID: "c0", Name: "observer", Arguments: []byte(`{}`)},
		provider.ToolCall{Index: 1, ID: "c1", Name: "no_such_tool", Arguments: []byte(`{}`)},
		provider.ToolCall{Index: 2, ID: "c2", Name: "observer", Arguments: []byte(`{"k":1}`)},
		provider.ToolCall{Index: 3, ID: "c3", Name: "observer", Arguments: []byte(`{}`)},
	)
	final := provider.Response{Message: textMessage(provider.RoleAssistant, "done")}
	completer := &scriptedCompleter{responses: []provider.Response{resp, final}}

	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 3,
		DedupWithinTurn: true, MaxConcurrentTools: maxConcurrent,
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
	failing := &orderObservingTool{schemaEchoTool: schemaEchoTool{name: "boom", schema: []byte(`{}`), runErr: errAudit}}
	reg := tools.New()
	mustAdd(t, reg, failing)

	resp := toolCallResponse(
		provider.ToolCall{Index: 0, ID: "c0", Name: "boom", Arguments: []byte(`{}`)},
		provider.ToolCall{Index: 1, ID: "c1", Name: "boom", Arguments: []byte(`{"k":1}`)},
		provider.ToolCall{Index: 2, ID: "c2", Name: "boom", Arguments: []byte(`{"k":2}`)},
	)
	completer := &scriptedCompleter{responses: []provider.Response{resp}}

	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 3,
		OnToolError: agentloop.ErrorPolicyFail,
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
