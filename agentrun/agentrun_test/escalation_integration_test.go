package agentrun_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// escalateTool fails with an error wrapping agent.ErrEscalated, so the
// built chain routes the step to a human when Ask is set.
type escalateTool struct{}

// Name returns the tool's registry name.
func (escalateTool) Name() string { return "escalate" }

// Run reports a step that needs human review.
func (escalateTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{}, fmt.Errorf("%w: human review required", agent.ErrEscalated)
}

// TestRunEscalation drives the Ask round trip in three behaviors:
// approve, decline, and a Notifier error.
func TestRunEscalation(t *testing.T) {
	plan := mustFlow(t, []flow.Step{{ID: "escalate", To: "resolved", Payload: "p"}}, nil)
	m := mustMachine(t, "queued", tr("queued", "resolved", "run"))

	t.Run("approve", func(t *testing.T) {
		ctx := context.Background()
		ask := &captureAsk{approved: true, payload: "human approved"}
		runner, err := agentrun.New(agentrun.Options{
			Agent:   mustAgent(t, plan),
			Machine: m,
			Tools:   registryOf(t, escalateTool{}),
			Ask:     ask.Answer,
			AskTo:   "human",
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		status, _, err := runner.Run(ctx, "thread-esc-approve", machine.InOut{})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if status != "resolved" {
			t.Fatalf("status = %q, want %q", status, "resolved")
		}
		calls, q := ask.record()
		if calls != 1 || q.Recipient != "human" || q.ID != "escalate" {
			t.Fatalf("ask calls = %d, question = %+v", calls, q)
		}
	})

	t.Run("decline", func(t *testing.T) {
		ctx := context.Background()
		ask := &captureAsk{approved: false, payload: ""}
		runner, err := agentrun.New(agentrun.Options{
			Agent:   mustAgent(t, plan),
			Machine: m,
			Tools:   registryOf(t, escalateTool{}),
			Ask:     ask.Answer,
			AskTo:   "human",
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, _, err = runner.Run(ctx, "thread-esc-decline", machine.InOut{})
		if err == nil {
			t.Fatal("Run succeeded, want a decline failure")
		}
		if calls, _ := ask.record(); calls != 1 {
			t.Fatalf("ask calls = %d, want 1", calls)
		}
	})

	t.Run("notifier error", func(t *testing.T) {
		ctx := context.Background()
		ask := &errAsk{msg: "transport down"}
		runner, err := agentrun.New(agentrun.Options{
			Agent:   mustAgent(t, plan),
			Machine: m,
			Tools:   registryOf(t, escalateTool{}),
			Ask:     ask.Notify,
			AskTo:   "human",
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, _, err = runner.Run(ctx, "thread-esc-err", machine.InOut{})
		if err == nil {
			t.Fatal("Run succeeded, want a Notifier error")
		}
	})
}

// registryOf returns a registry holding t, failing on error.
func registryOf(t *testing.T, ts ...tools.Tool) *tools.Registry {
	t.Helper()
	reg := tools.New()
	addTools(t, reg, ts...)
	return reg
}

// errAsk is a channel.Notifier test double returning a fixed error.
type errAsk struct{ msg string }

// Notify returns the fixed transport error.
func (n *errAsk) Notify(ctx context.Context, q channel.Question) (channel.Answer, error) {
	return channel.Answer{}, errors.New(n.msg)
}
