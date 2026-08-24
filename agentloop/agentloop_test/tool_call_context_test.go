package agentloop_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/toolcallctx"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

type ctxObservingTool struct {
	mu           sync.Mutex
	recordedCall provider.ToolCall
	hasCall      bool
}

func (c *ctxObservingTool) Name() string            { return "ctx_tool" }
func (c *ctxObservingTool) ParameterSchema() []byte { return []byte(`{"type":"object"}`) }
func (c *ctxObservingTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}
func (c *ctxObservingTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	if call, ok := toolcallctx.ToolCallFromContext(ctx); ok {
		c.mu.Lock()
		c.recordedCall = call
		c.hasCall = true
		c.mu.Unlock()
	}
	return tools.Out{Value: "ok"}, nil
}

// 1. Round-trip: tool, hooks (Pre/Post), and bus events receive callCtx with the tool call.
func TestRunOneToolCall_ThreadsToolCallContext(t *testing.T) {
	observedTool := &ctxObservingTool{}
	reg := tools.New()
	if err := reg.Add(observedTool); err != nil {
		t.Fatalf("failed to add tool: %v", err)
	}

	var preHookCall provider.ToolCall
	var preHookFound bool
	var postHookCall provider.ToolCall
	var postHookFound bool
	hookReg := hooks.New()
	_ = hookReg.Add(hooks.PointPreTool, "test", func(ctx context.Context, _ any) (bool, error) {
		if call, ok := toolcallctx.ToolCallFromContext(ctx); ok {
			preHookCall = call
			preHookFound = true
		}
		return true, nil
	})
	_ = hookReg.Add(hooks.PointPostTool, "test", func(ctx context.Context, _ any) (bool, error) {
		if call, ok := toolcallctx.ToolCallFromContext(ctx); ok {
			postHookCall = call
			postHookFound = true
		}
		return true, nil
	})

	var busEventCall provider.ToolCall
	var busEventFound bool
	bus := events.New()
	_ = bus.Subscribe(agentloop.EventToolCallEnd, func(ctx context.Context, _ events.Event) error {
		if call, ok := toolcallctx.ToolCallFromContext(ctx); ok {
			busEventCall = call
			busEventFound = true
		}
		return nil
	})

	completer := &scriptedCompleter{
		responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call_abc", Name: "ctx_tool", Arguments: []byte(`{}`)}),
			{Message: textMessage(provider.RoleAssistant, "Finished")},
		},
	}

	opts := agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		Hooks:         hookReg,
		Bus:           bus,
		MaxIterations: 5,
	}

	loop, err := agentloop.New(opts)
	if err != nil {
		t.Fatalf("agentloop.New err = %v", err)
	}

	_, err = loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "Hello"),
	})
	if err != nil {
		t.Fatalf("loop.Run err = %v", err)
	}

	if !observedTool.hasCall || observedTool.recordedCall.ID != "call_abc" {
		t.Fatalf("Tool.Run expected tool call ID 'call_abc', got %+v (found=%v)", observedTool.recordedCall, observedTool.hasCall)
	}
	if !preHookFound || preHookCall.ID != "call_abc" {
		t.Fatalf("PointPreTool expected tool call ID 'call_abc', got %+v (found=%v)", preHookCall, preHookFound)
	}
	if !postHookFound || postHookCall.ID != "call_abc" {
		t.Fatalf("PointPostTool expected tool call ID 'call_abc', got %+v (found=%v)", postHookCall, postHookFound)
	}
	if !busEventFound || busEventCall.ID != "call_abc" {
		t.Fatalf("EventToolCallEnd expected tool call ID 'call_abc', got %+v (found=%v)", busEventCall, busEventFound)
	}
}

// 2. Parallel-run correctness: 3 overlapTools each reading their own call via context, no time.Sleep, WaitGroup + release channels.
type parallelContextTool struct {
	name    string
	entered chan struct{}
	release chan struct{}
	sawCall provider.ToolCall
	sawOK   bool
	mu      sync.Mutex
}

func (p *parallelContextTool) Name() string            { return p.name }
func (p *parallelContextTool) ParameterSchema() []byte { return []byte(`{"type":"object"}`) }
func (p *parallelContextTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{Value: string(raw)}, nil
}
func (p *parallelContextTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	p.mu.Lock()
	if call, ok := toolcallctx.ToolCallFromContext(ctx); ok {
		p.sawCall = call
		p.sawOK = true
	}
	p.mu.Unlock()

	p.entered <- struct{}{}
	<-p.release

	return tools.Out{Value: "done:" + p.name}, nil
}

