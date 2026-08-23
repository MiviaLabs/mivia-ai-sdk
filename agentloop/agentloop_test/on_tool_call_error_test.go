package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestOnToolCallErrorDefaultUnchanged proves the pre-Item-4 contract
// still holds when OnToolCallError is nil: a decodeAndRun failure
// (here, an empty-Name call synthesizing tools.ErrUnknownName) lands
// in history as a RoleTool message whose Content carries the
// [tool-error] prefix, and the run stops gracefully with
// StopNoToolCalls after the next Completer call.
func TestOnToolCallErrorDefaultUnchanged(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	content := findToolContent(t, res.History, "call-1")
	if !strings.HasPrefix(content, agentloop.ToolErrorPrefix) {
		t.Fatalf("content = %q, want it to start with %q", content, agentloop.ToolErrorPrefix)
	}
	if !strings.Contains(content, tools.ErrUnknownName.Error()) {
		t.Fatalf("content = %q, want it to carry %q", content, tools.ErrUnknownName.Error())
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls: the default hook must not change the stop reason", res.Stop)
	}
}

// TestOnToolCallErrorSynthesizesMessage proves a hook returning a
// non-nil synthesized provider.Message replaces the [tool-error]
// body. The returned message reaches history as the call's RoleTool
// entry, the run continues, and the stop reason stays StopNoToolCalls.
func TestOnToolCallErrorSynthesizesMessage(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	var (
		mu         sync.Mutex
		gotCall    provider.ToolCall
		gotErr     error
		hookCalled bool
	)
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		OnToolCallError: func(ctx context.Context, call provider.ToolCall, cerr error) (provider.Message, error) {
			mu.Lock()
			defer mu.Unlock()
			hookCalled = true
			gotCall = call
			gotErr = cerr
			return provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    "skipped-by-hook",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !hookCalled {
		t.Fatalf("OnToolCallError was not called")
	}
	if gotCall.ID != "call-1" {
		t.Fatalf("hook received call.ID = %q, want %q", gotCall.ID, "call-1")
	}
	if !errors.Is(gotErr, tools.ErrUnknownName) {
		t.Fatalf("hook received err = %v, want it to wrap tools.ErrUnknownName", gotErr)
	}
	content := findToolContent(t, res.History, "call-1")
	if content != "skipped-by-hook" {
		t.Fatalf("content = %q, want %q", content, "skipped-by-hook")
	}
	if strings.HasPrefix(content, agentloop.ToolErrorPrefix) {
		t.Fatalf("content = %q, want the hook's synthesized body, not the [tool-error] prefix", content)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
}

// TestOnToolCallErrorSkipsWithErr proves a hook returning a non-nil
// error ends the call without appending a [tool-error] body to
// history: runToolCalls propagates the hook's error up, run hard-fails
// the run with that error wrapped under iteration + call.ID. The
// completed assistant turn's state still travels per hardFail's rule.
func TestOnToolCallErrorSkipsWithErr(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "", Arguments: []byte("{}")}),
	}}
	hookErr := errors.New("agentloop_test: hook skipped the call")
	var (
		mu         sync.Mutex
		hookCalled bool
	)
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		OnToolCallError: func(ctx context.Context, call provider.ToolCall, cerr error) (provider.Message, error) {
			mu.Lock()
			defer mu.Unlock()
			hookCalled = true
			return provider.Message{}, hookErr
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, runErr := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(runErr, hookErr) {
		t.Fatalf("Run() error = %v, want it to wrap hookErr", runErr)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the assistant turn that requested the call already completed", res.Iterations)
	}
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			t.Fatalf("history carries a RoleTool for call-1 = %+v, want the hook's error to skip the append", m)
		}
	}
	if !isZeroMessage(res.Final) || res.Stop != "" {
		t.Fatalf("Final = %+v, Stop = %v, want both zero: hardFail never sets Final or Stop", res.Final, res.Stop)
	}
	mu.Lock()
	defer mu.Unlock()
	if !hookCalled {
		t.Fatalf("OnToolCallError was not called")
	}
}

// TestOnToolCallErrorFailPolicyBypassesHook proves ErrorPolicyFail
// still hard-fails Run before the hook fires: the hook only runs on
// the ErrorPolicyReport path, so a Fail-policy tool error must not
// silently degrade into a synthesized message.
func TestOnToolCallErrorFailPolicyBypassesHook(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "", Arguments: []byte("{}")}),
	}}
	var (
		mu         sync.Mutex
		hookCalled bool
	)
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		OnToolError: agentloop.ErrorPolicyFail,
		OnToolCallError: func(ctx context.Context, call provider.ToolCall, cerr error) (provider.Message, error) {
			mu.Lock()
			defer mu.Unlock()
			hookCalled = true
			return provider.Message{Content: "should-never-append"}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, runErr := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(runErr, tools.ErrUnknownName) {
		t.Fatalf("Run() error = %v, want it to wrap tools.ErrUnknownName", runErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if hookCalled {
		t.Fatalf("OnToolCallError was called under ErrorPolicyFail, want it bypassed")
	}
}
