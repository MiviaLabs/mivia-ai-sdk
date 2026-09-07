package main

import (
	"context"
	"fmt"

	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/scheduler"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// buildSurfaceRunner assembles the one-step runner a subagent spawn
// wraps, with the smallest valid option set.
func buildSurfaceRunner() (*run.Runner, error) {
	id, err := envelope.New()
	if err != nil {
		return nil, fmt.Errorf("envelope.New: %w", err)
	}
	d, err := flow.New([]flow.Step{{ID: "only", To: "done"}}, nil)
	if err != nil {
		return nil, fmt.Errorf("flow.New: %w", err)
	}
	card := flow.Card{Name: "surface-sub", Capabilities: []string{"surface"}}
	agent, err := workflow.New(id, card, d)
	if err != nil {
		return nil, fmt.Errorf("workflow.New: %w", err)
	}
	m, err := machine.New("start",
		machine.Transition{From: "start", To: "done", Trigger: "go"},
	)
	if err != nil {
		return nil, fmt.Errorf("machine.New: %w", err)
	}
	reg := tools.New()
	if err := reg.Add(stubTool{name: "only"}); err != nil {
		return nil, fmt.Errorf("reg.Add: %w", err)
	}
	return run.New(run.Options{Agent: agent, Machine: m, Tools: reg})
}

// subagentSurface builds every tool the subagent package hands to
// external composition: spawn, mailbox pair, channel ask, provider
// fronts, and the scheduler pair.
func subagentSurface() error {
	box, err := subagent.NewMailbox(8)
	if err != nil {
		return fmt.Errorf("NewMailbox: %w", err)
	}
	_ = subagent.InboxTool("inbox", box)
	id, err := envelope.New()
	if err != nil {
		return fmt.Errorf("envelope.New: %w", err)
	}
	_ = subagent.SendTool("send", box, id)
	ask := func(_ context.Context, _ channel.Question) (channel.Answer, error) {
		return channel.Answer{}, nil
	}
	_ = subagent.ChannelTool("ask", ask, "peer")
	_ = subagent.ProviderTool("provider", stubCompleter{})
	reg := provider.NewRegistry()
	if err := reg.Register("stub", stubCompleter{}); err != nil {
		return fmt.Errorf("Register: %w", err)
	}
	_ = subagent.ProviderRegistryTool("providers", reg, []string{"stub"}, func(error) bool { return false })
	sched := scheduler.New()
	_ = subagent.SchedulerTool("schedule", sched, func(_ context.Context) error { return nil })
	_ = subagent.TriggerTool("trigger", scheduler.NewRegistry())
	runner, err := buildSurfaceRunner()
	if err != nil {
		return err
	}
	_ = subagent.AsTool("sub", runner, subagent.ToolOptions{})
	return nil
}
