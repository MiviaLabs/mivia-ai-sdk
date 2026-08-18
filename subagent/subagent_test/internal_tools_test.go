package subagent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/events"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/ledger"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/memory"
	"github.com/MiviaLabs/mivia-ai-sdk/subagent"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// twoStepPlanMachine returns a plan and machine for FlowTool tests.
func twoStepPlanMachine(t *testing.T) (*flow.Definition, *machine.Definition) {
	t.Helper()
	plan, err := flow.New([]flow.Step{
		{ID: "draft", To: "drafted", Payload: "a"},
		{ID: "send", To: "sent", Needs: []string{"draft"}, Payload: "b"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "drafted", Trigger: "t1"},
		machine.Transition{From: "drafted", To: "sent", Trigger: "t2"},
	)
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	return plan, m
}

// TestFlowToolRunsPlanAndReportsStatus proves the flow tool drives a
// real walk and reports the final status.
func TestFlowToolRunsPlanAndReportsStatus(t *testing.T) {
	ctx := context.Background()
	plan, m := twoStepPlanMachine(t)
	out, err := subagent.FlowTool("flow", plan, m, nil).Run(ctx, inString("payload"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != "sent" {
		t.Fatalf("result = %v, want %q", out.Value, "sent")
	}
}

// TestFlowToolEmitsStepEvents proves an observing bus sees one step
// event per step.
func TestFlowToolEmitsStepEvents(t *testing.T) {
	ctx := context.Background()
	plan, m := twoStepPlanMachine(t)
	bus := events.New()
	seen := 0
	if err := bus.Subscribe(flow.StepCompletedEvent, func(context.Context, events.Event) error {
		seen++
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, err := subagent.FlowTool("flow", plan, m, bus).Run(ctx, inString("p")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if seen != 2 {
		t.Fatalf("step events = %d, want 2", seen)
	}
}

// TestLedgerToolRunAndState proves the run command completes the
// ceremony and the state command reports the record.
func TestLedgerToolRunAndState(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	tool := subagent.LedgerTool("ledger", l, "sub-actor", time.Minute)

	run, err := json.Marshal(subagent.LedgerCommand{
		Op: subagent.OpRun, Key: "job-1", Seq: 1, Description: "work",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := tool.Run(ctx, inString(string(run)))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Value != string(ledger.StatusCompleted) {
		t.Fatalf("run result = %v, want completed", out.Value)
	}

	query, err := json.Marshal(subagent.LedgerCommand{Op: subagent.OpState, Key: "job-1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err = tool.Run(ctx, inString(string(query)))
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if out.Value != string(ledger.StatusCompleted) {
		t.Fatalf("state result = %v, want completed", out.Value)
	}
}

// TestLedgerToolRunBlockedFails proves a run against a failed
// dependency reports the blocked sentinel.
func TestLedgerToolRunBlockedFails(t *testing.T) {
	ctx := context.Background()
	l, err := ledger.New(nil, nil)
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	now := time.Now()
	if _, err := l.Admit(ctx, "sub-actor", "dep", 1, nil, now); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	fence, err := l.Claim(ctx, "sub-actor", "dep", "sub-owner", time.Minute, now)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := l.Complete(ctx, "sub-actor", "dep", "sub-owner", fence, ledger.StatusFailed, now); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	tool := subagent.LedgerTool("ledger", l, "sub-actor", time.Minute)
	cmd, _ := json.Marshal(subagent.LedgerCommand{
		Op: subagent.OpRun, Key: "child", Seq: 1, Needs: []ledger.IdempotencyKey{"dep"},
	})
	_, err = tool.Run(ctx, inString(string(cmd)))
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("err = %v, want the blocked sentinel", err)
	}
}

// TestMemoryToolPutGet proves the put command returns a ref and the
// get command returns the bytes.
func TestMemoryToolPutGet(t *testing.T) {
	ctx := context.Background()
	store, err := memory.New(1024)
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	tool := subagent.MemoryTool("memory", store)

	put, _ := json.Marshal(subagent.MemoryCommand{Op: subagent.OpPut, Data: "blob-bytes"})
	out, err := tool.Run(ctx, inString(string(put)))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if out.Value == "" || len(out.Value.(string)) == 0 {
		t.Fatalf("put result = %v, want a ref", out.Value)
	}

	get, _ := json.Marshal(subagent.MemoryCommand{Op: subagent.OpGet, Ref: out.Value.(string)})
	out, err = tool.Run(ctx, inString(string(get)))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.Value != "blob-bytes" {
		t.Fatalf("get result = %v, want the blob", out.Value)
	}
}

// TestMemoryToolBadCommandFails proves a malformed command fails with
// the sentinel, not a panic.
func TestMemoryToolBadCommandFails(t *testing.T) {
	ctx := context.Background()
	store, _ := memory.New(64)
	_, err := subagent.MemoryTool("memory", store).Run(ctx, inString("not json"))
	if err == nil || !strings.Contains(err.Error(), subagent.ErrBadCommand.Error()) {
		t.Fatalf("err = %v, want the bad-command sentinel", err)
	}
}

// inString wraps s as a tool input value.
func inString(s string) tools.InOut {
	return tools.InOut{Value: s}
}
