package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunCallsPerTurnExceeded proves a turn whose response requests
// more calls than a positive MaxCallsPerTurn fails the run before any
// call in that turn runs, under both error policies.
func TestRunCallsPerTurnExceeded(t *testing.T) {
	names := map[agentloop.ErrorPolicy]string{
		agentloop.ErrorPolicyReport: "report",
		agentloop.ErrorPolicyFail:   "fail",
	}
	for _, policy := range []agentloop.ErrorPolicy{agentloop.ErrorPolicyReport, agentloop.ErrorPolicyFail} {
		t.Run(names[policy], func(t *testing.T) {
			tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
			reg := tools.New()
			mustAdd(t, reg, tool)
			completer := &scriptedCompleter{responses: []provider.Response{
				toolCallResponse(
					provider.ToolCall{Index: 0, ID: "call-1", Name: "echo", Arguments: []byte("{}")},
					provider.ToolCall{Index: 1, ID: "call-2", Name: "echo", Arguments: []byte("{}")},
				),
			}}
			loop, err := agentloop.New(agentloop.Options{
				Completer: completer, Tools: reg, MaxIterations: 5, MaxCallsPerTurn: 1, OnToolError: policy,
			})
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
			if !errors.Is(err, agentloop.ErrCallsPerTurnExceeded) {
				t.Fatalf("Run() error = %v, want ErrCallsPerTurnExceeded", err)
			}
			if tool.callCount() != 0 {
				t.Fatalf("tool call count = %d, want 0: the trip happens before any call runs", tool.callCount())
			}
			if res.Iterations != 1 {
				t.Fatalf("Iterations = %d, want 1: the assistant turn that requested the calls already completed", res.Iterations)
			}
			if len(res.History) != 2 {
				t.Fatalf("History len = %d, want 2 (user + assistant turn): per hardFail's rule, the completed turn's state must travel", len(res.History))
			}
			if !isZeroMessage(res.Final) || res.Stop != "" {
				t.Fatalf("Final = %+v, Stop = %v, want both zero: hardFail never sets Final or Stop", res.Final, res.Stop)
			}
		})
	}
}

// TestRunMaxCallsPerTurnZeroUnbounded proves a zero MaxCallsPerTurn
// lets a turn requesting many calls run every one of them, unlike a
// positive bound. This is the behavioral proof "zero means unbounded"
// only Validate covered before this test: a >0-only guard bug (for
// example dropping the positivity check) would fail every one of these
// calls instead of running them.
func TestRunMaxCallsPerTurnZeroUnbounded(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{Index: 0, ID: "call-1", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{Index: 1, ID: "call-2", Name: "echo", Arguments: []byte("{}")},
			provider.ToolCall{Index: 2, ID: "call-3", Name: "echo", Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxCallsPerTurn: 0,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
	if tool.callCount() != 3 {
		t.Fatalf("tool call count = %d, want 3: a zero MaxCallsPerTurn must not bound the turn", tool.callCount())
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
		responses = append(responses, toolCallResponse(provider.ToolCall{ID: "call", Name: "echo", Arguments: []byte("{}")}))
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
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
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
		resp:   toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
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

// TestRunCtxCanceledMidTurnStopsRemainingCalls proves a ctx canceled
// by the Completer's own call stops the turn before any of its tool
// calls run: runToolCalls checks ctx.Err() ahead of every call, not
// only once at the top of the outer loop. A run that keeps executing
// tool calls after its ctx is canceled would pay for and cause side
// effects it could still skip.
func TestRunCtxCanceledMidTurnStopsRemainingCalls(t *testing.T) {
	first := &schemaEchoTool{name: "first", schema: []byte(`{}`), result: "x"}
	second := &schemaEchoTool{name: "second", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)
	ctx, cancel := context.WithCancel(context.Background())
	completer := &cancelingCompleter{
		cancel: cancel,
		resp: toolCallResponse(
			provider.ToolCall{Index: 0, ID: "call-1", Name: "first", Arguments: []byte("{}")},
			provider.ToolCall{Index: 1, ID: "call-2", Name: "second", Arguments: []byte("{}")},
		),
	}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(ctx, []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if first.callCount() != 0 {
		t.Fatalf("first.callCount() = %d, want 0: cancellation happens inside Chat, before runToolCalls's first ctx check", first.callCount())
	}
	if second.callCount() != 0 {
		t.Fatalf("second.callCount() = %d, want 0: a canceled ctx must stop the turn before its later calls run", second.callCount())
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

// TestRunBudgetExceededLaterIteration proves a Budget that the first
// iteration fits under, but that the history grown by a tool-call
// round trip no longer fits, fails the run on the second iteration
// and preserves the first iteration's already-accumulated state.
func TestRunBudgetExceededLaterIteration(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		Budget: &contextbudget.Limits{MaxEvents: 2},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want ErrOverBudget", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the prior successful iteration must be preserved", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the prior iteration's accumulated state")
	}
}

// TestRunMidTurnVetoPreservesPriorCall proves a turn with two calls,
// where the first succeeds and the second is vetoed, returns history
// carrying the first call's already-appended RoleTool message: a veto
// stops the turn, it does not discard what already ran.
func TestRunMidTurnVetoPreservesPriorCall(t *testing.T) {
	allowed := &schemaEchoTool{name: "allowed", schema: []byte(`{}`), result: "x"}
	vetoed := &schemaEchoTool{name: "vetoed", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, allowed)
	mustAdd(t, reg, vetoed)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "selective-veto", func(ctx context.Context, payload any) (bool, error) {
		call, ok := payload.(provider.ToolCall)
		if !ok {
			return true, nil
		}
		return call.Name != "vetoed", nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Index: 0, Name: "allowed", Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-2", Index: 1, Name: "vetoed", Arguments: []byte("{}")},
		),
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %v, want StopHookVeto", res.Stop)
	}
	if allowed.callCount() != 1 {
		t.Fatalf("allowed.callCount() = %d, want 1: the first call must run before the veto", allowed.callCount())
	}
	if vetoed.callCount() != 0 {
		t.Fatalf("vetoed.callCount() = %d, want 0: a vetoed call must never run", vetoed.callCount())
	}
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no RoleTool message for call-1 in history: %+v, want the first call's result preserved", res.History)
	}
}
