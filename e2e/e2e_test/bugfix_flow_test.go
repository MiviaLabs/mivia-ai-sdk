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

// verdictScript hands out scripted verdicts in order and remembers the
// last one. Loop guards and routes read script state, not artifact
// keys: a step repeated inside a looped child records its later
// results under #N-suffixed message IDs, so the unsuffixed key keeps
// the first iteration's verdict forever.
type verdictScript struct {
	verdicts []string
	calls    int
	last     string
}

// next returns the next verdict or fails once the script runs out.
func (s *verdictScript) next() (string, error) {
	if s.calls >= len(s.verdicts) {
		return "", fmt.Errorf("verdict script exhausted after %d calls", len(s.verdicts))
	}
	s.last = s.verdicts[s.calls]
	s.calls++
	return s.last, nil
}

// verdictTool answers one scripted verdict per call.
type verdictTool struct {
	name   string
	script *verdictScript
}

// Name returns the registry name.
func (v verdictTool) Name() string { return v.name }

// Run returns the next scripted verdict.
func (v verdictTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	verdict, err := v.script.next()
	if err != nil {
		return tools.Out{}, err
	}
	return tools.Out{Value: "verdict:" + verdict}, nil
}

// bugfixAuditPlan mirrors the bug-fix workflow's hunt-and-triage core:
// a bounded refinement loop around hunt and triage, then a gate step
// routing on the triage verdict. The child ends every iteration on
// one final; the equal-standing re-entry needs no self-row.
func bugfixAuditPlan(t *testing.T, script *verdictScript) *flow.Definition {
	t.Helper()
	child, err := flow.New([]flow.Step{
		{ID: "hunt", To: "hunted", Payload: "hunt-scope"},
		{ID: "triage", To: "triaged", Needs: []string{"hunt"}, Payload: "triage-findings"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New audit child: %v", err)
	}
	refine := func(ctx context.Context) (bool, error) {
		return strings.Contains(script.last, "insufficient"), nil
	}
	plan, err := flow.New([]flow.Step{
		{
			ID: "audit", Sub: child, Payload: "audit",
			Loop: &flow.LoopPolicy{Guard: refine, Max: 5},
		},
		{
			ID: "gate", To: "gated", Needs: []string{"audit"}, Payload: "route-verdict",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				switch {
				case strings.Contains(script.last, "confirmed"):
					return []string{"fix_plan"}, nil
				case strings.Contains(script.last, "no_bug"):
					return []string{"clean_exit"}, nil
				}
				return nil, fmt.Errorf("unknown triage verdict %q", script.last)
			},
		},
		{ID: "fix_plan", To: "planned", Needs: []string{"gate"}, Payload: "plan-the-fix",
			When: flow.AdmissionOnSucceeded},
		{ID: "implement", To: "implemented", Needs: []string{"fix_plan"}, Payload: "ship-the-fix",
			When: flow.AdmissionOnSucceeded},
		{ID: "clean_exit", To: "clean", Needs: []string{"gate"}, Payload: "no-diff-close",
			When: flow.AdmissionOnSucceeded},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New audit plan: %v", err)
	}
	return plan
}

// bugfixAuditMachine carries every row the audit walk can fire: the
// child rows, the parent's first fire, and both gate branches. The
// same-final re-entry needs no row.
func bugfixAuditMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "hunted", Trigger: "r01"},
		machine.Transition{From: "hunted", To: "triaged", Trigger: "r02"},
		machine.Transition{From: "queued", To: "triaged", Trigger: "r03"},
		machine.Transition{From: "triaged", To: "gated", Trigger: "r04"},
		machine.Transition{From: "gated", To: "planned", Trigger: "r05"},
		machine.Transition{From: "planned", To: "implemented", Trigger: "r06"},
		machine.Transition{From: "gated", To: "clean", Trigger: "r07"},
		// The validator walks branch siblings in declaration order, so
		// the clean-exit branch needs a row from the fix path's final.
		machine.Transition{From: "implemented", To: "clean", Trigger: "r08"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestBugfixTriageRoutes pins the triage gate's three-way contract:
// confirmed drives the fix path, no_bug exits clean, and
// insufficient_evidence re-enters the hunt once before confirming.
func TestBugfixTriageRoutes(t *testing.T) {
	cases := []struct {
		name      string
		verdicts  []string
		wantFinal string
		wantHunt  int
		onFixPath bool
	}{
		{name: "confirmed first pass", verdicts: []string{"confirmed"}, wantFinal: "implemented", wantHunt: 1, onFixPath: true},
		{name: "no bug exits clean", verdicts: []string{"no_bug"}, wantFinal: "clean", wantHunt: 1},
		{name: "insufficient refines once", verdicts: []string{"insufficient", "confirmed"}, wantFinal: "implemented", wantHunt: 2, onFixPath: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script := &verdictScript{verdicts: tc.verdicts}
			hunt := 0
			plan := bugfixAuditPlan(t, script)
			m := bugfixAuditMachine(t)
			if err := agentrun.ValidateMatrix(plan, m); err != nil {
				t.Fatalf("ValidateMatrix = %v, want nil", err)
			}
			reg := tools.New()
			addTools(t, reg,
				huntTool{calls: &hunt},
				verdictTool{name: "triage", script: script},
				e2e.PrefixTool{ToolName: "audit", Prefix: "audit:"},
				e2e.PrefixTool{ToolName: "gate", Prefix: "gate:"},
				e2e.PrefixTool{ToolName: "fix_plan", Prefix: "plan:"},
				e2e.PrefixTool{ToolName: "implement", Prefix: "fix:"},
				e2e.PrefixTool{ToolName: "clean_exit", Prefix: "clean:"},
			)
			artifacts := &agentrun.Artifacts{}
			runner, err := agentrun.New(agentrun.Options{
				Agent: e2eAgent(t, "bugfix-audit", plan), Machine: m,
				Tools: reg, Artifacts: artifacts,
			})
			if err != nil {
				t.Fatalf("agentrun.New: %v", err)
			}
			status, _, err := runner.Run(context.Background(), "thread-bugfix", machine.InOut{})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if status != machine.Status(tc.wantFinal) {
				t.Fatalf("status = %q, want %q", status, tc.wantFinal)
			}
			if hunt != tc.wantHunt {
				t.Errorf("hunt runs = %d, want %d", hunt, tc.wantHunt)
			}
			if got, ok := artifacts.Get("fix_plan"); tc.onFixPath && (!ok || got != "plan:plan-the-fix") {
				t.Errorf("fix_plan artifact = %q,%v want the plan", got, ok)
			}
			if _, ok := artifacts.Get("clean_exit"); tc.onFixPath && ok {
				t.Errorf("clean_exit ran on the fix path; the route must exclude it")
			}
		})
	}
}

// huntTool counts hunts and reports one fixed finding.
type huntTool struct {
	calls *int
}

// Name returns the registry name.
func (huntTool) Name() string { return "hunt" }

// Run counts the hunt and returns its finding.
func (h huntTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*h.calls++
	return tools.Out{Value: "found:off-by-one"}, nil
}

// evidenceGateTool reports failure while no repair has landed, then
// passes. Its shared state feeds the loop guard.
type evidenceGateTool struct {
	calls *int
	fixes *int
	last  *string
}

// Name returns the registry name.
func (evidenceGateTool) Name() string { return "test_validate" }

// Run reports failure before any repair, passes after one.
func (e evidenceGateTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*e.calls++
	if *e.fixes == 0 {
		*e.last = "evidence:failed"
	} else {
		*e.last = "evidence:passed"
	}
	return tools.Out{Value: *e.last}, nil
}

// repairTool lands one fix per call.
type repairTool struct {
	fixes *int
}

// Name returns the registry name.
func (repairTool) Name() string { return "repair_tests" }

// Run records the repair and returns its result.
func (r repairTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*r.fixes++
	return tools.Out{Value: "repaired:off-by-one"}, nil
}

// bugfixRepairMachine carries the repair child's rows, the loop's
// re-entry rows between distinct finals, and both ship rows.
func bugfixRepairMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "checking", Trigger: "r01"},
		machine.Transition{From: "checking", To: "repaired", Trigger: "r02"},
		machine.Transition{From: "checking", To: "passed", Trigger: "r03"},
		machine.Transition{From: "queued", To: "repaired", Trigger: "r04"},
		machine.Transition{From: "queued", To: "passed", Trigger: "r05"},
		machine.Transition{From: "repaired", To: "passed", Trigger: "r06"},
		machine.Transition{From: "passed", To: "repaired", Trigger: "r07"},
		machine.Transition{From: "passed", To: "shipped", Trigger: "r08"},
		machine.Transition{From: "repaired", To: "shipped", Trigger: "r09"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// TestBugfixEvidenceRepairLoop pins the evidence-gate contract: the
// gate reports its first validation as failed output, its route runs
// the repair, and the loop re-enters the gate once, which then
// passes and ships. An ack rejection stays fatal, so a gate that
// must route to repair reports failure as output, never error.
func TestBugfixEvidenceRepairLoop(t *testing.T) {
	artifacts := &agentrun.Artifacts{}
	fixes, gateCalls := 0, 0
	lastVerdict := "evidence:unrun"
	child, err := flow.New([]flow.Step{
		{
			ID: "test_validate", To: "checking", Payload: "run-suite",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				if lastVerdict == "evidence:passed" {
					return []string{"evidence_ok"}, nil
				}
				return []string{"repair_tests"}, nil
			},
		},
		{ID: "repair_tests", To: "repaired", Needs: []string{"test_validate"}, Payload: "repair-suite"},
		{ID: "evidence_ok", To: "passed", Needs: []string{"test_validate"}, Payload: "pass-note",
			When: flow.AdmissionOnSucceeded},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New repair child: %v", err)
	}
	untilPass := func(ctx context.Context) (bool, error) {
		return lastVerdict != "evidence:passed", nil
	}
	plan, err := flow.New([]flow.Step{
		{
			ID: "validate", Sub: child, Payload: "validate",
			Loop: &flow.LoopPolicy{Guard: untilPass, Max: 5},
		},
		{ID: "ship", To: "shipped", Needs: []string{"validate"}, Payload: "ship-fix"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New repair plan: %v", err)
	}
	m := bugfixRepairMachine(t)
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}
	reg := tools.New()
	addTools(t, reg,
		evidenceGateTool{calls: &gateCalls, fixes: &fixes, last: &lastVerdict},
		repairTool{fixes: &fixes},
		e2e.PrefixTool{ToolName: "evidence_ok", Prefix: "ok:"},
		e2e.PrefixTool{ToolName: "validate", Prefix: "validated:"},
		e2e.PrefixTool{ToolName: "ship", Prefix: "ship:"},
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "bugfix-repair", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-repair", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "shipped" {
		t.Fatalf("status = %q, want %q", status, "shipped")
	}
	if gateCalls != 2 || fixes != 1 {
		t.Fatalf("gate calls = %d, repairs = %d, want 2 and 1", gateCalls, fixes)
	}
	if got, ok := artifacts.Get("test_validate"); !ok || got != "evidence:passed" {
		t.Errorf("gate artifact = %q,%v, want the latest result, the pass", got, ok)
	}
}
