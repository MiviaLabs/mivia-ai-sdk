package workflow_test

import (
	"errors"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/workflow"
)

// TestNewCrossesIdentityDiscoveryFlow builds a real Identity with
// envelope.New, a real Card by struct literal, and a real Definition
// with flow.New over a two-step, no-panel plan. It proves workflow.New
// accepts the triple and that Name and Capabilities resolve to the
// card's own values, crossing all three import edges the policy
// declares for workflow.
func TestNewCrossesIdentityDiscoveryFlow(t *testing.T) {
	id, err := envelope.New()
	if err != nil {
		t.Fatalf("envelope.New() unexpected error: %v", err)
	}
	card := flow.Card{
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

	a, err := workflow.New(id, card, plan)
	if err != nil {
		t.Fatalf("workflow.New() unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("workflow.New() returned a nil Agent, want non-nil")
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
// pair that forms a cycle. flow.New must reject it before workflow.New
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

// TestNewNilPlanIsErrNoPlan feeds workflow.New a nil plan directly and
// confirms ErrInvalidOptions naming the plan field, the path a
// cycle-rejected flow.New call never reaches because it returns
// before workflow.New runs.
func TestNewNilPlanIsErrNoPlan(t *testing.T) {
	id, err := envelope.New()
	if err != nil {
		t.Fatalf("envelope.New() unexpected error: %v", err)
	}
	card := flow.Card{Name: "Agent A", Capabilities: []string{"read"}}
	_, err = workflow.New(id, card, nil)
	if !errors.Is(err, workflow.ErrInvalidOptions) || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("workflow.New() error = %v, want ErrInvalidOptions naming plan", err)
	}
}
