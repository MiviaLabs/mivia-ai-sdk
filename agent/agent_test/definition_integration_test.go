package agent_test

import (
	"errors"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/discovery"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/identity"
)

// TestNewCrossesIdentityDiscoveryFlow builds a real Identity with
// identity.New, a real Card by struct literal, and a real Definition
// with flow.New over a two-step, no-panel plan. It proves agent.New
// accepts the triple and that Name and Capabilities resolve to the
// card's own values, crossing all three import edges the policy
// declares for agent.
func TestNewCrossesIdentityDiscoveryFlow(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{
		Name:         "Agent A",
		Capabilities: []string{"read", "write"},
	}
	plan, err := flow.New([]flow.Step{
		{ID: "fetch"},
		{ID: "summarize", Needs: []string{"fetch"}},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New() unexpected error: %v", err)
	}

	a, err := agent.New(id, card, plan)
	if err != nil {
		t.Fatalf("agent.New() unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("agent.New() returned a nil Agent, want non-nil")
	}
	if got := a.Name(); got != card.Name {
		t.Fatalf("Name() = %q, want %q", got, card.Name)
	}
	got := a.Capabilities()
	if len(got) != len(card.Capabilities) {
		t.Fatalf("Capabilities() = %v, want %v", got, card.Capabilities)
	}
	for i, c := range card.Capabilities {
		if got[i] != c {
			t.Fatalf("Capabilities()[%d] = %q, want %q", i, got[i], c)
		}
	}
}

// TestFlowNewRejectsCycleBeforeAgentEverRuns feeds flow.New a step
// pair that forms a cycle. flow.New must reject it before agent.New
// ever sees the plan.
func TestFlowNewRejectsCycleBeforeAgentEverRuns(t *testing.T) {
	_, err := flow.New([]flow.Step{
		{ID: "a", Needs: []string{"b"}},
		{ID: "b", Needs: []string{"a"}},
	}, nil)
	if err == nil {
		t.Fatal("flow.New() returned a nil error for a two-step cycle, want error")
	}
}

// TestNewNilPlanIsErrNoPlan feeds agent.New a nil plan directly and
// confirms ErrNoPlan, the path a cycle-rejected flow.New call never
// reaches because it returns before agent.New runs.
func TestNewNilPlanIsErrNoPlan(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatalf("identity.New() unexpected error: %v", err)
	}
	card := discovery.Card{Name: "Agent A", Capabilities: []string{"read"}}
	_, err = agent.New(id, card, nil)
	if !errors.Is(err, agent.ErrNoPlan) {
		t.Fatalf("agent.New() error = %v, want errors.Is match for ErrNoPlan", err)
	}
}
