package agentloop_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MiviaLabs/mivia-ai-sdk/agentloop"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/provider"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// blockingTool is a SchemaTool whose Run signals entry once, then
// blocks on release until the test closes it.
type blockingTool struct {
	name    string
	entered chan struct{}
	release chan struct{}
	runs    atomic.Int64
}

func (t *blockingTool) Name() string { return t.name }

func (t *blockingTool) ParameterSchema() []byte { return []byte(`{}`) }

func (t *blockingTool) DecodeArguments(raw []byte) (tools.InOut, error) {
	return tools.InOut{}, nil
}

func (t *blockingTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	t.runs.Add(1)
	close(t.entered)
	<-t.release
	return tools.Out{Value: t.name}, nil
}

// TestVetoWhileLaterCallInFlightStillRecordsOutcome proves the
// parallel dispatch path records every call that already ran when an
// earlier-index veto stops the batch. A veto prevents later calls
// from starting, but a call already claimed and in flight when the
// veto lands runs to completion. Discarding its outcome and audit
// record hides an executed side effect, so the collect pass must
// append and audit it before the short-circuit return.
func TestVetoWhileLaterCallInFlightStillRecordsOutcome(t *testing.T) {
	slow := &blockingTool{name: "slow", entered: make(chan struct{}), release: make(chan struct{})}
	reg := tools.New()
	mustAdd(t, reg, &schemaEchoTool{name: "gate", schema: []byte(`{}`)})
	mustAdd(t, reg, slow)

	hreg := hooks.New()
	vetoed := make(chan struct{})
	var vetoOnce sync.Once
	if err := hreg.Add(hooks.PointPreTool, "veto-gate", func(ctx context.Context, payload any) (bool, error) {
		call, ok := payload.(provider.ToolCall)
		if !ok || call.Name != "gate" {
			return true, nil
		}
		<-slow.entered
		vetoOnce.Do(func() { close(vetoed) })
		return false, nil
	}); err != nil {
		t.Fatalf("hook Add: %v", err)
	}

	auditor := &recordingAuditor{}
	completer := &scriptedCompleter{responses: []provider.Response{
		toolCallResponse(
			provider.ToolCall{ID: "call-gate", Name: "gate", Index: 0, Arguments: []byte("{}")},
			provider.ToolCall{ID: "call-slow", Name: "slow", Index: 1, Arguments: []byte("{}")},
		),
		{Message: textMessage(provider.RoleAssistant, "final")},
	}}
	loop, err := agentloop.New(agentloop.Options{
		Completer: completer, Tools: reg, MaxIterations: 5,
		MaxConcurrentTools: 2, Hooks: hreg, Audit: auditor.Audit,
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}

	res := runPastVeto(t, loop, slow, vetoed)
	if res.Stop != agentloop.StopHookVeto {
		t.Fatalf("Stop = %q, want StopHookVeto", res.Stop)
	}
	if n := slow.runs.Load(); n != 1 {
		t.Fatalf("slow runs = %d, want 1: the in-flight call must complete", n)
	}

	ids := historyToolIDs(res)
	foundSlow := false
	for _, id := range ids {
		if id == "call-slow" {
			foundSlow = true
		}
	}
	if !foundSlow {
		t.Fatalf("history tool IDs = %v, want call-slow recorded: the completed call's result must reach history", ids)
	}
	var auditIDs []string
	for _, rec := range auditor.snapshot() {
		if rec.Kind == agentloop.AuditKindToolCall {
			auditIDs = append(auditIDs, rec.ToolCall.ID)
		}
	}
	foundSlow = false
	for _, id := range auditIDs {
		if id == "call-slow" {
			foundSlow = true
		}
	}
	if !foundSlow {
		t.Fatalf("audit tool-call IDs = %v, want call-slow audited: the executed side effect must leave a record", auditIDs)
	}
}

// runPastVeto drives one Run in the background, releases the blocked
// tool once the veto has landed, and returns the result with a
// deadlock timeout guard.
func runPastVeto(t *testing.T, loop *agentloop.Loop, slow *blockingTool, vetoed chan struct{}) agentloop.Result {
	t.Helper()
	type runResult struct {
		res agentloop.Result
		err error
	}
	resultCh := make(chan runResult, 1)
	go func() {
		res, err := loop.Run(context.Background(), []provider.Message{textMessage(provider.RoleUser, "hi")})
		resultCh <- runResult{res, err}
	}()
	<-slow.entered
	<-vetoed
	close(slow.release)
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("Run() error = %v, want nil", r.err)
		}
		return r.res
	case <-time.After(5 * time.Second):
		t.Fatal("Run never returned: the batch deadlocked after the veto")
		return agentloop.Result{}
	}
}
