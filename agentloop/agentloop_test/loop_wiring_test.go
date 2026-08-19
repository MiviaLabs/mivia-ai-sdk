package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/trace"
)

// TestNewPropagatesValidateError proves New returns Options.Validate's
// error before it ever calls Definitions.
func TestNewPropagatesValidateError(t *testing.T) {
	_, err := agentloop.New(agentloop.Options{})
	if !errors.Is(err, agentloop.ErrNoCompleter) {
		t.Fatalf("New() error = %v, want ErrNoCompleter", err)
	}
}

// TestRunHallucinatedSchemaFreeToolName proves a model-requested call
// naming a registered tool that does not implement tools.SchemaTool
// reports a decode-path error under ErrorPolicyReport, the same as
// any other tool-run error.
func TestRunHallucinatedSchemaFreeToolName(t *testing.T) {
	schemaTool := &schemaEchoTool{name: "with-schema", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, schemaTool)
	mustAdd(t, reg, &noSchemaTool{name: "no-schema"})
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "no-schema"}),
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
	var content string
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
			content = m.Content
		}
	}
	if !found {
		t.Fatalf("no RoleTool message reporting the schema-free-tool error: %+v", res.History)
	}
	if !strings.Contains(content, "publishes no schema") {
		t.Fatalf("tool message content = %q, want it to carry the schema-free-tool error text", content)
	}
}

// TestRunBudgetFits proves a Budget the history fits under lets the
// run continue.
func TestRunBudgetFits(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5,
		Budget: &contextbudget.Limits{MaxBytes: 1 << 20},
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
}

// TestRunFiresPointStopOnGracefulStop proves a wired Hooks registry
// fires PointStop once on a graceful StopNoToolCalls return.
func TestRunFiresPointStopOnGracefulStop(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: textMessage(provider.RoleAssistant, "hi")},
	}}
	hreg := hooks.New()
	var fired int
	if err := hreg.Add(hooks.PointStop, "count", func(ctx context.Context, payload any) (bool, error) {
		fired++
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: tools.New(), MaxIterations: 5, Hooks: hreg,
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
	if fired != 1 {
		t.Fatalf("PointStop fired %d times, want 1", fired)
	}
}

// TestRunFiresPointStopOnHardFail proves a wired Hooks registry fires
// PointStop once on a hard-fail return, here ErrCallsPerTurnExceeded,
// and that a PointStop handler's own veto does not change Run's
// error.
func TestRunFiresPointStopOnHardFail(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-1", Name: "echo"},
			provider.ToolCall{ID: "call-2", Name: "echo"},
		),
	}}
	hreg := hooks.New()
	var fired int
	if err := hreg.Add(hooks.PointStop, "veto", func(ctx context.Context, payload any) (bool, error) {
		fired++
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5, MaxCallsPerTurn: 1, Hooks: hreg,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, agentloop.ErrCallsPerTurnExceeded) {
		t.Fatalf("Run() error = %v, want ErrCallsPerTurnExceeded", err)
	}
	if fired != 1 {
		t.Fatalf("PointStop fired %d times, want 1", fired)
	}
}

// TestNewPropagatesDefinitionsError proves New returns Definitions's
// ErrNoSchemas when the registry offers nothing the model can call.
func TestNewPropagatesDefinitionsError(t *testing.T) {
	reg := tools.New()
	mustAdd(t, reg, &noSchemaTool{name: "no-schema"})
	_, err := agentloop.New(agentloop.Options{
		Completer: &scriptedCompleter{}, Tools: reg, MaxIterations: 1,
	})
	if !errors.Is(err, agentloop.ErrNoSchemas) {
		t.Fatalf("New() error = %v, want ErrNoSchemas", err)
	}
}

// TestRunOpensTracerSpans proves a non-nil Tracer opens one span per
// iteration and one per tool call.
func TestRunOpensTracerSpans(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo"}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	tracer := trace.New()
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Tracer: tracer})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	spans := tracer.Spans()
	if len(spans) != 3 {
		t.Fatalf("Spans() len = %d, want 3 (two iterations, one tool call)", len(spans))
	}
}

// TestRunPostToolErrorIsIgnored proves a PointPostTool handler's own
// error does not fail the run: PostTool is informational.
func TestRunPostToolErrorIsIgnored(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	hreg := hooks.New()
	if err := hreg.Add(hooks.PointPostTool, "boom", func(ctx context.Context, payload any) (bool, error) {
		return false, errBoom
	}); err != nil {
		t.Fatalf("hooks.Add error = %v, want nil", err)
	}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo"}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, MaxIterations: 5, Hooks: hreg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: PostTool errors are informational", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %v, want StopNoToolCalls", res.Stop)
	}
}

// TestRunScopeDeniedToolName proves a model-requested call naming a
// registered, schema-bearing tool that Options.Scope denies is
// reported through RunScoped's ErrScopeDenied, the same as any other
// tool-run error, and the denied tool never runs.
func TestRunScopeDeniedToolName(t *testing.T) {
	allowed := &schemaEchoTool{name: "allowed", schema: []byte(`{}`), result: "x"}
	denied := &schemaEchoTool{name: "denied", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, allowed)
	mustAdd(t, reg, denied)
	scope := tools.NewScope(tools.ScopeOptions{Allowlist: []string{"allowed"}})
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "denied", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, Scope: scope, MaxIterations: 5})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	var content string
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
			content = m.Content
		}
	}
	if !found {
		t.Fatalf("no RoleTool message reporting the scope-denied error: %+v", res.History)
	}
	if !strings.Contains(content, tools.ErrScopeDenied.Error()) {
		t.Fatalf("tool message content = %q, want it to carry %q", content, tools.ErrScopeDenied.Error())
	}
	if denied.callCount() != 0 {
		t.Fatalf("denied.callCount() = %d, want 0: a scope-denied tool must never run", denied.callCount())
	}
}

// TestRunToolExecutionError proves a tool's own Run method returning
// an error propagates through the same report/fail path as an
// unknown name or a decode failure.
func TestRunToolExecutionError(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), runErr: errBoom}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
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
	var content string
	found := false
	for _, m := range res.History {
		if m.Role == provider.RoleTool && m.ToolCallID == "call-1" {
			found = true
			content = m.Content
		}
	}
	if !found {
		t.Fatalf("no RoleTool message reporting the tool's own run error: %+v", res.History)
	}
	if !strings.Contains(content, errBoom.Error()) {
		t.Fatalf("tool message content = %q, want it to carry %q", content, errBoom.Error())
	}
	if tool.callCount() != 1 {
		t.Fatalf("tool.callCount() = %d, want 1", tool.callCount())
	}
}
