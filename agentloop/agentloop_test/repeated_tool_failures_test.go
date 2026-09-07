package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRepeatedToolFailuresStopsEarly proves consecutive turns where every
// tool call fails with an unknown tool error stop the run once reaching
// MaxConsecutiveToolFailures, following the graceful Result-shape rule.
func TestRepeatedToolFailuresStopsEarly(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "nonexistent_1", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "nonexistent_2", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "nonexistent_3", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-4", Name: "nonexistent_4", Arguments: []byte("{}")}),
	}}
	const maxFailures = 3
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 10, MaxConsecutiveToolFailures: maxFailures},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopRepeatedToolFailures {
		t.Fatalf("Stop = %v, want StopRepeatedToolFailures", res.Stop)
	}
	if completer.callCount() != maxFailures {
		t.Fatalf("completer calls = %d, want %d: run must stop immediately at threshold", completer.callCount(), maxFailures)
	}
	if res.Iterations != maxFailures {
		t.Fatalf("Iterations = %d, want %d", res.Iterations, maxFailures)
	}
	if !isZeroMessage(res.Final) {
		t.Fatalf("Final = %+v, want zero value on graceful tool stop", res.Final)
	}
	if len(res.History) != 1+2*maxFailures {
		t.Fatalf("History len = %d, want %d", len(res.History), 1+2*maxFailures)
	}
}

// TestRepeatedToolFailuresResetsOnSuccess proves a successful tool call resets
// the consecutive tool failure counter, so N-1 failures followed by a success
// and N-1 more failures do not trigger an early stop.
func TestRepeatedToolFailuresResetsOnSuccess(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "nonexistent", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "nonexistent", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 10, MaxConsecutiveToolFailures: 2},
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
	if completer.callCount() != 4 {
		t.Fatalf("completer calls = %d, want 4", completer.callCount())
	}
	if tool.callCount() != 1 {
		t.Fatalf("tool call count = %d, want 1", tool.callCount())
	}
}

// TestRepeatedToolFailuresResetsOnMixedTurn proves a turn with both unknown and
// valid tool calls does not increment the failure counter and resets it.
func TestRepeatedToolFailuresResetsOnMixedTurn(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "nonexistent", Arguments: []byte("{}")}),
		toolCallResponse(
			provider.ToolCall{Index: 0, ID: "call-2a", Name: "nonexistent", Arguments: []byte("{}")},
			provider.ToolCall{Index: 1, ID: "call-2b", Name: "echo", Arguments: []byte("{}")},
		),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "nonexistent", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 10, MaxConsecutiveToolFailures: 2},
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
	if completer.callCount() != 4 {
		t.Fatalf("completer calls = %d, want 4", completer.callCount())
	}
}

// TestRepeatedToolFailuresExcludesArgValidationAndToolError now proves the
// opposite of its original name: argument validation failures and reported
// tool execution errors DO count toward consecutive tool failures under the
// default ErrorPolicyReport, alongside unknown-tool-name errors, because
// collectCalls now counts any reported tool error. Name kept for history;
// see the addendum "widen the consecutive-tool-failure counter" in
// docs/history/agentloop.md.
func TestRepeatedToolFailuresExcludesArgValidationAndToolError(t *testing.T) {
	echo := &schemaEchoTool{name: "echo", schema: []byte(`{"type":"object","required":["req"]}`), result: "x"}
	failing := &schemaEchoTool{name: "failing", schema: []byte(`{}`), runErr: errBoom}
	reg := tools.New()
	mustAdd(t, reg, echo)
	mustAdd(t, reg, failing)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-2", Name: "failing", Arguments: []byte("{}")}),
		toolCallResponse(provider.ToolCall{ID: "call-3", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: 10, MaxConsecutiveToolFailures: 2},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if res.Stop != agentloop.StopRepeatedToolFailures {
		t.Fatalf("Stop = %v, want StopRepeatedToolFailures", res.Stop)
	}
	if completer.callCount() != 2 {
		t.Fatalf("completer calls = %d, want 2: a validation failure then a reported tool-run error must trip the counter", completer.callCount())
	}
}

// TestRepeatedToolFailuresDefaultZeroUnbounded proves MaxConsecutiveToolFailures 0
// preserves unbounded failure behavior until MaxIterations.
func TestRepeatedToolFailuresDefaultZeroUnbounded(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	const maxIter = 4
	var responses []provider.Response
	for i := 0; i < maxIter; i++ {
		responses = append(responses, toolCallResponse(provider.ToolCall{ID: "call", Name: "nonexistent", Arguments: []byte("{}")}))
	}
	completer := &scriptedCompleter{responses: responses}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     reg,
		Bounds:    agentloop.Bounds{MaxIterations: maxIter, MaxConsecutiveToolFailures: 0},
	})
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
}

// TestRepeatedToolFailuresValidateNegative proves negative
// MaxConsecutiveToolFailures fails Validate with ErrInvalidOptions,
// naming the field.
func TestRepeatedToolFailuresValidateNegative(t *testing.T) {
	opts := agentloop.Options{
		Completer: &scriptedCompleter{},
		Tools:     tools.New(),
		Bounds:    agentloop.Bounds{MaxConsecutiveToolFailures: -1},
	}
	err := opts.Validate()
	if !errors.Is(err, agentloop.ErrInvalidOptions) {
		t.Fatalf("Validate() error = %v, want ErrInvalidOptions", err)
	}
	if !strings.Contains(err.Error(), "MaxConsecutiveToolFailures") {
		t.Fatalf("Validate() error = %v, want it to name MaxConsecutiveToolFailures", err)
	}
}
