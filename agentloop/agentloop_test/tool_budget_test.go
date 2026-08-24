package agentloop_test

// ToolBudget hook tests: reserve-before-dispatch with the full raw
// tool-call count, no hook calls when Options.ToolBudget is nil, and
// hard-fail before any tool runs when Reserve errors.

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// toolBudgetLog records every ToolBudget.Reserve call's count, in order.
type toolBudgetLog struct {
	calls []int
}

func (b *toolBudgetLog) hook() *agentloop.ToolBudget {
	return &agentloop.ToolBudget{
		Reserve: func(ctx context.Context, calls int) error {
			b.calls = append(b.calls, calls)
			return nil
		},
	}
}

// TestToolBudgetReserveRunsWithFullRawCountBeforeDispatch proves
// Reserve fires exactly once per turn, with the raw resp.ToolCalls
// count (including a call whose tool the registry never validates
// against a schema), before any tool call's Run executes. The SDK has
// no notion of "malformed" at this call point - that filtering
// happens later, inside runToolCalls - so a host's cumulative budget
// is always charged the full requested count, never a
// downstream-filtered subset. This is a deliberate, documented
// approximation on the conservative side: it can only exhaust a
// host's budget sooner than an exact accounting would, never later.
func TestToolBudgetReserveRunsWithFullRawCountBeforeDispatch(t *testing.T) {
	a := &schemaEchoTool{name: "a", schema: []byte(`{}`), result: "a-out"}
	b := &schemaEchoTool{name: "b", schema: []byte(`{}`), result: "b-out"}
	reg := tools.New()
	mustAdd(t, reg, a)
	mustAdd(t, reg, b)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-a", Name: "a", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-unknown", Name: "does-not-exist", Index: 1, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-b", Name: "b", Index: 2, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	log := &toolBudgetLog{}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, ToolBudget: log.hook()})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(log.calls) != 1 || log.calls[0] != 3 {
		t.Fatalf("reserve calls = %v, want a single call with count 3 (the raw batch size, including the unknown tool)", log.calls)
	}
}

// TestToolBudgetNilHookStillRuns proves a nil ToolBudget changes
// nothing: the run completes and no hook fires (no panic).
func TestToolBudgetNilHookStillRuns(t *testing.T) {
	a := &schemaEchoTool{name: "a", schema: []byte(`{}`), result: "a-out"}
	reg := tools.New()
	mustAdd(t, reg, a)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-a", Name: "a", Index: 0, Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

// TestToolBudgetReserveErrorFailsClosedBeforeAnyToolRuns proves a
// Reserve error hard-fails the run before any tool call in the batch
// executes.
func TestToolBudgetReserveErrorFailsClosedBeforeAnyToolRuns(t *testing.T) {
	errRefused := errors.New("host: tool call budget exceeded")
	a := &schemaEchoTool{name: "a", schema: []byte(`{}`), result: "a-out"}
	b := &schemaEchoTool{name: "b", schema: []byte(`{}`), result: "b-out"}
	reg := tools.New()
	mustAdd(t, reg, a)
	mustAdd(t, reg, b)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-a", Name: "a", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-b", Name: "b", Index: 1, Arguments: []byte("{}")},
		),
	}}
	budget := &agentloop.ToolBudget{Reserve: func(ctx context.Context, calls int) error { return errRefused }}
	loop, err := agentloop.New(agentloop.Options{Completer: completer, Tools: reg, ToolBudget: budget})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	_, err = loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err == nil || !errors.Is(err, errRefused) {
		t.Fatalf("err = %v, want wrap of errRefused", err)
	}
	if got := a.callCount(); got != 0 {
		t.Fatalf("tool a Run calls = %d, want 0 (reserve fails before any dispatch)", got)
	}
	if got := b.callCount(); got != 0 {
		t.Fatalf("tool b Run calls = %d, want 0 (reserve fails before any dispatch)", got)
	}
}
