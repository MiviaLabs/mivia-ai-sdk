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

// TestRunNoToolCallEndsAtOneIteration proves a response with no tool
// call ends the loop at one iteration.
func TestRunNoToolCallEndsAtOneIteration(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi there")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: tools.New(), MaxIterations: 5})
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
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", res.Iterations)
	}
	if res.Final.Content != "hi there" {
		t.Fatalf("Final.Content = %q, want %q", res.Final.Content, "hi there")
	}
}

// TestRunOneToolCallAppendsRoleToolMessage proves a response with one
// tool call runs the tool and appends a RoleTool message whose
// ToolCallID matches the call.
func TestRunOneToolCallAppendsRoleToolMessage(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "tool-result"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("in")}),
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
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
			if m.Content != "tool-result" {
				t.Fatalf("tool message content = %q, want tool-result", m.Content)
			}
		}
	}
	if !found {
		t.Fatalf("no RoleTool message with ToolCallID call-1 in history: %+v", res.History)
	}
	if tool.callCount() != 1 {
		t.Fatalf("tool call count = %d, want 1", tool.callCount())
	}
}

// TestRunTwoCallsRunInIndexOrder proves two calls in one turn run in
// Index order, regardless of slice order.
func TestRunTwoCallsRunInIndexOrder(t *testing.T) {
	var order []string
	mk := func(name string) *schemaEchoTool {
		return &schemaEchoTool{name: name, schema: []byte(`{}`), result: name}
	}
	first := mk("first")
	second := mk("second")
	reg := tools.New()
	mustAdd(t, reg, first)
	mustAdd(t, reg, second)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{Index: 1, ID: "call-2", Name: "second", Arguments: []byte("in")},
			provider.ToolCall{Index: 0, ID: "call-1", Name: "first", Arguments: []byte("in")},
		),
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
	for _, m := range res.History {
		if m.Role == provider.RoleTool {
			order = append(order, m.ToolCallID)
		}
	}
	if len(order) != 2 || order[0] != "call-1" || order[1] != "call-2" {
		t.Fatalf("tool call order = %v, want [call-1 call-2] (Index order)", order)
	}
}

// TestRunUnknownToolName proves an unknown tool name reports
// tools.ErrUnknownName under ErrorPolicyReport and fails under
// ErrorPolicyFail.
func TestRunUnknownToolName(t *testing.T) {
	t.Run("report", func(t *testing.T) {
		reg := tools.New()
		completer := &scriptedCompleter{responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: "missing", Arguments: []byte("in")}),
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
		found := false
		for _, m := range res.History {
			if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no RoleTool message reporting the unknown-name error: %+v", res.History)
		}
	})
	t.Run("fail", func(t *testing.T) {
		reg := tools.New()
		completer := &scriptedCompleter{responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: "missing", Arguments: []byte("in")}),
		}}
		loop, err := agentloop.New(agentloop.Options{
			Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
		})
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
		}
		_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
		if !errors.Is(err, tools.ErrUnknownName) {
			t.Fatalf("Run() error = %v, want tools.ErrUnknownName", err)
		}
	})
}

// TestRunPreToolVetoStops proves a PointPreTool veto stops with
// StopHookVeto and runs no tool.
func TestRunPreToolVetoStops(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "unused"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPreTool, "veto", func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("in")}),
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
	if !isZeroMessage(res.Final) {
		t.Fatalf("Final = %+v, want the zero value", res.Final)
	}
	if tool.callCount() != 0 {
		t.Fatalf("tool call count = %d, want 0: a veto must not run the tool", tool.callCount())
	}
}

// TestRunDecodeArgumentsFailure proves a tool whose DecodeArguments
// fails on malformed model-supplied JSON reports the decode error
// under ErrorPolicyReport and fails the run under ErrorPolicyFail,
// the same as a failed Run.
func TestRunDecodeArgumentsFailure(t *testing.T) {
	t.Run("report", func(t *testing.T) {
		tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), decodeErr: errBoom}
		reg := tools.New()
		mustAdd(t, reg, tool)
		completer := &scriptedCompleter{responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("bad")}),
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
		found := false
		for _, m := range res.History {
			if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no RoleTool message reporting the decode error: %+v", res.History)
		}
		if tool.callCount() != 0 {
			t.Fatalf("tool call count = %d, want 0: a decode failure never reaches Run", tool.callCount())
		}
	})
	t.Run("fail", func(t *testing.T) {
		tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), decodeErr: errBoom}
		reg := tools.New()
		mustAdd(t, reg, tool)
		completer := &scriptedCompleter{responses: []provider.Response{
			toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("bad")}),
		}}
		loop, err := agentloop.New(agentloop.Options{
			Completer: completer, Tools: reg, MaxIterations: 5, OnToolError: agentloop.ErrorPolicyFail,
		})
		if err != nil {
			t.Fatalf("New() error = %v, want nil", err)
		}
		_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
		if !errors.Is(err, errBoom) {
			t.Fatalf("Run() error = %v, want errBoom", err)
		}
	})
}

// TestRunBudgetExceeded proves a Budget the history outgrows fails
// the run with ErrOverBudget.
func TestRunBudgetExceeded(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Budget: &contextbudget.Limits{MaxBytes: 1},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "a much longer starting message")})
	if !errors.Is(err, agentloop.ErrOverBudget) {
		t.Fatalf("Run() error = %v, want ErrOverBudget", err)
	}
}

// TestRunTrimError proves a Trim hook returning an error fails the
// run.
func TestRunTrimError(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			return nil, errBoom
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run() error = %v, want errBoom", err)
	}
}

// TestRunTrimInvalidMessage proves a Trim hook returning a slice with
// one invalid message fails the run with the wrapped
// provider.Message.Validate error, before the next Completer call.
func TestRunTrimInvalidMessage(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			return []provider.Message{{Role: provider.RoleTool, ToolCallID: ""}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("Run() error = %v, want ErrToolCallIDRequired", err)
	}
	if completer.callCount() != 0 {
		t.Fatalf("Chat call count = %d, want 0: validation must fail before the Completer call", completer.callCount())
	}
}

// TestRunTrimDropsToolReplyPassesValidation documents that Trim
// dropping a RoleTool reply while keeping the assistant message's
// matching ToolCalls entry passes Run's per-message validation
// unchanged and reaches Completer: cross-message pairing stays the
// caller's responsibility.
func TestRunTrimDropsToolReplyPassesValidation(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	assistantWithCall := provider.Message{
		Role:      provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "echo"}},
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			return []provider.Message{textMessage(provider.RoleUser, "hi"), assistantWithCall}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: cross-message pairing is not validated here", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
}
