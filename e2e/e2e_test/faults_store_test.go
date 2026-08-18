package e2e_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// shipCeremonyTool is a step tool that runs a real ledger ceremony:
// Admit, Claim, then Complete. Its result lands only after the
// ceremony, so a store fault mid-ceremony fails the step.
type shipCeremonyTool struct {
	l *ledger.Ledger
}

// Name returns the registry name.
func (s *shipCeremonyTool) Name() string { return "ship" }

// Run drives one ledger ceremony and names the wrap when a store call
// faults.
func (s *shipCeremonyTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	now := time.Now()
	key := ledger.IdempotencyKey("ship")
	if _, err := s.l.Admit(ctx, "e2e-actor", key, 1, "cargo", now); err != nil {
		return tools.Out{}, err
	}
	fence, err := s.l.Claim(ctx, "e2e-actor", key, "e2e-owner", time.Minute, now)
	if err != nil {
		return tools.Out{}, err
	}
	if err := s.l.Complete(ctx, "e2e-actor", key, "e2e-owner", fence,
		ledger.StatusCompleted, now); err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: "shipped"}, nil
}

// TestFaultStoreMidCeremonyFailsRunAndKeepsStepOneArtifact proves a
// ledger store fault mid-ceremony fails the run, names the fault, and
// leaves step one's artifact in the bag.
func TestFaultStoreMidCeremonyFailsRunAndKeepsStepOneArtifact(t *testing.T) {
	ctx := context.Background()
	artifacts := &agentrun.Artifacts{}

	// The store faults on its fifth call: Admit.Load, Admit.CAS,
	// Claim.Load, Claim.CAS, then Complete.Load.
	store := &e2e.FaultStore{Store: ledger.NewMemStore(), FaultOn: 5}
	l, err := ledger.New(store, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}

	plan, err := flow.New([]flow.Step{
		{ID: "insp", To: "inspected", Payload: "src"},
		{ID: "ship", To: "shipped", Needs: []string{"insp"}, Payload: "cargo"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "inspected", Trigger: "r1"},
		machine.Transition{From: "inspected", To: "shipped", Trigger: "r2"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	addTools(t, reg,
		e2e.PrefixTool{ToolName: "insp", Prefix: "built:"},
		&shipCeremonyTool{l: l},
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "fault-store-agent", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-store", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the store fault")
	}
	if !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("Run error = %v, want e2e.ErrFault", err)
	}
	if !strings.Contains(err.Error(), "store") {
		t.Fatalf("Run error %q does not name the store fault", err)
	}
	if got, ok := artifacts.Get("insp"); !ok || got != "built:src" {
		t.Fatalf("step-one artifact = %q,%v, want built:src present", got, ok)
	}
	if _, ok := artifacts.Get("ship"); ok {
		t.Fatalf("step-two artifact present; the failing step must not confirm")
	}
}

// TestFaultStoreDecoratorFaultsOnTheNthCall covers the Range pass and
// the Load fault paths the ceremony scenario does not reach.
func TestFaultStoreDecoratorFaultsOnTheNthCall(t *testing.T) {
	ctx := context.Background()
	fs := &e2e.FaultStore{Store: ledger.NewMemStore(), FaultOn: 2}
	if _, _, err := fs.Load(ctx, "k"); err != nil {
		t.Fatalf("call 1 Load = %v, want pass-through", err)
	}
	if err := fs.Range(ctx, func(ledger.TaskState) bool { return true }); !errors.Is(err, e2e.ErrFault) {
		t.Fatalf("call 2 Range = %v, want the fault", err)
	}
}
