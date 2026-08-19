package e2e_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// hangRunner builds a one-step runner whose tool is a provider that
// never answers until its ctx is canceled.
func hangRunner(t *testing.T) *agentrun.Runner {
	t.Helper()
	plan, err := flow.New([]flow.Step{{ID: "chat", To: "done", Payload: "go"}}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued", machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	if err := reg.Add(subagent.ProviderTool("chat", &e2e.HangCompleter{})); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "hang-agent", plan), Machine: m, Tools: reg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	return runner
}

// TestFaultHangCompleterSurfacesTimeout proves a provider that never
// answers fails the run with the deadline error once the run ctx
// cancels, instead of hanging the run forever.
func TestFaultHangCompleterSurfacesTimeout(t *testing.T) {
	runner := hangRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err := runner.Run(ctx, "thread-hang", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the hang to surface as a timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

// TestHangCompleterStreamSurfacesTimeout covers the stream half the
// run scenario does not reach: ChatStream blocks until ctx is done,
// then returns the deadline error, and Name stays a fixed label.
func TestHangCompleterStreamSurfacesTimeout(t *testing.T) {
	h := &e2e.HangCompleter{}
	if got := h.Name(); got != "hang-completer" {
		t.Fatalf("Name = %q, want hang-completer", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := h.ChatStream(ctx, provider.Request{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ChatStream = %v, want it to wrap context.DeadlineExceeded", err)
	}
}
