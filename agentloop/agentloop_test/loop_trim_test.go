package agentloop_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunTrimErrorLaterIteration proves a Trim error on the second
// iteration fails the run and preserves the first iteration's
// already-accumulated state, per hardFail's partial-Result rule.
func TestRunTrimErrorLaterIteration(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	calls := 0
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			calls++
			if calls > 1 {
				return nil, errBoom
			}
			return msgs, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run() error = %v, want errBoom", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the prior successful iteration must be preserved", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the prior iteration's accumulated state")
	}
}

// TestRunTrimInvalidMessageLaterIteration proves a Trim hook that
// returns an invalid message on the second iteration fails the run
// and preserves the first iteration's already-accumulated state.
func TestRunTrimInvalidMessageLaterIteration(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	calls := 0
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		Trim: func(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
			calls++
			if calls > 1 {
				return []provider.Message{{Role: provider.RoleTool, ToolCallID: ""}}, nil
			}
			return msgs, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if !errors.Is(err, provider.ErrToolCallIDRequired) {
		t.Fatalf("Run() error = %v, want ErrToolCallIDRequired", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1: the prior successful iteration must be preserved", res.Iterations)
	}
	if len(res.History) == 0 {
		t.Fatalf("History is empty, want the prior iteration's accumulated state")
	}
}
