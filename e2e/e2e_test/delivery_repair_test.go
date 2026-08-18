package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// prHostTool mirrors the delivery endpoint: it opens the pull request
// when the title passes the policy, and reports a rejection as
// output. A stubborn host rejects every title, driving the run into
// the repair budget.
type prHostTool struct {
	title     *string
	attempts  *int
	stubborn  bool
	last      *string
	rejectMsg string
}

// Name returns the registry name.
func (prHostTool) Name() string { return "deliver" }

// Run opens the pull request or reports the rejection as output.
func (p prHostTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*p.attempts++
	if p.stubborn {
		*p.last = "pr-rejected:" + p.rejectMsg
		return tools.Out{Value: *p.last}, nil
	}
	if !strings.Contains(*p.title, ": ") {
		*p.last = "pr-rejected: title \"" + *p.title + "\" missing scope"
		return tools.Out{Value: *p.last}, nil
	}
	*p.last = "pr-opened:" + *p.title
	return tools.Out{Value: *p.last}, nil
}

// metadataRepairTool mirrors the dedicated metadata repair step: it
// rewrites the title from the rejection hint. Once its budget is
// spent it fails hard, which settles the run terminal, the engine's
// delivery_failed outcome.
type metadataRepairTool struct {
	title  *string
	calls  *int
	budget int
}

// Name returns the registry name.
func (metadataRepairTool) Name() string { return "repair_metadata" }

// Run fixes the title or fails once the budget is spent.
func (m metadataRepairTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*m.calls++
	if *m.calls > m.budget {
		return tools.Out{}, fmt.Errorf("metadata repair budget exhausted after %d repairs", m.budget)
	}
	*m.title = "fix: " + strings.ToLower(*m.title)
	return tools.Out{Value: "title-fixed"}, nil
}

// deliveryPlan wires the delivery repair loop: the deliver step's
// route picks the metadata repair on rejection and the open note on
// success, and the parent loop re-enters delivery until the request
// opens.
func deliveryPlan(t *testing.T, last *string) (*flow.Definition, *machine.Definition) {
	t.Helper()
	child, err := flow.New([]flow.Step{
		{
			ID: "deliver", To: "delivering", Payload: "open-pr",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				if strings.Contains(*last, "pr-opened") {
					return []string{"pr_open"}, nil
				}
				return []string{"repair_metadata"}, nil
			},
		},
		{ID: "repair_metadata", To: "meta-fixed", Needs: []string{"deliver"}, Payload: "fix-title"},
		{ID: "pr_open", To: "opened", Needs: []string{"deliver"}, Payload: "open-note",
			When: flow.AdmissionOnSucceeded},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New delivery child: %v", err)
	}
	untilOpened := func(ctx context.Context) (bool, error) {
		return !strings.Contains(*last, "pr-opened"), nil
	}
	plan, err := flow.New([]flow.Step{
		{
			ID: "ship_pr", Sub: child, Payload: "ship",
			Loop: &flow.LoopPolicy{Guard: untilOpened, Max: 5},
		},
		{ID: "done", To: "delivered", Needs: []string{"ship_pr"}, Payload: "delivery-done"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New delivery plan: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "delivering", Trigger: "r01"},
		machine.Transition{From: "delivering", To: "meta-fixed", Trigger: "r02"},
		machine.Transition{From: "delivering", To: "opened", Trigger: "r03"},
		machine.Transition{From: "queued", To: "meta-fixed", Trigger: "r04"},
		machine.Transition{From: "queued", To: "opened", Trigger: "r05"},
		machine.Transition{From: "meta-fixed", To: "opened", Trigger: "r06"},
		machine.Transition{From: "opened", To: "meta-fixed", Trigger: "r07"},
		machine.Transition{From: "opened", To: "delivered", Trigger: "r08"},
		machine.Transition{From: "meta-fixed", To: "delivered", Trigger: "r09"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return plan, m
}

// runDelivery drives one delivery scenario and returns the run
// status, artifacts, and error.
func runDelivery(t *testing.T, stubborn bool, budget int) (machine.Status, *agentrun.Artifacts, error) {
	t.Helper()
	title := "bug fix"
	attempts, repairs := 0, 0
	last := "unrun"
	plan, m := deliveryPlan(t, &last)
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}
	artifacts := &agentrun.Artifacts{}
	reg := tools.New()
	addTools(t, reg,
		prHostTool{title: &title, attempts: &attempts, stubborn: stubborn, last: &last,
			rejectMsg: "pr-title policy violated"},
		metadataRepairTool{title: &title, calls: &repairs, budget: budget},
		e2e.PrefixTool{ToolName: "pr_open", Prefix: "opened:"},
		e2e.PrefixTool{ToolName: "ship_pr", Prefix: "shipped:"},
		e2e.PrefixTool{ToolName: "done", Prefix: "done:"},
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "delivery-agent", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-delivery", machine.InOut{})
	if !stubborn && err != nil {
		t.Fatalf("Run: %v", err)
	}
	return status, artifacts, err
}

// TestDeliveryMetadataRepairLoopRecovers pins the metadata repair
// path: the first title is rejected, the repair rewrites it from the
// hint, and the second delivery attempt opens the pull request.
func TestDeliveryMetadataRepairLoopRecovers(t *testing.T) {
	status, artifacts, err := runDelivery(t, false, 3)
	if err != nil {
		t.Fatalf("runDelivery: %v", err)
	}
	if status != "delivered" {
		t.Fatalf("status = %q, want %q", status, "delivered")
	}
	got, ok := artifacts.Get("deliver")
	if !ok || !strings.Contains(got, "pr-opened:fix: bug fix") {
		t.Errorf("delivery artifact = %q,%v, want the latest result, the opened PR", got, ok)
	}
	runs := artifacts.History("deliver")
	if len(runs) != 2 ||
		!strings.Contains(runs[0].Value, "pr-rejected") ||
		!strings.Contains(runs[1].Value, "pr-opened") {
		t.Errorf("delivery history = %+v, want the rejection then the opening", runs)
	}
	if runs[1].MessageID != "deliver#2" {
		t.Errorf("second run message ID = %q, want the signed counter ID", runs[1].MessageID)
	}
	if _, ok := artifacts.Get("repair_metadata"); !ok {
		t.Error("the metadata repair never ran")
	}
}

// TestDeliveryBudgetExhaustedFailsTerminal pins the terminal edge:
// a stubborn host outlasts a one-repair budget, the second repair
// fails hard, and the run settles terminal with the budget note. A
// longer budget cannot be expressed here: every stubborn iteration
// ends on the same final, and a same-final re-entry row is
// unrepresentable.
func TestDeliveryBudgetExhaustedFailsTerminal(t *testing.T) {
	_, _, err := runDelivery(t, true, 1)
	if err == nil {
		t.Fatal("Run succeeded, want the budget-exhausted failure")
	}
	if !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("Run error %q lacks the budget note", err)
	}
}
