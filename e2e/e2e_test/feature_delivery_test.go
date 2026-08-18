package e2e_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/e2e"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// implementTool writes one code revision per call.
type implementTool struct {
	calls *int
}

// Name returns the registry name.
func (implementTool) Name() string { return "implement" }

// Run records the revision and returns its label.
func (i implementTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	*i.calls++
	return tools.Out{Value: "code:r" + strconv.Itoa(*i.calls)}, nil
}

// featureDeliveryPlan mirrors the feature-delivery workflow's review
// loop: a develop step looping implement and review until the review
// approves, then an evidence step, then a delivery step that needs a
// human merge approval. The review branch alternates its final so
// the loop's re-entry rows never self-pair.
func featureDeliveryPlan(t *testing.T, script *verdictScript, parity *int) *flow.Definition {
	t.Helper()
	child, err := flow.New([]flow.Step{
		{ID: "implement", To: "coded", Payload: "write-code"},
		{
			ID: "review", To: "reviewing", Needs: []string{"implement"}, Payload: "review-code",
			Route: func(ctx context.Context, cur machine.Status, rec machine.InOut) ([]string, error) {
				*parity++
				if *parity%2 == 1 {
					return []string{"rA"}, nil
				}
				return []string{"rB"}, nil
			},
		},
		{ID: "rA", To: "reviewedA", Needs: []string{"review"}, Payload: "verdict-a"},
		{ID: "rB", To: "reviewedB", Needs: []string{"review"}, Payload: "verdict-b"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New develop child: %v", err)
	}
	untilApproved := func(ctx context.Context) (bool, error) {
		return !strings.Contains(script.last, "approved"), nil
	}
	plan, err := flow.New([]flow.Step{
		{
			ID: "develop", Sub: child, Payload: "develop",
			Loop: &flow.LoopPolicy{Guard: untilApproved, Max: 8},
		},
		{ID: "verify", To: "verified", Needs: []string{"develop"}, Payload: "verify-evidence"},
		{ID: "deliver", To: "delivered", Needs: []string{"verify"}, Payload: "merge the pull request"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New delivery plan: %v", err)
	}
	return plan
}

// featureDeliveryMachine carries the develop loop's child rows, its
// re-entry rows between distinct finals, and the delivery tail.
func featureDeliveryMachine(t *testing.T) *machine.Definition {
	t.Helper()
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "coded", Trigger: "r01"},
		machine.Transition{From: "coded", To: "reviewing", Trigger: "r02"},
		machine.Transition{From: "reviewing", To: "reviewedA", Trigger: "r03"},
		machine.Transition{From: "reviewing", To: "reviewedB", Trigger: "r04"},
		machine.Transition{From: "queued", To: "reviewedA", Trigger: "r05"},
		machine.Transition{From: "queued", To: "reviewedB", Trigger: "r06"},
		machine.Transition{From: "reviewedA", To: "reviewedB", Trigger: "r07"},
		machine.Transition{From: "reviewedB", To: "reviewedA", Trigger: "r08"},
		machine.Transition{From: "reviewedA", To: "verified", Trigger: "r09"},
		machine.Transition{From: "reviewedB", To: "verified", Trigger: "r10"},
		machine.Transition{From: "verified", To: "delivered", Trigger: "r11"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return m
}

// mergeHuman wires one merge-approval round trip over an in-memory
// NDJSON pair. It serves exactly one question: it checks the line,
// then answers with the fixed approval.
func mergeHuman(t *testing.T, approve bool) channel.Notifier {
	t.Helper()
	questionRead, questionWrite := io.Pipe()
	answerRead, answerWrite := io.Pipe()
	go func() {
		defer questionWrite.Close()
		defer answerWrite.Close()
		line, err := bufio.NewReader(questionRead).ReadString('\n')
		if err != nil {
			t.Errorf("maintainer read: %v", err)
			return
		}
		var q wireQuestion
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			t.Errorf("maintainer decode: %v", err)
			return
		}
		if q.Type != "question" || q.ID != "deliver" ||
			q.Recipient != "maintainer-1" || q.Payload != "merge the pull request" {
			t.Errorf("question line = %+v, want the deliver step for maintainer-1", q)
			return
		}
		ans, err := json.Marshal(wireAnswer{
			Type: "answer", QuestionID: q.ID,
			Approved: approve, Payload: "human approved merge",
		})
		if err != nil {
			t.Errorf("maintainer encode: %v", err)
			return
		}
		if _, err := answerWrite.Write(append(ans, '\n')); err != nil {
			t.Errorf("maintainer write: %v", err)
		}
	}()
	return channel.NewNDJSONNotifier(answerRead, questionWrite)
}

// TestFeatureDeliveryReviewLoopThenHumanMerge pins the composite:
// the review loop reworks once, the evidence step runs, and the
// delivery escalates to a human whose approval completes the run.
func TestFeatureDeliveryReviewLoopThenHumanMerge(t *testing.T) {
	script := &verdictScript{verdicts: []string{"changes_requested", "approved"}}
	parity, implementCalls := 0, 0
	plan := featureDeliveryPlan(t, script, &parity)
	m := featureDeliveryMachine(t)
	if err := agentrun.ValidateMatrix(plan, m); err != nil {
		t.Fatalf("ValidateMatrix = %v, want nil", err)
	}
	artifacts := &agentrun.Artifacts{}
	reg := tools.New()
	addTools(t, reg,
		implementTool{calls: &implementCalls},
		verdictTool{name: "review", script: script},
		e2e.PrefixTool{ToolName: "rA", Prefix: "settled:"},
		e2e.PrefixTool{ToolName: "rB", Prefix: "settled:"},
		e2e.PrefixTool{ToolName: "develop", Prefix: "developed:"},
		e2e.PrefixTool{ToolName: "verify", Prefix: "verified:"},
		e2e.EscalateTool{ToolName: "deliver"},
	)
	runner, err := agentrun.New(agentrun.Options{
		Agent: e2eAgent(t, "feature-agent", plan), Machine: m,
		Tools: reg, Artifacts: artifacts,
		Ask: mergeHuman(t, true), AskTo: "maintainer-1",
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-feature", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != "delivered" {
		t.Fatalf("status = %q, want %q", status, "delivered")
	}
	if implementCalls != 2 {
		t.Errorf("implement runs = %d, want 2 (one rework)", implementCalls)
	}
	if got, _ := artifacts.Get("implement#2"); got != "code:r2" {
		t.Errorf("second implementation = %q, want code:r2 under the suffixed ID", got)
	}
	if got, _ := artifacts.Get("verify"); got != "verified:verify-evidence" {
		t.Errorf("verify artifact = %q, want the evidence", got)
	}
	if script.last != "approved" {
		t.Errorf("final review verdict = %q, want approved", script.last)
	}
}
