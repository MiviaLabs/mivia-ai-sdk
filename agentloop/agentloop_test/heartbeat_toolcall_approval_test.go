package agentloop_test

import (
	"context"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestRunToolCallHeartbeatSpansApprovalGate proves the tool-call-
// heartbeat ticker startHeartbeat starts around decodeAndRun in
// runOneToolCall keeps ticking across tools.Registry.RunScoped's
// approval gate, a Scope.Approve call that runs before Tool.Run and
// blocks independently of it (tools/registry.go's RunScoped doc: "the
// map lookup lock is released before approve runs, so a blocking
// approve never blocks other registry callers"). The tool's own Run
// returns instantly here, so any observed tick can only come from the
// approval wait: a build that started the ticker only around Tool.Run
// itself, narrower than decodeAndRun's actual bracket, would go
// silent during a slow approval and still pass every other heartbeat
// test, none of which wires a Scope with Approve.
func TestRunToolCallHeartbeatSpansApprovalGate(t *testing.T) {
	tool := &schemaEchoTool{name: "echo", schema: []byte(`{}`), result: "x"}
	reg := tools.New()
	mustAdd(t, reg, tool)
	approve := func(ctx context.Context, call tools.ToolCall) (bool, error) {
		select {
		case <-time.After(heartbeatTestBlock):
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	scope := tools.NewScope(tools.ScopeOptions{Approve: approve})
	bus := events.New()
	ch, handler := eventChan()
	subscribeEvents(t, bus, handler, agentloop.EventToolCallStart, agentloop.EventToolCallHeartbeat, agentloop.EventToolCallEnd)
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(provider.ToolCall{ID: "call-1", Name: "echo", Arguments: []byte("{}")}),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, Scope: scope, MaxIterations: 5, Bus: bus, HeartbeatInterval: heartbeatTestInterval,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")}); err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	}()

	timer := time.NewTimer(heartbeatTestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out after %v waiting for Run to return", heartbeatTestTimeout)
	}

	got := drainAllBuffered(ch)
	assertToolCallHeartbeatSequence(t, got)
	if tool.callCount() != 1 {
		t.Fatalf("tool call count = %d, want 1: the tool must still run once approval passes", tool.callCount())
	}
}