func TestToolCallContext_ParallelRun(t *testing.T) {
	release := make(chan struct{})
	t1 := &parallelContextTool{name: "tool_1", entered: make(chan struct{}, 1), release: release}
	t2 := &parallelContextTool{name: "tool_2", entered: make(chan struct{}, 1), release: release}
	t3 := &parallelContextTool{name: "tool_3", entered: make(chan struct{}, 1), release: release}

	reg := tools.New()
	_ = reg.Add(t1)
	_ = reg.Add(t2)
	_ = reg.Add(t3)

	completer := &scriptedCompleter{
		responses: []provider.Response{
			toolCallResponse(
				provider.ToolCall{ID: "call_1", Name: "tool_1", Arguments: []byte(`{}`), Index: 0},
				provider.ToolCall{ID: "call_2", Name: "tool_2", Arguments: []byte(`{}`), Index: 1},
				provider.ToolCall{ID: "call_3", Name: "tool_3", Arguments: []byte(`{}`), Index: 2},
			),
			{Message: textMessage(provider.RoleAssistant, "All done")},
		},
	}

	opts := agentloop.Options{
		Completer:          completer,
		Tools:              reg,
		MaxConcurrentTools: 3,
		MaxIterations:      5,
	}

	loop, err := agentloop.New(opts)
	if err != nil {
		t.Fatalf("agentloop.New err = %v", err)
	}

	runDone := make(chan struct{})
	var runErr error
	go func() {
		defer close(runDone)
		_, runErr = loop.Run(context.Background(), []provider.Message{
			textMessage(provider.RoleUser, "Run parallel"),
		})
	}()

	// Wait for all 3 tools to enter concurrently
	<-t1.entered
	<-t2.entered
	<-t3.entered

	// Release all 3 tools
	close(release)
	<-runDone

	if runErr != nil {
		t.Fatalf("loop.Run err = %v", runErr)
	}

	if !t1.sawOK || t1.sawCall.ID != "call_1" {
		t.Fatalf("t1 saw %+v (ok=%v), want call_1", t1.sawCall, t1.sawOK)
	}
	if !t2.sawOK || t2.sawCall.ID != "call_2" {
		t.Fatalf("t2 saw %+v (ok=%v), want call_2", t2.sawCall, t2.sawOK)
	}
	if !t3.sawOK || t3.sawCall.ID != "call_3" {
		t.Fatalf("t3 saw %+v (ok=%v), want call_3", t3.sawCall, t3.sawOK)
	}
}

// 3. PointPreTool veto path (identity still readable)
func TestToolCallContext_PreToolVeto(t *testing.T) {
	observedTool := &ctxObservingTool{}
	reg := tools.New()
	_ = reg.Add(observedTool)

	var vetoedCall provider.ToolCall
	var vetoedFound bool
	hookReg := hooks.New()
	_ = hookReg.Add(hooks.PointPreTool, "veto-test", func(ctx context.Context, _ any) (bool, error) {
		if call, ok := toolcallctx.ToolCallFromContext(ctx); ok {
			vetoedCall = call
			vetoedFound = true
		}
		return false, nil // Veto!
	})

	completer := &scriptedCompleter{
		responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call_vetoed", Name: "ctx_tool", Arguments: []byte(`{}`)}),
		},
	}

	opts := agentloop.Options{
		Completer:     completer,
		Tools:         reg,
		Hooks:         hookReg,
		MaxIterations: 5,
	}

	loop, err := agentloop.New(opts)
	if err != nil {
		t.Fatalf("agentloop.New err = %v", err)
	}

	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "Veto test"),
	})
	if err != nil {
		t.Fatalf("loop.Run err = %v", err)
	}
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %v, want StopHookVeto", res.Stop)
	}
	if !vetoedFound || vetoedCall.ID != "call_vetoed" {
		t.Fatalf("PointPreTool expected call ID 'call_vetoed', got %+v (found=%v)", vetoedCall, vetoedFound)
	}
	if observedTool.hasCall {
		t.Fatalf("Tool.Run executed despite PointPreTool veto")
	}
}

// 4. ErrorPolicyReport decode-failure path (identity still threaded)
func TestToolCallContext_ErrorPolicyReportDecodeFailure(t *testing.T) {
	observedTool := &ctxObservingTool{}
	reg := tools.New()
	_ = reg.Add(observedTool)

	var errHookCall provider.ToolCall
	var errHookFound bool

	completer := &scriptedCompleter{
		responses: []provider.Response{
			// Invalid arguments JSON fails argument validation / decode
			toolCallResponse(provider.ToolCall{ID: "call_bad_args", Name: "ctx_tool", Arguments: []byte(`{invalid-json`)}),
			{Message: textMessage(provider.RoleAssistant, "Recovered from error")},
		},
	}

	opts := agentloop.Options{
		Completer: completer,
		Tools:     reg,
		OnToolCallError: func(ctx context.Context, call provider.ToolCall, cerr error) (provider.Message, error) {
			if tc, ok := toolcallctx.ToolCallFromContext(ctx); ok {
				errHookCall = tc
				errHookFound = true
			}
			return provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    fmt.Sprintf("recovered: %v", cerr),
			}, nil
		},
		MaxIterations: 5,
	}

	loop, err := agentloop.New(opts)
	if err != nil {
		t.Fatalf("agentloop.New err = %v", err)
	}

	res, err := loop.Run(context.Background(), []provider.Message{
		textMessage(provider.RoleUser, "Bad args test"),
	})
	if err != nil {
		t.Fatalf("loop.Run err = %v", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopCompleted", res.Stop)
	}
	if !errHookFound || errHookCall.ID != "call_bad_args" {
		t.Fatalf("OnToolCallError expected call ID 'call_bad_args', got %+v (found=%v)", errHookCall, errHookFound)
	}
}
