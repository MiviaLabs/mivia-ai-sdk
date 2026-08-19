package agentrun_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/MiviaLabs/mivia-ai-sdk/agent"
	"github.com/MiviaLabs/mivia-ai-sdk/agentrun"
	"github.com/MiviaLabs/mivia-ai-sdk/channel"
	"github.com/MiviaLabs/mivia-ai-sdk/envelope"
	"github.com/MiviaLabs/mivia-ai-sdk/flow"
	"github.com/MiviaLabs/mivia-ai-sdk/hooks"
	"github.com/MiviaLabs/mivia-ai-sdk/machine"
	"github.com/MiviaLabs/mivia-ai-sdk/tools"
)

// boomTool fails every run with a fixed sentinel.
type boomTool struct{}

// Name returns the registry name.
func (boomTool) Name() string { return "work" }

// Run fails with the sentinel.
func (boomTool) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{}, errBoomSentinel
}

// errBoomSentinel is boomTool's failure.
var errBoomSentinel = errors.New("boom: tool down")

// vetoingStop adds one stop-point veto handler.
func vetoingStop(t *testing.T, reg *hooks.Registry, name string) {
	t.Helper()
	if err := reg.Add(hooks.PointStop, name, func(ctx context.Context, payload any) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatalf("hooks.Add(%s): %v", name, err)
	}
}

// TestStopHookJoinsRunError proves a stop-hook veto over a failing
// run joins both errors: errors.Is finds the tool's sentinel and the
// hook's veto, and the final status rides along.
func TestStopHookJoinsRunError(t *testing.T) {
	plan, m := oneStepPlanMachine(t)
	reg := tools.New()
	addTools(t, reg, boomTool{})
	hookReg := hooks.New()
	vetoingStop(t, hookReg, "auditor")
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m,
		Tools: reg, Hooks: hookReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-join", machine.InOut{})
	if !errors.Is(err, errBoomSentinel) {
		t.Fatalf("Run error %v lacks the tool sentinel", err)
	}
	if !errors.Is(err, hooks.ErrVetoed) {
		t.Fatalf("Run error %v lacks the stop-hook veto", err)
	}
	if status != "done" {
		t.Fatalf("status = %q, want the fired row's final status", status)
	}
}

// TestStopHookFiresWithWaitResolver proves PointStop fires when the
// Wait resolver drives the run, with the final status as payload.
func TestStopHookFiresWithWaitResolver(t *testing.T) {
	plan, m := oneStepPlanMachine(t)
	hookReg := hooks.New()
	var stopPayload any
	fired := false
	if err := hookReg.Add(hooks.PointStop, "watcher", func(ctx context.Context, payload any) (bool, error) {
		fired = true
		stopPayload = payload
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m,
		Wait:  waitFn(),
		Hooks: hookReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	status, _, err := runner.Run(context.Background(), "thread-wait-stop", machine.InOut{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fired {
		t.Fatal("PointStop never fired under the Wait resolver")
	}
	if got, ok := stopPayload.(machine.Status); !ok || got != status || got != "done" {
		t.Fatalf("stop payload = %#v, want the final status %q", stopPayload, status)
	}
}

// TestAskPathFiresPostTool proves an approved Ask round trip fires
// PointPostTool with the confirmed ack, the same payload shape the
// tools path delivers.
func TestAskPathFiresPostTool(t *testing.T) {
	plan, err := flow.New([]flow.Step{
		{ID: "work", To: "done", Payload: "decide"},
	}, nil)
	if err != nil {
		t.Fatalf("flow.New: %v", err)
	}
	m, err := machine.New("queued",
		machine.Transition{From: "queued", To: "done", Trigger: "run"})
	if err != nil {
		t.Fatalf("machine.New: %v", err)
	}
	reg := tools.New()
	addTools(t, reg, escalateByName{})
	approve := channel.Notifier(func(ctx context.Context, q channel.Question) (channel.Answer, error) {
		return channel.Answer{QuestionID: q.ID, Approved: true, Payload: "human approved"}, nil
	})
	hookReg := hooks.New()
	fired := 0
	var postPayload any
	if err := hookReg.Add(hooks.PointPostTool, "auditor", func(ctx context.Context, payload any) (bool, error) {
		fired++
		postPayload = payload
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m,
		Tools: reg, Ask: approve, AskTo: "human-1", Hooks: hookReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	if _, _, err := runner.Run(context.Background(), "thread-ask-post", machine.InOut{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fired != 1 {
		t.Fatalf("post-tool fires = %d, want 1 on the ask path", fired)
	}
	ack, ok := postPayload.(envelope.Ack)
	if !ok || ack.Status != envelope.AckConfirmed || ack.Restatement != "human approved" {
		t.Fatalf("post payload = %#v, want the confirmed ask ack", postPayload)
	}
}

// escalateByName fails every run wrapping agent.ErrEscalated.
type escalateByName struct{}

// Name returns the registry name.
func (escalateByName) Name() string { return "work" }

// Run asks for a human.
func (escalateByName) Run(ctx context.Context, in tools.InOut) (tools.Out, error) {
	return tools.Out{}, fmt.Errorf("ask: %w", agent.ErrEscalated)
}

// TestPreToolPayloadIsMessage proves PointPreTool delivers the signed
// step message, not a fragment of it.
func TestPreToolPayloadIsMessage(t *testing.T) {
	plan, m := oneStepPlanMachine(t)
	hookReg := hooks.New()
	var prePayload any
	if err := hookReg.Add(hooks.PointPreTool, "gate", func(ctx context.Context, payload any) (bool, error) {
		prePayload = payload
		return true, nil
	}); err != nil {
		t.Fatalf("hooks.Add: %v", err)
	}
	calls := 0
	reg := tools.New()
	addTools(t, reg, runCounterTool{calls: &calls})
	runner, err := agentrun.New(agentrun.Options{
		Agent: mustAgent(t, plan), Machine: m,
		Tools: reg, Hooks: hookReg,
	})
	if err != nil {
		t.Fatalf("agentrun.New: %v", err)
	}
	if _, _, err := runner.Run(context.Background(), "thread-pre", machine.InOut{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msg, ok := prePayload.(envelope.Message)
	if !ok || msg.ID != "work" || msg.Payload != "go" {
		t.Fatalf("pre payload = %#v, want the signed work message", prePayload)
	}
}
