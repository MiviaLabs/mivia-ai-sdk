package e2e_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestFaultNotifierSurfacesEscalationAsStepFailure proves a channel
// notifier that dies mid-ask fails the step with an error naming the
// fault, instead of leaving the run blocked. The run returns, so the
// escalation never hangs.
func TestFaultNotifierSurfacesEscalationAsStepFailure(t *testing.T) {
	ctx := context.Background()
	plan, m := decidePlanMachine(t)
	reg := tools.New()
	if err := reg.Add(e2e.EscalateTool{ToolName: "decide"}); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	ask := &e2e.FaultNotifier{
		Notifier: func(ctx context.Context, q channel.Question) (channel.Answer, error) {
			return channel.Answer{QuestionID: q.ID, Approved: true, Payload: "ok"}, nil
		},
		FaultOn: 1,
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "fault-notifier-agent", plan), Machine: m,
		Tools: reg, Ask: ask.Notify, AskTo: "human-1",
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-notifier", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the notifier fault")
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("Run error = %v, want e2e.ErrFault", err)
	}
	if !strings.Contains(err.Error(), "channel notifier fault") {
		t.Fatalf("Run error %q does not name the channel notifier fault", err)
	}
}

// TestFaultNotifierPassesThroughThenFaults covers the forwarding path
// the escalation scenario does not reach: the first ask passes to the
// wrapped notifier, the second faults.
func TestFaultNotifierPassesThroughThenFaults(t *testing.T) {
	ctx := context.Background()
	calls := 0
	ask := &e2e.FaultNotifier{FaultOn: 2, Notifier: func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		calls++
		return channel.Answer{QuestionID: q.ID, Approved: true}, nil
	}}
	ans, err := ask.Notify(ctx, channel.Question{ID: "q1", Recipient: "r", Payload: "p"})
	if err != nil || !ans.Approved || calls != 1 {
		t.Fatalf("call 1 Notify = %+v,%v (calls %d), want forwarding", ans, err, calls)
	}
	if _, err := ask.Notify(ctx, channel.Question{ID: "q2", Recipient: "r", Payload: "p"}); !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("call 2 Notify = %v, want the fault", err)
	}
}
