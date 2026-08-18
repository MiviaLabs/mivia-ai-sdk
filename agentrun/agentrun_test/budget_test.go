package agentrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/contextbudget"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestBudgetTripsSecondStep proves a Limits value that fails on step
// two returns agent.ErrOverBudget and confirms no ack for that step.
func TestBudgetTripsSecondStep(t *testing.T) {
	ctx := context.Background()
	artifacts := &agentrun.Artifacts{}
	plan := mustFlow(t, []flow.Step{
		{ID: "review", To: "reviewed", Payload: "seed1"},
		{ID: "ship", To: "shipped", Needs: []string{"review"}, Payload: "ABCDEFGHIJKLMNOPQRST"},
	}, nil)
	reg := tools.New()
	addTools(t, reg,
		prefixTool{name: "review", prefix: "reviewed:"},
		prefixTool{name: "ship", prefix: "shipped:"},
	)
	m := mustMachine(t, "queued",
		tr("queued", "reviewed", "run"),
		tr("reviewed", "shipped", "run"),
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent:     mustAgent(t, plan),
		Machine:   m,
		Tools:     reg,
		Artifacts: artifacts,
		Budget:    &contextbudget.Limits{MaxBytes: 10},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	counter := &eventCounter{}
	runner.Bus().Subscribe(agent.MessageAckedEvent, counter.handler())

	_, _, err = runner.Run(ctx, "thread-budget", machine.InOut{})
	if !errors.Is(err, agent.ErrOverBudget) {
		t.Fatalf("Run error = %v, want agent.ErrOverBudget", err)
	}

	// Step one confirmed; step two never reached its ack.
	if got := counter.count(agent.MessageAckedEvent); got != 1 {
		t.Errorf("acked events = %d, want 1", got)
	}
	if _, ok := artifacts.Get("ship"); ok {
		t.Errorf("ship artifact set; step two must have no ack")
	}
	if v, ok := artifacts.Get("review"); !ok || v != "reviewed:seed1" {
		t.Errorf("artifact review = %q,%v want reviewed:seed1,true", v, ok)
	}
}

// TestRunValidBudgetProves a passing budget leaves the run unchanged.
func TestRunValidBudget(t *testing.T) {
	ctx := context.Background()
	plan := mustFlow(t, []flow.Step{{ID: "t1", To: "resolved", Payload: "seed"}}, nil)
	runner, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, plan),
		Machine: oneStepMachine(t),
		Tools:   oneStepRegistry(t),
		Budget:  &contextbudget.Limits{MaxBytes: 1024},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, _, err := runner.Run(ctx, "thread-budget-ok", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("status = %q, want %q", status, "resolved")
	}
}
