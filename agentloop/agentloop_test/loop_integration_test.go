package agentloop_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextplan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestLoopIntegrationTwoToolThreeIteration runs a scripted Completer
// and a real tools.Registry through a two-tool, three-iteration task
// end to end.
func TestLoopIntegrationTwoToolThreeIteration(t *testing.T) {
	search := &schemaEchoTool{name: "search", schema: []byte(`{"type":"object"}`), result: "search-hit"}
	fetch := &schemaEchoTool{name: "fetch", schema: []byte(`{"type":"object"}`), result: "fetch-body"}
	reg := tools.New()
	mustAdd(t, reg, search)
	mustAdd(t, reg, fetch)

	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "search", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "fetch", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final answer")},
	}}

	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "find and fetch")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if res.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3", res.Iterations)
	}
	if res.Final.Content != "final answer" {
		t.Fatalf("Final.Content = %q, want %q", res.Final.Content, "final answer")
	}
	if search.callCount() != 1 || fetch.callCount() != 1 {
		t.Fatalf("call counts = search:%d fetch:%d, want 1,1", search.callCount(), fetch.callCount())
	}
}

// TestLoopIntegrationTrimBindsToContextPlanPlan proves Options.Trim's
// signature is type-compatible with a closure over
// contextplan.Planner.Plan: the closure discards msgs and reads from a
// Session the test seeds once, and its ctx and error returns pass
// straight through Trim's call site.
func TestLoopIntegrationTrimBindsToContextPlanPlan(t *testing.T) {
	store, err := contextstate.New(contextstate.Limits{})
	if err != nil {
		t.Fatalf("contextstate.New: %v", err)
	}
	cache, err := memory.New(1 << 20)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	planner, err := contextplan.NewPlanner(store, cache)
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	data := []byte("seed content")
	ref, err := contextstate.NewContentRef("agentloop_test", "workspace-a", "sess-a", "subject-a", data)
	if err != nil {
		t.Fatalf("NewContentRef: %v", err)
	}
	if err := store.Put(contextstate.PayloadRecord{Ref: ref, Retention: contextstate.RetentionSession, Data: data}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sess := &contextstate.Session{Source: []contextstate.SourceEvent{{
		ID:              contextstate.SourceID{SessionID: "sess-a", Sequence: 1},
		Kind:            "message",
		Role:            string(provider.RoleUser),
		PayloadRef:      ref.Ref,
		Provenance:      "fixture",
		RedactionStatus: "none",
		Size:            len(data),
	}}}
	window := contextplan.Window{MaxTokens: 1000}
	estimator := byteEstimator{}

	trim := func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
		result, err := planner.Plan(ctx, sess, window, estimator)
		if err != nil {
			return nil, err
		}
		return result.Request.Messages, nil
	}

	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "done")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: tools.New(), MaxIterations: 3, Trim: trim})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "ignored by trim")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	req := completer.lastRequest()
	if len(req.Messages) != 1 || req.Messages[0].Content != string(data) {
		t.Fatalf("Request.Messages = %+v, want the trimmed Session content", req.Messages)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
}

// byteEstimator counts one token per content byte across every
// message; deterministic for the window budget above.
type byteEstimator struct{}

func (byteEstimator) EstimateTokens(req provider.Request) (int, error) {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}
