package e2e_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// TestFaultStoreHangSurfacesDeadline proves a ledger store that hangs
// on its first call fails the run once the run ctx's deadline fires,
// instead of hanging the run forever. This mirrors
// TestFaultHangCompleterSurfacesTimeout for the FaultStore seam.
func TestFaultStoreHangSurfacesDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	artifacts := &agentrun.Artifacts{}

	// The store hangs on its first call: Admit.Load.
	store := &e2e.FaultStore{Store: ledger.NewMemStore(), HangOn: 1}
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
		Agent: e2eAgent(t, "fault-store-hang-agent", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}

	_, _, err = runner.Run(ctx, "thread-store-hang", machine.InOut{})
	if err == nil {
		t.Fatal("Run succeeded, want the deadline to surface")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run error = %v, want it to wrap context.DeadlineExceeded", err)
	}
}
