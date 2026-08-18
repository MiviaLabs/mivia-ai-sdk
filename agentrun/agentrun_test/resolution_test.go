package agentrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// bigPanelSubPlan builds a plan with a two-member panel whose members
// are never Confirm-gated, plus a Sub step whose child holds one inner
// step, and a tail step that needs the Sub. Only sub1, inner, and tail
// are Confirm-gated; the panel members a and b are not.
func bigPanelSubPlan(t *testing.T) *flow.Definition {
	t.Helper()
	child := mustFlow(t, []flow.Step{{ID: "inner", To: "cs"}}, nil)
	return mustFlow(t, []flow.Step{
		{ID: "a", To: "w"},
		{ID: "b", To: "w"},
		{ID: "sub1", Sub: child, To: "pt"},
		{ID: "tail", To: "done", Needs: []string{"sub1"}},
	}, []flow.Panel{{"a", "b"}})
}

// bigPanelSubMachine satisfies the matrix for bigPanelSubPlan.
func bigPanelSubMachine(t *testing.T) *machine.Definition {
	t.Helper()
	return mustMachine(t, "queued",
		tr("queued", "w", "p"),
		tr("queued", "cs", "s"),
		tr("cs", "done", "t"),
	)
}

// gatedOnlyRegistry registers tools for the Confirm-gated steps only:
// sub1, inner, and tail. It omits the big-panel members a and b.
func gatedOnlyRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.New()
	addTools(t, reg,
		prefixTool{name: "sub1", prefix: "s:"},
		prefixTool{name: "inner", prefix: "i:"},
		prefixTool{name: "tail", prefix: "t:"},
	)
	return reg
}

// TestNewSkipsBigPanelMembers proves New does not require a registered
// tool for a step in a two-or-more-member panel, because flow.Run never
// calls Confirm for such a wave. a and b carry no tool here; New must
// still succeed.
func TestNewSkipsBigPanelMembers(t *testing.T) {
	_, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, bigPanelSubPlan(t)),
		Machine: bigPanelSubMachine(t),
		Tools:   gatedOnlyRegistry(t),
	})
	if err != nil {
		t.Fatalf("New = %v, want success: big-panel members must be skipped", err)
	}
}

// TestNewRequiresSubChildTools proves New recurses into a Sub child and
// requires the inner step's tool. Omitting inner makes New fail.
func TestNewRequiresSubChildTools(t *testing.T) {
	reg := tools.New()
	addTools(t, reg,
		prefixTool{name: "sub1", prefix: "s:"},
		prefixTool{name: "tail", prefix: "t:"},
	)
	_, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, bigPanelSubPlan(t)),
		Machine: bigPanelSubMachine(t),
		Tools:   reg,
	})
	if !errors.Is(err, tools.ErrUnknownName) {
		t.Fatalf("New = %v, want ErrUnknownName for the missing inner tool", err)
	}
}

// TestNewRequiresGatedTailTool proves a Confirm-gated non-panel step
// still needs its tool; omitting tail fails New.
func TestNewRequiresGatedTailTool(t *testing.T) {
	reg := tools.New()
	addTools(t, reg,
		prefixTool{name: "sub1", prefix: "s:"},
		prefixTool{name: "inner", prefix: "i:"},
	)
	_, err := agentrun.New(agentrun.Options{
		Agent:   mustAgent(t, bigPanelSubPlan(t)),
		Machine: bigPanelSubMachine(t),
		Tools:   reg,
	})
	if !errors.Is(err, tools.ErrUnknownName) {
		t.Fatalf("New = %v, want ErrUnknownName for the missing tail tool", err)
	}
}

// TestNewReceiverOverride proves a set Options.Receiver is accepted and
// the run still completes. The receiver's signer hex becomes the ack
// From; the branch is exercised here even though From is not observable
// through the public surface.
func TestNewReceiverOverride(t *testing.T) {
	receiver, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent:    mustAgent(t, oneStepPlan(t)),
		Machine:  oneStepMachine(t),
		Tools:    oneStepRegistry(t),
		Receiver: receiver,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-recv", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "resolved" {
		t.Fatalf("status = %q, want %q", status, "resolved")
	}
}
