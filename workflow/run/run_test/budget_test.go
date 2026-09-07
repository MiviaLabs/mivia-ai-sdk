package run_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/context/budget"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow/run"
)

// TestBudgetTripsSecondStep proves a Limits value that fails on step
// two returns workflow.ErrOverBudget and confirms no ack for that step.
func TestBudgetTripsSecondStep(t *testing.T) {
	ctx := context.Background()
	artifacts := &run.Artifacts{}
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
	runner, err := run.New(run.Options{
		Agent:     mustAgent(t, plan),
		Machine:   m,
		Tools:     reg,
		Artifacts: artifacts,
		Budget:    &budget.Limits{MaxBytes: 10},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	counter := &eventCounter{}
	runner.Bus().Subscribe(workflow.MessageAckedEvent, counter.handler())

	_, _, err = runner.Run(ctx, "thread-budget", machine.InOut{})
	if !errors.Is(err, workflow.ErrOverBudget) {
		t.Fatalf("Run error = %v, want workflow.ErrOverBudget", err)
	}

	// Step one confirmed; step two never reached its ack.
	if got := counter.count(workflow.MessageAckedEvent); got != 1 {
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
	runner, err := run.New(run.Options{
		Agent:   mustAgent(t, plan),
		Machine: oneStepMachine(t),
		Tools:   oneStepRegistry(t),
		Budget:  &budget.Limits{MaxBytes: 1024},
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
