package agentloop_test

// Shape-repair tests: the loop never carries a shape-empty assistant
// turn into planning or a request. Covers both call points: the
// caller-supplied initial history, and the iteration top on a
// ContinueOnStop continuation after StopEmptyResponse.

import (
	"context"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// carriesEmptyAssistantTurn reports whether msgs holds a
// shape-empty assistant turn: RoleAssistant, Content blank after
// TrimSpace, no ToolCalls, and no ReasoningBlocks.
func carriesEmptyAssistantTurn(msgs []provider.Message) bool {
	for _, m := range msgs {
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) == "" &&
			len(m.ToolCalls) == 0 && len(m.ReasoningBlocks) == 0 {
			return true
		}
	}
	return false
}

// TestShapeRepairDropsEmptyTurnAtIterationStart proves the reachable
// route: a ContinueOnStop continuation after StopEmptyResponse leaves
// the empty assistant turn in history, and the next iteration-top
// filter drops it before the request ships.
func TestShapeRepairDropsEmptyTurnAtIterationStart(t *testing.T) {
	rec := &stopHookRecorder{decide: continueTimes(1, continuationMessage())}
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: ""}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer:      completer,
		Tools:          tools.New(),
		Bounds:         agentloop.Bounds{MaxIterations: 5},
		ContinueOnStop: rec.hook,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := completer.callCount(); got != 2 {
		t.Fatalf("completer calls = %d, want 2", got)
	}
	_, reqs := completerRequests(completer)
	if !carriesEmptyAssistantTurn([]provider.Message{{Role: provider.RoleAssistant}}) {
		t.Fatal("test helper lost its own empty-turn detection")
	}
	if carriesEmptyAssistantTurn(reqs[1].Messages) {
		t.Fatalf("second request still carries the empty assistant turn: %+v", reqs[1].Messages)
	}
	if len(reqs[1].Messages) != 2 ||
		reqs[1].Messages[0].Content != "hi" || reqs[1].Messages[1].Content != "keep going" {
		t.Fatalf("second request = %+v, want [user hi, user keep going]", reqs[1].Messages)
	}
	if carriesEmptyAssistantTurn(res.History) {
		t.Fatalf("Result.History still carries the empty assistant turn: %+v", res.History)
	}
}

// TestShapeRepairFiltersInitialHistory proves an empty assistant turn
// in Run's input never reaches the completer.
func TestShapeRepairFiltersInitialHistory(t *testing.T) {
	completer := &scriptedCompleter{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer,
		Tools:     tools.New(),
		Bounds:    agentloop.Bounds{MaxIterations: 3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msgs := []provider.Message{
		textMessage(provider.RoleUser, "hi"),
		{Role: provider.RoleAssistant, Content: ""},
		textMessage(provider.RoleUser, "next"),
	}
	res, err := loop.Run(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if res.Stop != agentloop.StopNoToolCalls {
		t.Fatalf("Stop = %q, want StopNoToolCalls", res.Stop)
	}
	if got := completer.callCount(); got != 1 {
		t.Fatalf("completer calls = %d, want 1", got)
	}
	_, reqs := completerRequests(completer)
	if carriesEmptyAssistantTurn(reqs[0].Messages) {
		t.Fatalf("request carries the empty assistant turn from the input: %+v", reqs[0].Messages)
	}
	if len(reqs[0].Messages) != 2 ||
		reqs[0].Messages[0].Content != "hi" || reqs[0].Messages[1].Content != "next" {
		t.Fatalf("request = %+v, want [user hi, user next]", reqs[0].Messages)
	}
}
