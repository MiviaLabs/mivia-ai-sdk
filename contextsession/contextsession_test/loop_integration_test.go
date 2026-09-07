package contextsession_test

import (
	"errors"
	"sync"

	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/context/plan"
	"github.com/MiviaLabs/mivia-ai-sdk/contextsession"
	"github.com/MiviaLabs/mivia-ai-sdk/contextstate"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestLoopIntegrationTwoToolThreeIteration runs a scripted Completer
// and a real tools.Registry through a two-tool, three-iteration task
// end to end.
func TestLoopIntegrationTwoToolThreeIteration(t *testing.T) {
	search := &loopSchemaTool{name: "search", schema: []byte(`{"type":"object"}`), result: "search-hit"}
	fetch := &loopSchemaTool{name: "fetch", schema: []byte(`{"type":"object"}`), result: "fetch-body"}
	reg := tools.New()
	loopMustAdd(t, reg, search)
	loopMustAdd(t, reg, fetch)

	completer := &loopScriptedCompleter{responses: []provider.Response{
		loopToolCallResponse(provider.ToolCall{ID: "call-1", Name: "search", Arguments: []byte("{}")}),
		loopToolCallResponse(provider.ToolCall{ID: "call-2", Name: "fetch", Arguments: []byte("{}")}),
		{Message: loopTextMessage(provider.RoleAssistant, "final answer")},
	}}

	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, Bounds: agentloop.Bounds{MaxIterations: 5}})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{loopTextMessage(provider.RoleUser, "find and fetch")})
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
// plan.Planner.Plan: the closure discards msgs and reads from a
// Session the test seeds once, and its ctx and error returns pass
// straight through Trim's call site.
func TestLoopIntegrationTrimBindsToContextPlanPlan(t *testing.T) {
	store, err := contextstate.New(contextstate.Limits{})
	if err != nil {
		t.Fatalf("contextstate.New: %v", err)
	}
	planner, err := contextsession.NewPlanner(store, nil)
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
	window := plan.Window{MaxTokens: 1000}
	estimator := loopByteEstimator{}

	trim := func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
		result, err := planner.Plan(ctx, sess, window, estimator)
		if err != nil {
			return nil, err
		}
		return result.Request.Messages, nil
	}

	completer := &loopScriptedCompleter{responses: []provider.Response{
		{Message: loopTextMessage(provider.RoleAssistant, "done")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: tools.New(), Bounds: agentloop.Bounds{MaxIterations: 3}, Trim: trim})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{loopTextMessage(provider.RoleUser, "ignored by trim")})
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

// loopByteEstimator counts one token per content byte across every
// message; deterministic for the window budget above.
type loopByteEstimator struct{}

func (loopByteEstimator) EstimateTokens(req provider.Request) (int, error) {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content)
	}
	return total, nil
}

// loopSchemaTool is the loop integration test's schema tool double.
type loopSchemaTool struct {
	mu     sync.Mutex
	name   string
	schema []byte
	result any
	calls  int
}

func (t *loopSchemaTool) Name() string            { return t.name }
func (t *loopSchemaTool) ParameterSchema() []byte { return t.schema }

func (t *loopSchemaTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}

func (t *loopSchemaTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	return tools.Out{Value: t.result}, nil
}

func (t *loopSchemaTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func loopMustAdd(t *testing.T, reg *tools.Registry, tool tools.Tool) {
	t.Helper()
	if err := reg.Add(tool); err != nil {
		t.Fatalf("Add(%s) error = %v, want nil", tool.Name(), err)
	}
}

// loopScriptedCompleter replays scripted responses in order.
type loopScriptedCompleter struct {
	responses []provider.Response
	reqs      []provider.Request
}

func (s *loopScriptedCompleter) Name() string { return "scripted" }

func (s *loopScriptedCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	s.reqs = append(s.reqs, req)
	if len(s.responses) == 0 {
		return provider.Response{}, errors.New("loopScriptedCompleter: no response scripted")
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	return resp, nil
}

func (s *loopScriptedCompleter) lastRequest() provider.Request {
	return s.reqs[len(s.reqs)-1]
}

func (s *loopScriptedCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("loopScriptedCompleter: ChatStream not supported")
}

func loopToolCallResponse(calls ...provider.ToolCall) provider.Response {
	msg := provider.Message{Role: provider.RoleAssistant, ToolCalls: calls}
	return provider.Response{Message: msg, ToolCalls: calls}
}

func loopTextMessage(role provider.Role, content string) provider.Message {
	return provider.Message{Role: role, Content: content}
}
