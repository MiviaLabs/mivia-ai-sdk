package subagent_test

import (
	"context"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestAsToolForwardsEventsToParentBus proves a caller-supplied bus
// observes the spawned run's delivered, acked, and verified events.
func TestAsToolForwardsEventsToParentBus(t *testing.T) {
	ctx := context.Background()
	parent := events.New()
	seen := map[events.Name]int{}
	noop := func(context.Context, events.Event) error { return nil }
	for _, name := range []events.Name{
		agent.MessageDeliveredEvent, agent.MessageAckedEvent,
		agent.ThreadVerifiedEvent,
	} {
		if err := parent.Subscribe(name, noop); err != nil {
			t.Fatalf("Subscribe(%s): %v", name, err)
		}
		if err := parent.Subscribe(name, func(context.Context, events.Event) error {
			seen[name]++
			return nil
		}); err != nil {
			t.Fatalf("Subscribe(%s): %v", name, err)
		}
	}
	runner := prefixRunner(t, "ran:", &agentrun.Artifacts{})
	tool := subagent.AsTool("sub", runner, subagent.ToolOptions{Bus: parent})
	if _, err := tool.Run(ctx, tools.InOut{Value: "go"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for name, want := range map[events.Name]int{
		agent.MessageDeliveredEvent: 1,
		agent.MessageAckedEvent:     1,
		agent.ThreadVerifiedEvent:   1,
	} {
		if seen[name] != want {
			t.Errorf("%s = %d, want %d", name, seen[name], want)
		}
	}
}
