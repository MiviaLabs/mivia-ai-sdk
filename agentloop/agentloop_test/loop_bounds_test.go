package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/usage"
)

// TestRunCallsPerTurnExceeded proves a turn whose response requests
// more calls than a positive MaxCallsPerTurn fails the run before any
// call in that turn runs, under both error policies.
func TestRunCallsPerTurnExceeded(t *testing.T) {
	for _, policy := range []agentloop.ErrorPolicy{agentloop.ErrorPolicyReport, agentloop.ErrorPolicyFail} {
		t.Run(string(policy)+"-or-report", func(t *testing.T) {
			tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
			reg := tools.New()
			mustAdd(t, reg, tool)
			completer := &scriptedCompleter{responses: []provider.Response{
				toolCallResponse(
					provider.ToolCall{Index: 0, ID: "call-1", Name: "echo"},
					provider.ToolCall{Index: 1, ID: "call-2", Name: "echo"},
				),
			}}
			loop, err := agentloop.New(agentloop.Options{
				Completer: completer, Tools: reg, MaxIterations: 5, MaxCallsPerTurn: 1, OnToolError: policy,
			})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
			if !errors.Is(err, agentloop.ErrCallsPerTurnExceeded) {
				t.Fatalf("Run() error = %v, want ErrCallsPerTurnExceeded", err)
			}
			if tool.callCount() != 0 {
				t.Fatalf("tool call count = %d, want 0: the trip happens before any call runs", tool.callCount())
			}
		})
	}
}

// TestRunMaxIterationsGracefulStop proves a model that always calls a
// tool stops at MaxIterations, following the Result-shape rule: a nil
// error and Stop == StopMaxIterations, with History, Iterations, and
// Usage carrying the accumulated state.
func TestRunMaxIterationsGracefulStop(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	const maxIter = 3
	var responses []provider.Response
	for i := 0; i < maxIter+2; i++ {
		responses = append(responses, toolCallResponse(provider.ToolCall{ID: "call", Name: "echo"}))
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: maxIter})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations", res.Stop)
	}
	if res.Iterations != maxIter {
		t.Fatalf("Iterations = %d, want %d", res.Iterations, maxIter)
	}
	if !isZeroMessage(res.Final) {
		t.Fatalf("Final = %+v, want the zero value", res.Final)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the accumulated turns")
	}
}

// TestRunMaxTotalTokens proves a scripted Completer whose responses'
// summed Usage.TotalTokens crosses a positive MaxTotalTokens on the
// second iteration fails the run with ErrTokenBudgetExceeded, and
// that a paired Options.Usage accumulator still records the tripping
// call's tokens.
func TestRunMaxTotalTokens(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	responses := []provider.Response{
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo"})
			r.Usage = provider.Usage{TotalTokens: 60}
			return r
		}(),
		func() provider.Response {
			r := toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo"})
			r.Usage = provider.Usage{TotalTokens: 60}
			return r
		}(),
	}
	completer := &scriptedCompleter{responses: responses}
	acc := usage.New()
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxTotalTokens: 100,
		Usage: acc, SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrTokenBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrTokenBudgetExceeded", err)
	}
	total, ok := acc.Total("sess-1")
	if !ok {
		t.Fatalf("acc.Total(sess-1) ok = false, want true")
	}
	if total.TotalTokens != 120 {
		t.Fatalf("acc.Total(sess-1).TotalTokens = %d, want 120: the tripping call's tokens must still land", total.TotalTokens)
	}
}

// TestRunZeroMaxTotalTokensUnbounded proves a zero MaxTotalTokens with
// the same scripted responses runs to StopMaxIterations unaffected.
func TestRunZeroMaxTotalTokensUnbounded(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	mk := func(id string) provider.Response {
		r := toolCallResponse(provider.ToolCall{ID: id, Name: "echo"})
		r.Usage = provider.Usage{TotalTokens: 1000}
		return r
	}
	completer := &scriptedCompleter{responses: []provider.Response{mk("call-1"), mk("call-2")}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 2})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopMaxIterations {
		t.Fatalf("Stop = %v, want StopMaxIterations", res.Stop)
	}
}

// TestRunDefinitionsCachedOnce proves a tool registered after New but
// before Run is never offered to the model, since Definitions runs
// once at New and Run reuses the cached result.
func TestRunDefinitionsCachedOnce(t *testing.T) {
	reg := tools.New()
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	mustAdd(t, reg, &schemaEchoTool{name: "late", schema: []byte(`{}`), result: "x"})

	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	req := completer.lastRequest()
	for _, def := range req.Tools {
		if def.Name == "late" {
			t.Fatalf("Request.Tools contains %q, want Definitions cached at New to exclude it", def.Name)
		}
	}
}

// TestRunPreToolNonVetoErrorFails proves a PointPreTool handler
// returning a non-veto error fails the run with the wrapped handler
// error, distinguished from a veto by errors.Is against
// hooks.ErrVetoed, and that the returned Result carries the
// accumulated state at the point of failure.
func TestRunPreToolNonVetoErrorFails(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "boom", func(ctx context.Context, payload any) (bool, error) {
		return false, errBoom
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo"}),
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run() error = %v, want errBoom", err)
	}
	if errors.Is(err, hooks.ErrVetoed) {
		t.Fatalf("Run() error wraps hooks.ErrVetoed, want a plain non-veto handler error")
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the assistant turn that requested the call already completed", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the accumulated state at the point of failure")
	}
}

// TestRunCtxCanceledBeforeFirstIteration proves a ctx canceled before
// the first iteration completes returns the ctx error alongside the
// zero-value Result.
func TestRunCtxCanceledBeforeFirstIteration(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: tools.New(), MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := loop.Run(ctx, []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if res.Iterations != 0 || res.Stop != "" || !isZeroMessage(res.Final) || len(res.History) != 0 {
		t.Fatalf("Result = %+v, want the zero value", res)
	}
}

// TestRunCtxCanceledAtLaterIteration proves a ctx canceled at the
// start of a later iteration, after at least one prior iteration
// already completed, returns the ctx error alongside a Result whose
// History, Iterations, and Usage carry that prior iteration's
// accumulated state.
func TestRunCtxCanceledAtLaterIteration(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	ctx, cancel := context.WithCancel(context.Background())
	completer := &cancelingCompleter{
		cancel: cancel,
		resp:   toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo"}),
	}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(ctx, []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", res.Iterations)
	}
	if !isZeroMessage(res.Final) || res.Stop != "" {
		t.Fatalf("Final = %+v, Stop = %v, want both zero", res.Final, res.Stop)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the prior iteration's accumulated state")
	}
}

// cancelingCompleter returns resp on its first Chat call and cancels
// ctx as a side effect of that first call returning, so Run's next
// top-of-loop ctx check observes cancellation.
type cancelingCompleter struct {
	cancel context.CancelFunc
	resp   provider.Response
	calls  int
}

func (c *cancelingCompleter) Name() string { return "canceling" }

func (c *cancelingCompleter) Chat(ctx context.Context, req provider.Request) (provider.Response, error) {
	c.calls++
	if c.calls == 1 {
		c.cancel()
		return c.resp, nil
	}
	return provider.Response{}, errors.New("cancelingCompleter: no further response scripted")
}

func (c *cancelingCompleter) ChatStream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("cancelingCompleter: ChatStream not supported")
}
